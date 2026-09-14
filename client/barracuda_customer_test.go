package client

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	pb "github.com/OcupointInc/rf-control/controlpb"
)

type scriptedTransport struct {
	replies  []*pb.Packet
	sent     []*pb.Packet
	fallback *pb.Packet
}

func (s *scriptedTransport) Send(packet *pb.Packet) (*pb.Packet, error) {
	s.sent = append(s.sent, packet)
	if len(s.replies) == 0 {
		if s.fallback != nil {
			return s.fallback, nil
		}
		return nil, errors.New("unexpected request")
	}
	reply := s.replies[0]
	s.replies = s.replies[1:]
	return reply, nil
}

func (*scriptedTransport) Close() error { return nil }

func barracudaStatus(locked bool) *pb.Packet {
	return &pb.Packet{MessageId: &pb.Packet_GetStatusResponse{GetStatusResponse: &pb.GetStatusResponse{
		BoardType: "barracuda", PllLocked: locked,
		Barracuda: &pb.BarracudaDiagnostics{
			LmxRequestedFrequencyHz: uint64(BarracudaFixedLOMHz) * 1_000_000,
			LmxActualFrequencyHz:    uint64(BarracudaFixedLOMHz) * 1_000_000,
			LmxLocked:               true,
			LmxOutputPowerCode:      BarracudaCalibratedLMXPowerCode,
		},
	}}}
}

func TestConfigureBarracudaCWSequence(t *testing.T) {
	tx := &scriptedTransport{replies: []*pb.Packet{
		barracudaStatus(false),
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
		{MessageId: &pb.Packet_SetClockSourceResponse{SetClockSourceResponse: &pb.SetClockSourceResponse{External: false}}},
		{MessageId: &pb.Packet_SetLoFrequencyResponse{SetLoFrequencyResponse: &pb.SetLoFrequencyResponse{}}},
		{MessageId: &pb.Packet_SetLmxOutputPowerResponse{SetLmxOutputPowerResponse: &pb.SetLmxOutputPowerResponse{}}},
		{MessageId: &pb.Packet_SetPllFrequencyResponse{SetPllFrequencyResponse: &pb.SetPllFrequencyResponse{}}},
		barracudaStatus(true),
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
	}}
	result, err := New(tx).ConfigureBarracudaCW(BarracudaCWConfig{Force: testForce(false),
		IFFrequencyMHz: 400, AttenuationDB: 6.25,
	})
	if err != nil {
		t.Fatalf("ConfigureBarracudaCW: %v", err)
	}
	wantTypes := []any{
		(*pb.Packet_GetStatusRequest)(nil),
		(*pb.Packet_SetDsaAttenuationRequest)(nil),
		(*pb.Packet_SetClockSourceRequest)(nil),
		(*pb.Packet_SetLoFrequencyRequest)(nil),
		(*pb.Packet_SetLmxOutputPowerRequest)(nil),
		(*pb.Packet_SetPllFrequencyRequest)(nil),
		(*pb.Packet_GetStatusRequest)(nil),
		(*pb.Packet_SetDsaAttenuationRequest)(nil),
	}
	if len(tx.sent) != len(wantTypes) {
		t.Fatalf("sent %d requests, want %d", len(tx.sent), len(wantTypes))
	}
	for i, want := range wantTypes {
		if reflect.TypeOf(tx.sent[i].MessageId) != reflect.TypeOf(want) {
			t.Errorf("request %d = %T, want %T", i, tx.sent[i].MessageId, want)
		}
	}
	if got := tx.sent[1].GetSetDsaAttenuationRequest().GetQuarterDb(); got != 0 {
		t.Errorf("prototype acquisition attenuation = %d quarter-dB, want 0", got)
	}
	if got := tx.sent[3].GetSetLoFrequencyRequest().GetFrequencyMhz(); got != BarracudaFixedLOMHz {
		t.Errorf("LO = %d MHz, want %d", got, BarracudaFixedLOMHz)
	}
	if got := tx.sent[4].GetSetLmxOutputPowerRequest().GetPowerCode(); got != BarracudaCalibratedLMXPowerCode {
		t.Errorf("LMX power code = %d, want %d", got, BarracudaCalibratedLMXPowerCode)
	}
	if got := tx.sent[5].GetSetPllFrequencyRequest().GetFrequencyMhz(); got != 10000 {
		t.Errorf("CW = %d MHz, want 10000", got)
	}
	if got := tx.sent[7].GetSetDsaAttenuationRequest().GetQuarterDb(); got != 25 {
		t.Errorf("final attenuation = %d quarter-dB, want 25", got)
	}
	if result.Mode != "cw" || result.StartIFMHz != 400 || result.StopIFMHz != 400 ||
		result.NominalOutputDBm != -31.25 || !result.SignalLocked || !result.LockVerified {
		t.Errorf("result = %+v", result)
	}
}

