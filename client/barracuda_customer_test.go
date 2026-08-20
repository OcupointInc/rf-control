package client

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	pb "github.com/OcupointInc/rf-control/controlpb"
)

type scriptedTransport struct {
	replies []*pb.Packet
	sent    []*pb.Packet
}

func (s *scriptedTransport) Send(packet *pb.Packet) (*pb.Packet, error) {
	s.sent = append(s.sent, packet)
	if len(s.replies) == 0 {
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
	result, err := New(tx).ConfigureBarracudaCW(BarracudaCWConfig{
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
	if got := tx.sent[1].GetSetDsaAttenuationRequest().GetQuarterDb(); got != 127 {
		t.Errorf("mute attenuation = %d quarter-dB, want 127", got)
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
		result.NominalOutputDBm != -31.25 || !result.SignalLocked {
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
		{MessageId: &pb.Packet_SetChirpResponse{SetChirpResponse: &pb.SetChirpResponse{Locked: true}}},
		barracudaStatus(true),
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
	}}
	result, err := New(tx).ConfigureBarracudaSweep(BarracudaSweepConfig{
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

func TestConfigureBarracudaExternalClockFailureLeavesMuted(t *testing.T) {
	tx := &scriptedTransport{replies: []*pb.Packet{
		barracudaStatus(false),
		{MessageId: &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}},
		{MessageId: &pb.Packet_SetClockSourceResponse{SetClockSourceResponse: &pb.SetClockSourceResponse{External: true}}},
	}}
	_, err := New(tx).ConfigureBarracudaCW(BarracudaCWConfig{
		IFFrequencyMHz: 400, ExternalClock: true,
	})
	if err == nil || !strings.Contains(err.Error(), "not valid and fully locked") {
		t.Fatalf("error = %v, want external-reference failure", err)
	}
	if len(tx.sent) != 3 {
		t.Fatalf("sent %d requests after clock failure, want 3", len(tx.sent))
	}
	if got := tx.sent[1].GetSetDsaAttenuationRequest().GetQuarterDb(); got != 127 {
		t.Errorf("failure path did not mute first: %d", got)
	}
}

func TestConfigureBarracudaPowerMismatchLeavesMuted(t *testing.T) {
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
	_, err := New(tx).ConfigureBarracudaCW(BarracudaCWConfig{IFFrequencyMHz: 400})
	if err == nil || !strings.Contains(err.Error(), "calibrated customer setting") {
		t.Fatalf("error = %v, want calibrated-power failure", err)
	}
	if len(tx.sent) != 7 {
		t.Fatalf("sent %d requests after verification failure, want 7", len(tx.sent))
	}
	if got := tx.sent[1].GetSetDsaAttenuationRequest().GetQuarterDb(); got != 127 {
		t.Errorf("failure path did not mute first: %d", got)
	}
}

func TestConfigureBarracudaRejectsInvalidInputBeforeTransport(t *testing.T) {
	tests := []struct {
		name string
		call func(*Client) error
	}{
		{"CW range", func(c *Client) error {
			_, err := c.ConfigureBarracudaCW(BarracudaCWConfig{IFFrequencyMHz: 49})
			return err
		}},
		{"attenuation step", func(c *Client) error {
			_, err := c.ConfigureBarracudaCW(BarracudaCWConfig{IFFrequencyMHz: 400, AttenuationDB: 0.1})
			return err
		}},
		{"sweep order", func(c *Client) error {
			_, err := c.ConfigureBarracudaSweep(BarracudaSweepConfig{StartIFMHz: 1000, StopIFMHz: 500, SweepTime: time.Second})
			return err
		}},
		{"sweep resolution", func(c *Client) error {
			_, err := c.ConfigureBarracudaSweep(BarracudaSweepConfig{StartIFMHz: 50, StopIFMHz: 1500, SweepTime: time.Nanosecond})
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