func TestConfigureBarracudaSweepSequence(t *testing.T) {
	tx := &scriptedTransport{replies: []*pb.Packet{
		barracudaStatus(false),
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
		{MessageId: &pb.Packet_SetClockSourceResponse{SetClockSourceResponse: &pb.SetClockSourceResponse{
			External: true, ReferenceValid: true, ReferenceSelected: true,
			DpllFrequencyLocked: true, DpllPhaseLocked: true,
		}}},
		{MessageId: &pb.Packet_SetLoFrequencyResponse{SetLoFrequencyResponse: &pb.SetLoFrequencyResponse{}}},
		{MessageId: &pb.Packet_SetLmxOutputPowerResponse{SetLmxOutputPowerResponse: &pb.SetLmxOutputPowerResponse{}}},
		// Firmware samples this bit immediately. A transient false must not make
		// the host fail before the following live status reports lock.
		{MessageId: &pb.Packet_SetChirpResponse{SetChirpResponse: &pb.SetChirpResponse{Locked: false}}},
		barracudaStatus(true),
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
	}}
	result, err := New(tx).ConfigureBarracudaSweep(BarracudaSweepConfig{Force: testForce(false),
		StartIFMHz: 50, StopIFMHz: 1500, SweepTime: 10 * time.Second,
		AttenuationDB: 0, ExternalClock: true,
	})
	if err != nil {
		t.Fatalf("ConfigureBarracudaSweep: %v", err)
	}
	if got := tx.sent[4].GetSetLmxOutputPowerRequest().GetPowerCode(); got != BarracudaCalibratedLMXPowerCode {
		t.Errorf("LMX power code = %d, want %d", got, BarracudaCalibratedLMXPowerCode)
	}
	chirp := tx.sent[5].GetSetChirpRequest()
	if chirp == nil {
		t.Fatalf("request 5 = %T, want chirp", tx.sent[5].MessageId)
	}
	if chirp.GetStartFreqMhz() != 9650 || chirp.GetDeviationMhz() != 1450 ||
		chirp.GetRampTimeUs() != 10_000_000 || !chirp.GetEnabled() ||
		chirp.GetMode() != pb.ChirpMode_CHIRP_MODE_SAWTOOTH_CONTINUOUS {
		t.Errorf("chirp = %+v", chirp)
	}
	if result.Mode != "sweep" || result.NominalOutputDBm != -25 || !result.ExternalClock {
		t.Errorf("result = %+v", result)
	}
}

func TestConfigureBarracudaCWRetriesDelayedLock(t *testing.T) {
	tx := &scriptedTransport{replies: []*pb.Packet{
		barracudaStatus(false),
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
		{MessageId: &pb.Packet_SetClockSourceResponse{SetClockSourceResponse: &pb.SetClockSourceResponse{External: false}}},
		{MessageId: &pb.Packet_SetLoFrequencyResponse{SetLoFrequencyResponse: &pb.SetLoFrequencyResponse{}}},
		{MessageId: &pb.Packet_SetLmxOutputPowerResponse{SetLmxOutputPowerResponse: &pb.SetLmxOutputPowerResponse{}}},
		{MessageId: &pb.Packet_SetPllFrequencyResponse{SetPllFrequencyResponse: &pb.SetPllFrequencyResponse{}}},
		barracudaStatus(false),
		barracudaStatus(true),
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
	}}

	result, err := New(tx).ConfigureBarracudaCW(BarracudaCWConfig{Force: testForce(false),
		IFFrequencyMHz: 400, AttenuationDB: 6.25,
	})
	if err != nil {
		t.Fatalf("ConfigureBarracudaCW: %v", err)
	}
	if !result.SignalLocked {
		t.Fatal("configuration did not report the delayed lock")
	}
	if len(tx.sent) != 9 {
		t.Fatalf("sent %d requests, want 9 including two lock polls", len(tx.sent))
	}
	if got := tx.sent[8].GetSetDsaAttenuationRequest().GetQuarterDb(); got != 25 {
		t.Errorf("final attenuation = %d quarter-dB, want 25", got)
	}
}

func TestWaitForBarracudaSynthesizerLocksTimeout(t *testing.T) {
	tx := &scriptedTransport{replies: []*pb.Packet{barracudaStatus(false)}}
	status, err := New(tx).waitForBarracudaSynthesizerLocks(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if status.GetPllLocked() {
		t.Fatal("timeout returned locked status")
	}
	if len(tx.sent) != 1 {
		t.Fatalf("sent %d status requests, want 1", len(tx.sent))
	}
}

func TestWaitForBarracudaSynthesizerLocksRetriesDelayedLMX(t *testing.T) {
	lmxAcquiring := barracudaStatus(true)
	lmxAcquiring.GetGetStatusResponse().Barracuda.LmxLocked = false
	tx := &scriptedTransport{replies: []*pb.Packet{
		lmxAcquiring,
		barracudaStatus(true),
	}}
	status, err := New(tx).waitForBarracudaSynthesizerLocks(time.Second, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !status.GetPllLocked() || !status.GetBarracuda().GetLmxLocked() {
		t.Fatalf("locks = ADF %v, LMX %v; want both true", status.GetPllLocked(), status.GetBarracuda().GetLmxLocked())
	}
	if len(tx.sent) != 2 {
		t.Fatalf("sent %d status requests, want 2", len(tx.sent))
	}
}

func TestBarracudaSynthesizerLockErrorNamesFailedDevice(t *testing.T) {
	tests := []struct {
		name      string
		adfLocked bool
		lmxLocked bool
		want      string
	}{
		{name: "ADF4159", adfLocked: false, lmxLocked: true, want: "ADF4159 did not lock"},
		{name: "LMX2595", adfLocked: true, lmxLocked: false, want: "LMX2595 did not lock"},
		{name: "both", adfLocked: false, lmxLocked: false, want: "ADF4159 and LMX2595 did not lock"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			status := barracudaStatus(test.adfLocked).GetGetStatusResponse()
			status.Barracuda.LmxLocked = test.lmxLocked
			err := barracudaSynthesizerLockError(status)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestConfigureBarracudaExternalClockFailureLeavesUnattenuated(t *testing.T) {
	t.Parallel()
	tx := &scriptedTransport{fallback: barracudaStatus(false), replies: []*pb.Packet{
		barracudaStatus(false),
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
		{MessageId: &pb.Packet_SetClockSourceResponse{SetClockSourceResponse: &pb.SetClockSourceResponse{External: true}}},
	}}
	_, err := New(tx).ConfigureBarracudaCW(BarracudaCWConfig{Force: testForce(false),
		IFFrequencyMHz: 400, ExternalClock: true,
	})
	if err == nil || !strings.Contains(err.Error(), "not valid and fully locked") {
		t.Fatalf("error = %v, want external-reference failure", err)
	}
	for _, packet := range tx.sent[3:] {
		if packet.GetGetStatusRequest() == nil {
			t.Fatalf("request after clock failure = %T, want status poll", packet.MessageId)
		}
	}
	if got := tx.sent[1].GetSetDsaAttenuationRequest().GetQuarterDb(); got != 0 {
		t.Errorf("failure path attenuation = %d quarter-dB, want 0", got)
	}
}

func TestConfigureBarracudaPowerMismatchLeavesUnattenuated(t *testing.T) {
	badStatus := barracudaStatus(true)
	badStatus.GetGetStatusResponse().GetBarracuda().LmxOutputPowerCode = 49
	tx := &scriptedTransport{replies: []*pb.Packet{
		barracudaStatus(false),
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
		{MessageId: &pb.Packet_SetClockSourceResponse{SetClockSourceResponse: &pb.SetClockSourceResponse{External: false}}},
		{MessageId: &pb.Packet_SetLoFrequencyResponse{SetLoFrequencyResponse: &pb.SetLoFrequencyResponse{}}},
		{MessageId: &pb.Packet_SetLmxOutputPowerResponse{SetLmxOutputPowerResponse: &pb.SetLmxOutputPowerResponse{}}},
		{MessageId: &pb.Packet_SetPllFrequencyResponse{SetPllFrequencyResponse: &pb.SetPllFrequencyResponse{}}},
		badStatus,
	}}
	_, err := New(tx).ConfigureBarracudaCW(BarracudaCWConfig{Force: testForce(false), IFFrequencyMHz: 400})
	if err == nil || !strings.Contains(err.Error(), "calibrated customer setting") {
		t.Fatalf("error = %v, want calibrated-power failure", err)
	}
	if len(tx.sent) != 7 {
		t.Fatalf("sent %d requests after verification failure, want 7", len(tx.sent))
	}
	if got := tx.sent[1].GetSetDsaAttenuationRequest().GetQuarterDb(); got != 0 {
		t.Errorf("failure path attenuation = %d quarter-dB, want 0", got)
	}
}

func TestConfigureBarracudaRejectsInvalidInputBeforeTransport(t *testing.T) {
	tests := []struct {
		name string
		call func(*Client) error
	}{
		{"CW range", func(c *Client) error {
			_, err := c.ConfigureBarracudaCW(BarracudaCWConfig{Force: testForce(false), IFFrequencyMHz: 49})
			return err
		}},
		{"attenuation step", func(c *Client) error {
			_, err := c.ConfigureBarracudaCW(BarracudaCWConfig{Force: testForce(false), IFFrequencyMHz: 400, AttenuationDB: 0.1})
			return err
		}},
		{"sweep order", func(c *Client) error {
			_, err := c.ConfigureBarracudaSweep(BarracudaSweepConfig{Force: testForce(false), StartIFMHz: 1000, StopIFMHz: 500, SweepTime: time.Second})
			return err
		}},
		{"sweep resolution", func(c *Client) error {
			_, err := c.ConfigureBarracudaSweep(BarracudaSweepConfig{Force: testForce(false), StartIFMHz: 50, StopIFMHz: 1500, SweepTime: time.Nanosecond})
			return err
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tx := &scriptedTransport{}
			if err := test.call(New(tx)); err == nil {
				t.Fatal("got nil error")
			}
			if len(tx.sent) != 0 {
				t.Fatalf("sent %d requests for invalid input", len(tx.sent))
			}
		})
	}
}

func TestConfigureBarracudaLockTimeoutLeavesUnattenuated(t *testing.T) {
	for _, mode := range []string{"cw", "sweep"} {
		for _, failed := range []string{"ADF4159", "LMX2595", "both"} {
			t.Run(mode+"/"+failed, func(t *testing.T) {
				t.Parallel()
				unlocked := barracudaStatus(failed == "LMX2595")
				unlocked.GetGetStatusResponse().Barracuda.LmxLocked = failed == "ADF4159"
				setResponse := &pb.Packet{MessageId: &pb.Packet_SetPllFrequencyResponse{SetPllFrequencyResponse: &pb.SetPllFrequencyResponse{}}}
				if mode == "sweep" {
					// Even an initially asserted chirp lock must be checked live.
					setResponse = &pb.Packet{MessageId: &pb.Packet_SetChirpResponse{SetChirpResponse: &pb.SetChirpResponse{Locked: true}}}
				}
				tx := &scriptedTransport{fallback: unlocked, replies: []*pb.Packet{
					barracudaStatus(true),
					{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
					{MessageId: &pb.Packet_SetClockSourceResponse{SetClockSourceResponse: &pb.SetClockSourceResponse{}}},
					{MessageId: &pb.Packet_SetLoFrequencyResponse{SetLoFrequencyResponse: &pb.SetLoFrequencyResponse{}}},
					{MessageId: &pb.Packet_SetLmxOutputPowerResponse{SetLmxOutputPowerResponse: &pb.SetLmxOutputPowerResponse{}}},
					setResponse,
				}}
				c := New(tx)
				var result *BarracudaConfiguration
				var err error
				if mode == "cw" {
					result, err = c.ConfigureBarracudaCW(BarracudaCWConfig{Force: testForce(false), IFFrequencyMHz: 400, AttenuationDB: 6.25})
				} else {
					result, err = c.ConfigureBarracudaSweep(BarracudaSweepConfig{Force: testForce(false), StartIFMHz: 50, StopIFMHz: 1500, SweepTime: time.Second, AttenuationDB: 6.25})
				}
				var deviceErr *DeviceError
				if result != nil || !errors.As(err, &deviceErr) || !strings.Contains(err.Error(), "did not lock before timeout") {
					t.Fatalf("result = %+v, error = %v; want lock timeout", result, err)
				}
				if len(tx.sent) < 8 {
					t.Fatalf("sent %d requests, want multiple lock polls", len(tx.sent))
				}
				attenuationWrites := 0
				for _, packet := range tx.sent {
					if attenuation := packet.GetSetDsaAttenuationRequest(); attenuation != nil {
						attenuationWrites++
						if attenuation.GetQuarterDb() != 0 {
							t.Fatalf("failed Apply set DSA to %d quarter-dB, want 0", attenuation.GetQuarterDb())
						}
					}
				}
				if attenuationWrites != 1 {
					t.Fatalf("attenuation writes = %d, want only acquisition write", attenuationWrites)
				}
			})
		}
	}
}

func TestConfigureBarracudaWaitsForExternalReference(t *testing.T) {
	ready := barracudaStatus(true)
	ready.GetGetStatusResponse().ClockSourceExternal = true
	details := ready.GetGetStatusResponse().Barracuda
	details.LmkRefValid, details.LmkRefsel, details.LmkDpllLock = 0x04, 0x01, 0x06
	tx := &scriptedTransport{replies: []*pb.Packet{
		barracudaStatus(true),
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
		{MessageId: &pb.Packet_SetClockSourceResponse{SetClockSourceResponse: &pb.SetClockSourceResponse{External: true}}},
		barracudaStatus(false), ready,
		{MessageId: &pb.Packet_SetLoFrequencyResponse{SetLoFrequencyResponse: &pb.SetLoFrequencyResponse{}}},
		{MessageId: &pb.Packet_SetLmxOutputPowerResponse{SetLmxOutputPowerResponse: &pb.SetLmxOutputPowerResponse{}}},
		{MessageId: &pb.Packet_SetPllFrequencyResponse{SetPllFrequencyResponse: &pb.SetPllFrequencyResponse{}}},
		ready,
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
	}}
	result, err := New(tx).ConfigureBarracudaCW(BarracudaCWConfig{Force: testForce(false), IFFrequencyMHz: 400, AttenuationDB: 6.25, ExternalClock: true})
	if err != nil || !result.SignalLocked {
		t.Fatalf("result = %+v, error = %v", result, err)
	}
	clockWrites := 0
	for _, packet := range tx.sent {
		if packet.GetSetClockSourceRequest() != nil {
			clockWrites++
		}
	}
	if clockWrites != 1 {
		t.Fatalf("clock-source writes = %d, want 1", clockWrites)
	}
}

func TestWaitForBarracudaExternalReferenceRejectsReadFailure(t *testing.T) {
	status := barracudaStatus(true)
	status.GetGetStatusResponse().ClockSourceExternal = true
	details := status.GetGetStatusResponse().Barracuda
	details.LmkRefValid, details.LmkRefsel, details.LmkDpllLock = 0xff, 0x01, 0xff
	err := New(&scriptedTransport{replies: []*pb.Packet{status}}).waitForBarracudaExternalReference(0, 0)
	if err == nil || !strings.Contains(err.Error(), "before timeout") {
		t.Fatalf("error = %v, want external-reference timeout", err)
	}
}

func testForce(value bool) *bool { return &value }

func TestBarracudaForceDefaultsToTuningWithoutLockVerification(t *testing.T) {
	for _, mode := range []string{"cw", "sweep"} {
		for _, external := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/external=%v", mode, external), func(t *testing.T) {
				// Only identity is available. Neither PLL nor the external reference is locked.
				tuneReply := &pb.Packet{MessageId: &pb.Packet_SetPllFrequencyResponse{SetPllFrequencyResponse: &pb.SetPllFrequencyResponse{}}}
				if mode == "sweep" {
					tuneReply = &pb.Packet{MessageId: &pb.Packet_SetChirpResponse{SetChirpResponse: &pb.SetChirpResponse{Locked: false}}}
				}
				tx := &scriptedTransport{replies: []*pb.Packet{
					{MessageId: &pb.Packet_GetStatusResponse{GetStatusResponse: &pb.GetStatusResponse{BoardType: "barracuda"}}},
					{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
					{MessageId: &pb.Packet_SetClockSourceResponse{SetClockSourceResponse: &pb.SetClockSourceResponse{External: external}}},
					{MessageId: &pb.Packet_SetLoFrequencyResponse{SetLoFrequencyResponse: &pb.SetLoFrequencyResponse{}}},
					{MessageId: &pb.Packet_SetLmxOutputPowerResponse{SetLmxOutputPowerResponse: &pb.SetLmxOutputPowerResponse{}}},
					tuneReply,
					{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
				}}
				c := New(tx)
				var result *BarracudaConfiguration
				var err error
				if mode == "cw" {
					result, err = c.ConfigureBarracudaCW(BarracudaCWConfig{IFFrequencyMHz: 400, AttenuationDB: 6.25, ExternalClock: external})
				} else {
					result, err = c.ConfigureBarracudaSweep(BarracudaSweepConfig{StartIFMHz: 50, StopIFMHz: 1500, SweepTime: time.Second, AttenuationDB: 6.25, ExternalClock: external})
				}
				if err != nil {
					t.Fatal(err)
				}
				if result.LockVerified || result.SignalLocked || result.AttenuationDB != 6.25 {
					t.Fatalf("force result = %+v", result)
				}
				if len(tx.sent) != 7 {
					t.Fatalf("sent %d requests, want 7 without lock polls", len(tx.sent))
				}
				if tx.sent[3].GetSetLoFrequencyRequest().GetFrequencyMhz() != BarracudaFixedLOMHz ||
					tx.sent[4].GetSetLmxOutputPowerRequest().GetPowerCode() != BarracudaCalibratedLMXPowerCode {
					t.Fatal("force mode skipped the frequency/power plan")
				}
				if mode == "cw" && tx.sent[5].GetSetPllFrequencyRequest().GetFrequencyMhz() != 10000 {
					t.Fatal("CW not tuned")
				}
				if mode == "sweep" && !tx.sent[5].GetSetChirpRequest().GetEnabled() {
					t.Fatal("sweep not enabled")
				}
				if tx.sent[1].GetSetDsaAttenuationRequest().GetQuarterDb() != 0 ||
					tx.sent[6].GetSetDsaAttenuationRequest().GetQuarterDb() != 25 {
					t.Fatal("force mode must apply requested attenuation without automatic maximum attenuation")
				}
			})
		}
	}
}

func TestBarracudaDefaultForceStillRejectsInvalidInputAndDeviceErrors(t *testing.T) {
	tx := &scriptedTransport{}
	if _, err := New(tx).ConfigureBarracudaCW(BarracudaCWConfig{IFFrequencyMHz: 1}); err == nil || len(tx.sent) != 0 {
		t.Fatal("invalid force request reached transport")
	}
	for _, response := range []*pb.Packet{
		{MessageId: &pb.Packet_GetStatusResponse{GetStatusResponse: &pb.GetStatusResponse{BoardType: "straps"}}},
		{MessageId: &pb.Packet_ErrorResponse{ErrorResponse: &pb.ErrorResponse{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR, Detail: "device failure"}}},
	} {
		tx := &scriptedTransport{replies: []*pb.Packet{response}}
		if _, err := New(tx).ConfigureBarracudaCW(BarracudaCWConfig{IFFrequencyMHz: 400}); err == nil || len(tx.sent) != 1 {
			t.Fatal("force mode ignored identity/device error")
		}
	}
	if _, err := New(&scriptedTransport{}).ConfigureBarracudaCW(BarracudaCWConfig{IFFrequencyMHz: 400}); err == nil {
		t.Fatal("force mode ignored transport error")
	}
}
