package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/OcupointInc/rf-control/client"
	pb "github.com/OcupointInc/rf-control/controlpb"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestDeriveCustomerNetwork(t *testing.T) {
	tests := []struct {
		address string
		ip      []byte
		gateway []byte
	}{
		{"192.168.50.25", []byte{192, 168, 50, 25}, []byte{192, 168, 50, 1}},
		{"10.20.30.254", []byte{10, 20, 30, 254}, []byte{10, 20, 30, 1}},
	}
	for _, test := range tests {
		ip, gateway, subnet, err := deriveCustomerNetwork(test.address)
		if err != nil {
			t.Fatalf("deriveCustomerNetwork(%q): %v", test.address, err)
		}
		if !bytes.Equal(ip, test.ip) || !bytes.Equal(gateway, test.gateway) ||
			!bytes.Equal(subnet, []byte{255, 255, 255, 0}) {
			t.Errorf("deriveCustomerNetwork(%q) = %v, %v, %v", test.address, ip, gateway, subnet)
		}
		gateway[3] = 99
		if ip[3] != test.ip[3] {
			t.Errorf("derived gateway aliases IP storage: ip=%v gateway=%v", ip, gateway)
		}
	}

	for _, address := range []string{
		"not-an-ip", "0.10.20.30", "127.0.0.2", "224.0.0.2",
		"192.168.50.0", "192.168.50.1", "192.168.50.255",
	} {
		_, _, _, err := deriveCustomerNetwork(address)
		if err == nil {
			t.Errorf("deriveCustomerNetwork(%q) succeeded, want error", address)
			continue
		}
		var usage *usageError
		if !errors.As(err, &usage) {
			t.Errorf("deriveCustomerNetwork(%q) error = %T, want usageError", address, err)
		}
	}
}

// resolveEnumArg must accept a bare integer, a canonical name, and a friendly
// alias — and reject unknown strings. The integer path is the regression guard:
// quoting the argument unconditionally used to make `set-band 2` fail.
func TestResolveEnumArg(t *testing.T) {
	ok := []struct {
		arg  string
		want int32
	}{
		{"2", 2},                    // bare integer
		{"1800-2700", 2},            // friendly alias
		{"RF_BAND_1800_2700MHZ", 2}, // canonical name
		{"rf_band_1800_2700mhz", 2}, // canonical name, lowercased
		{"0", 0},
		{"4", 4},
	}
	for _, c := range ok {
		got, err := resolveEnumArg(c.arg, pb.RfBand_value, rfBandAliases)
		if err != nil {
			t.Errorf("resolveEnumArg(%q) unexpected error: %v", c.arg, err)
			continue
		}
		if got != c.want {
			t.Errorf("resolveEnumArg(%q) = %d, want %d", c.arg, got, c.want)
		}
	}

	if _, err := resolveEnumArg("bogus", pb.RfBand_value, rfBandAliases); err == nil {
		t.Error("resolveEnumArg(\"bogus\") should have errored")
	}
}

// formatGPIOSelfTest must render an aligned table (GPIO number + signal name
// columns sized to the widest entry) and a summary Result line. On a whalepod
// board each stuck pin also gets an indented plain-English consequence line; on
// other boards (or board="") the table prints without descriptions.
func TestFormatGPIOSelfTest(t *testing.T) {
	pass := &pb.GpioSelfTestResponse{
		AllPassed: true,
		Pins: []*pb.GpioPinResult{
			{Pin: 0, Name: "PWR_EN", Passed: true, MinDriveMa: 4},
			{Pin: 2, Name: "SCK", Passed: true, MinDriveMa: 4},
		},
	}
	// The raw table always reports the drive each passing pin needed; both sit at
	// the 4 mA default here, so no marginal advisory follows the Result line.
	wantPass := "GPIO self-test — 2 pins\n" +
		"  GPIO 0  PWR_EN  PASS  (needs 4 mA)\n" +
		"  GPIO 2  SCK     PASS  (needs 4 mA)\n" +
		"Result: PASS — all 2 pin(s) ok\n"
	if got := formatGPIOSelfTest("whalepod_automation", pass); got != wantPass {
		t.Errorf("pass case:\n got:\n%q\nwant:\n%q", got, wantPass)
	}

	fail := &pb.GpioSelfTestResponse{
		AllPassed: false,
		Pins: []*pb.GpioPinResult{
			{Pin: 0, Name: "PWR_EN", Passed: true, MinDriveMa: 4},
			{Pin: 12, Name: "CAL_SW", Passed: false, Stuck: pb.GpioStuckState_GPIO_STUCK_STATE_LOW},
		},
	}
	// pinW widens to 2 (pin 12), so pin 0 right-aligns to "  0"; the stuck CAL_SW
	// gets a description line indented (9+pinW = 11 spaces) under the name column.
	// A failed pin keeps FAIL — no "needs 0 mA".
	wantFail := "GPIO self-test — 2 pins\n" +
		"  GPIO  0  PWR_EN  PASS  (needs 4 mA)\n" +
		"  GPIO 12  CAL_SW  FAIL  (stuck LOW)\n" +
		"           ↳ Calibration switch is stuck in CALIBRATION mode — the unit can't return to the normal RF signal path, so live signals never reach the output.\n" +
		"Result: FAIL — 1 of 2 pin(s) stuck\n"
	if got := formatGPIOSelfTest("whalepod_automation", fail); got != wantFail {
		t.Errorf("fail case:\n got:\n%q\nwant:\n%q", got, wantFail)
	}

	// Same failing response on a non-whalepod board: table only, no description.
	wantFailNoBoard := "GPIO self-test — 2 pins\n" +
		"  GPIO  0  PWR_EN  PASS  (needs 4 mA)\n" +
		"  GPIO 12  CAL_SW  FAIL  (stuck LOW)\n" +
		"Result: FAIL — 1 of 2 pin(s) stuck\n"
	if got := formatGPIOSelfTest("straps", fail); got != wantFailNoBoard {
		t.Errorf("non-whalepod fail case:\n got:\n%q\nwant:\n%q", got, wantFailNoBoard)
	}

	// A non-switch pad that only passes above the 4 mA default is flagged with its
	// elevated drive in-line AND draws the one-line advisory after the Result.
	marginal := &pb.GpioSelfTestResponse{
		AllPassed: true,
		Pins: []*pb.GpioPinResult{
			{Pin: 0, Name: "PWR_EN", Passed: true, MinDriveMa: 4},
			{Pin: 7, Name: "CS_VHF", Passed: true, MinDriveMa: 8},
		},
	}
	wantMarginal := "GPIO self-test — 2 pins\n" +
		"  GPIO 0  PWR_EN  PASS  (needs 4 mA)\n" +
		"  GPIO 7  CS_VHF  PASS  (needs 8 mA)\n" +
		"Result: PASS — all 2 pin(s) ok\n" +
		"Note: 1 pin needed elevated drive (possible flux/leakage — consider cleaning/rework).\n"
	if got := formatGPIOSelfTest("whalepod_automation", marginal); got != wantMarginal {
		t.Errorf("marginal case:\n got:\n%q\nwant:\n%q", got, wantMarginal)
	}
}

// formatGPIOSelfTestGrouped must roll the per-pin results up into functional
// subsystems: a subsystem is PASS only if all its present pins passed; a failing
// single-pin subsystem shows just the consequence; a failing multi-pin subsystem
// (the attenuator bus) spells out each member so it's clear whether all or one
// channel is affected.
func TestFormatGPIOSelfTestGrouped(t *testing.T) {
	// A full whalepod_automation pin set (8 pins). vhfDrive is the lowest drive
	// CS_VHF passed at (0 = it failed, stuck LOW); every other pad sits at its
	// board default (4 mA, or 12 mA for the CAL_SW switch line).
	auto := func(vhfDrive uint32) *pb.GpioSelfTestResponse {
		vhfPassed := vhfDrive != 0
		stuck := pb.GpioStuckState_GPIO_STUCK_STATE_NONE
		if !vhfPassed {
			stuck = pb.GpioStuckState_GPIO_STUCK_STATE_LOW
		}
		return &pb.GpioSelfTestResponse{
			AllPassed: vhfPassed,
			Pins: []*pb.GpioPinResult{
				{Pin: 0, Name: "PWR_EN", Passed: true, MinDriveMa: 4},
				{Pin: 2, Name: "SCK", Passed: true, MinDriveMa: 4},
				{Pin: 3, Name: "MOSI", Passed: true, MinDriveMa: 4},
				{Pin: 7, Name: "CS_VHF", Passed: vhfPassed, Stuck: stuck, MinDriveMa: vhfDrive},
				{Pin: 8, Name: "CS_UHF", Passed: true, MinDriveMa: 4},
				{Pin: 12, Name: "CAL_SW", Passed: true, MinDriveMa: 12},
				{Pin: 15, Name: "CAL_AMP_EN", Passed: true, MinDriveMa: 4},
				{Pin: 9, Name: "CLOCK_EN", Passed: true, MinDriveMa: 4},
			},
		}
	}

	// All pass at their defaults: one bare PASS line per subsystem, no per-pin
	// spell-out and — crucially — no marginal flag or advisory (healthy = clean).
	got := formatGPIOSelfTestGrouped("whalepod_automation", auto(4))
	for _, want := range []string{
		"GPIO self-test — whalepod_automation: PASS",
		"Power enable                  PASS",
		"Attenuator control (VHF/UHF)  PASS",
		"Calibration enable            PASS",
		"Calibration source select     PASS",
		"Clock enable                  PASS",
		"Result: PASS",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("all-pass grouped output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "GPIO 2") { // no per-pin detail when everything passes
		t.Errorf("all-pass grouped output should not spell out pins:\n%s", got)
	}
	for _, unwanted := range []string{"marginal", "needs", "elevated drive"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("healthy grouped output should stay clean, but contains %q:\n%s", unwanted, got)
		}
	}

	// CS_VHF stuck: the attenuator subsystem FAILs and spells out its four pins,
	// only CS_VHF carrying the consequence; other subsystems still PASS.
	got = formatGPIOSelfTestGrouped("whalepod_automation", auto(0))
	for _, want := range []string{
		"GPIO self-test — whalepod_automation: FAIL",
		"Attenuator control (VHF/UHF)  FAIL",
		"SCK", "MOSI", "CS_UHF", // members spelled out
		"CS_VHF", "FAIL  (stuck LOW)",
		"VHF attenuation is frozen. UHF is unaffected.",
		"Calibration enable            PASS",
		"Result: FAIL — 1 of 8 pin(s) stuck",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("attenuator-fail grouped output missing %q:\n%s", want, got)
		}
	}

	// CS_VHF passes but only at 8 mA (> the 4 mA default): the attenuator
	// subsystem still PASSes, but is flagged marginal on its headline, names the
	// offending pin on a continuation line, and the board draws the one-line
	// advisory after Result: PASS. This is the full expected block.
	wantMarginal := "GPIO self-test — whalepod_automation: PASS\n" +
		"  Power enable                  PASS\n" +
		"  Attenuator control (VHF/UHF)  PASS (marginal: needs 8 mA)\n" +
		"      ↳ CS_VHF (GPIO 7) passed only at 8 mA (default 4 mA)\n" +
		"  Calibration enable            PASS\n" +
		"  Calibration source select     PASS\n" +
		"  Clock enable                  PASS\n" +
		"Result: PASS\n" +
		"Note: 1 pin needed elevated drive (possible flux/leakage — consider cleaning/rework).\n"
	if got = formatGPIOSelfTestGrouped("whalepod_automation", auto(8)); got != wantMarginal {
		t.Errorf("marginal grouped case:\n got:\n%q\nwant:\n%q", got, wantMarginal)
	}

	// A single-pin subsystem failure (CAL_SW) shows the consequence, not a pin list.
	single := &pb.GpioSelfTestResponse{
		AllPassed: false,
		Pins: []*pb.GpioPinResult{
			{Pin: 0, Name: "PWR_EN", Passed: true},
			{Pin: 12, Name: "CAL_SW", Passed: false, Stuck: pb.GpioStuckState_GPIO_STUCK_STATE_LOW},
		},
	}
	got = formatGPIOSelfTestGrouped("whalepod_automation", single)
	if !strings.Contains(got, "Calibration enable  FAIL") ||
		!strings.Contains(got, "stuck in CALIBRATION mode") {
		t.Errorf("single-pin subsystem fail missing name/consequence:\n%s", got)
	}
}

// ptrInt is a tiny helper for building batchConfig fields.
func ptrInt(n int) *int { return &n }

// enumFieldFrom builds an enumField from a raw JSON scalar, mimicking how the
// JSON decoder populates it from an `apply` document.
func enumFieldFrom(rawJSON string) *enumField {
	return &enumField{raw: json.RawMessage(rawJSON)}
}

// validate must reject bad values (unknown enum, out-of-range integer enum,
// out-of-range number) as usageErrors, and pass a clean document.
func TestBatchConfigValidate(t *testing.T) {
	bad := []struct {
		name string
		cfg  batchConfig
	}{
		{"unknown rf_band string", batchConfig{RfBand: enumFieldFrom(`"nope"`)}},
		{"out-of-range rf_band int", batchConfig{RfBand: enumFieldFrom(`7`)}},
		{"out-of-range rf_switch int", batchConfig{RfSwitch: enumFieldFrom(`5`)}},
		{"attenuation too high", batchConfig{AttenuationDb: ptrInt(999)}},
		{"pll too high", batchConfig{PllFrequencyMhz: ptrInt(99999)}},
		{"pll negative", batchConfig{PllFrequencyMhz: ptrInt(-1)}},
		{"rf_switch_channel too high", batchConfig{RfSwitchChannel: ptrInt(9)}},
		{"bad network ip", batchConfig{Network: &networkBatch{StaticIP: "999.1.1"}}},
	}
	for _, c := range bad {
		err := c.cfg.validate()
		if err == nil {
			t.Errorf("%s: validate() = nil, want error", c.name)
			continue
		}
		var ue *usageError
		if !errors.As(err, &ue) {
			t.Errorf("%s: validate() error is %T, want *usageError", c.name, err)
		}
	}

	good := []struct {
		name string
		cfg  batchConfig
	}{
		{"empty", batchConfig{}},
		{"rf_band int in range", batchConfig{RfBand: enumFieldFrom(`4`)}},
		{"rf_band alias", batchConfig{RfBand: enumFieldFrom(`"1800-2700"`)}},
		{"full valid", batchConfig{
			RfBand:          enumFieldFrom(`"900-1800"`),
			AttenuationDb:   ptrInt(10),
			PllFrequencyMhz: ptrInt(3500),
			RfSwitchChannel: ptrInt(0),
			Network:         &networkBatch{StaticIP: "172.16.22.30"},
		}},
	}
	for _, c := range good {
		if err := c.cfg.validate(); err != nil {
			t.Errorf("%s: validate() = %v, want nil", c.name, err)
		}
	}
}

// ----- Barracuda ADF4159 extended control -----------------------------------

// fakeTransport records the packet a Client sends and replies with a canned
// one, so the request-building half of client.Client can be tested without a
// device.
type fakeTransport struct {
	got   *pb.Packet
	reply *pb.Packet
}

func (f *fakeTransport) Send(p *pb.Packet) (*pb.Packet, error) {
	f.got = p
	return f.reply, nil
}

func (f *fakeTransport) Close() error { return nil }

// packetTag returns the wire field number the packet's oneof was encoded with.
// The whole point of these numbers is that they match the firmware's canonical
// control.proto, so assert on the encoded tag rather than the Go wrapper type —
// a renumbered field would still produce the right wrapper type but a wrong,
// silently-unhandled packet on the device.
func packetTag(t *testing.T, p *pb.Packet) protowire.Number {
	t.Helper()
	b, err := proto.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	num, _, n := protowire.ConsumeTag(b)
	if n < 0 {
		t.Fatalf("could not read a tag from %d encoded bytes", len(b))
	}
	return num
}

// The new Barracuda requests must go out on the tags the shared wire spec
// assigns them (52-61); the firmware dispatches on exactly these numbers.
func TestAdfRequestPacketTags(t *testing.T) {
	cases := []struct {
		name  string
		want  protowire.Number
		reply *pb.Packet
		call  func(c *client.Client) error
	}{
		{
			"set-chirp", 34,
			&pb.Packet{MessageId: &pb.Packet_SetChirpResponse{SetChirpResponse: &pb.SetChirpResponse{}}},
			func(c *client.Client) error { _, err := c.SetChirpEx(client.ChirpConfig{}); return err },
		},
		{
			"set-fsk", 52,
			&pb.Packet{MessageId: &pb.Packet_SetFskResponse{SetFskResponse: &pb.SetFskResponse{}}},
			func(c *client.Client) error { return c.SetFsk(11700, 500, true) },
		},
		{
			"set-phase", 54,
			&pb.Packet{MessageId: &pb.Packet_SetPhaseResponse{SetPhaseResponse: &pb.SetPhaseResponse{}}},
			func(c *client.Client) error { return c.SetPhase(pb.PhaseMode_PHASE_MODE_PSK, 90000) },
		},
		{
			"set-adf-ref", 56,
			&pb.Packet{MessageId: &pb.Packet_SetAdfRefConfigResponse{SetAdfRefConfigResponse: &pb.SetAdfRefConfigResponse{}}},
			func(c *client.Client) error { return c.SetAdfRefConfig(client.AdfRefConfig{RCounter: 1}) },
		},
		{
			"set-adf-loop", 58,
			&pb.Packet{MessageId: &pb.Packet_SetAdfLoopConfigResponse{SetAdfLoopConfigResponse: &pb.SetAdfLoopConfigResponse{}}},
			func(c *client.Client) error { return c.SetAdfLoopConfig(client.AdfLoopConfig{Csr: true}) },
		},
		{
			"set-adf-power", 60,
			&pb.Packet{MessageId: &pb.Packet_SetAdfPowerResponse{SetAdfPowerResponse: &pb.SetAdfPowerResponse{}}},
			func(c *client.Client) error { return c.SetAdfPower(client.AdfPowerConfig{PowerDown: true}) },
		},
	}
	for _, tc := range cases {
		fx := &fakeTransport{reply: tc.reply}
		if err := tc.call(client.New(fx)); err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
			continue
		}
		if got := packetTag(t, fx.got); got != tc.want {
			t.Errorf("%s: encoded on tag %d, want %d", tc.name, got, tc.want)
		}
	}
}

// The response tags matter just as much: a Client must accept the reply the
// firmware actually sends, and reject one it doesn't.
func TestAdfResponsePacketTags(t *testing.T) {
	want := map[string]protowire.Number{
		"set_fsk_response":             53,
		"set_phase_response":           55,
		"set_adf_ref_config_response":  57,
		"set_adf_loop_config_response": 59,
		"set_adf_power_response":       61,
	}
	packets := map[string]*pb.Packet{
		"set_fsk_response":             {MessageId: &pb.Packet_SetFskResponse{SetFskResponse: &pb.SetFskResponse{}}},
		"set_phase_response":           {MessageId: &pb.Packet_SetPhaseResponse{SetPhaseResponse: &pb.SetPhaseResponse{}}},
		"set_adf_ref_config_response":  {MessageId: &pb.Packet_SetAdfRefConfigResponse{SetAdfRefConfigResponse: &pb.SetAdfRefConfigResponse{}}},
		"set_adf_loop_config_response": {MessageId: &pb.Packet_SetAdfLoopConfigResponse{SetAdfLoopConfigResponse: &pb.SetAdfLoopConfigResponse{}}},
		"set_adf_power_response":       {MessageId: &pb.Packet_SetAdfPowerResponse{SetAdfPowerResponse: &pb.SetAdfPowerResponse{}}},
	}
	for name, p := range packets {
		if got := packetTag(t, p); got != want[name] {
			t.Errorf("%s: encoded on tag %d, want %d", name, got, want[name])
		}
	}
}

// SetChirpEx must forward every extended field, and the classic SetChirp must
// leave all of them at zero so existing callers keep getting the old waveform.
func TestSetChirpFieldMapping(t *testing.T) {
	reply := &pb.Packet{MessageId: &pb.Packet_SetChirpResponse{SetChirpResponse: &pb.SetChirpResponse{Locked: true}}}

	fx := &fakeTransport{reply: reply}
	locked, err := client.New(fx).SetChirpEx(client.ChirpConfig{
		StartFreqMHz:       11700,
		DeviationMHz:       1500,
		RampTimeUs:         35,
		Mode:               pb.ChirpMode_CHIRP_MODE_TRIANGLE_TRIGGERED,
		Enabled:            true,
		Parabolic:          true,
		DualRamp:           true,
		Ramp2DeviationMHz:  700,
		Ramp2TimeUs:        20,
		FastRamp:           true,
		FastRampDownTimeUs: 12,
		FskOnRampKHz:       250,
		DelayedStart:       true,
		RampDelay:          true,
		DelayUs:            40,
		TriangularDelay:    true,
		TxdataTriggerDelay: true,
		ExternalStepClock:  true,
		TxdataInvert:       true,
		MuxoutRampComplete: true,
	})
	if err != nil {
		t.Fatalf("SetChirpEx: %v", err)
	}
	if !locked {
		t.Error("SetChirpEx returned locked=false, want the reply's true")
	}
	req := fx.got.GetSetChirpRequest()
	if req == nil {
		t.Fatalf("SetChirpEx sent %T, want a SetChirpRequest", fx.got.MessageId)
	}
	checks := []struct {
		field string
		got   any
		want  any
	}{
		{"start_freq_mhz", req.GetStartFreqMhz(), int32(11700)},
		{"deviation_mhz", req.GetDeviationMhz(), int32(1500)},
		{"ramp_time_us", req.GetRampTimeUs(), uint32(35)},
		{"mode", req.GetMode(), pb.ChirpMode_CHIRP_MODE_TRIANGLE_TRIGGERED},
		{"enabled", req.GetEnabled(), true},
		{"parabolic", req.GetParabolic(), true},
		{"dual_ramp", req.GetDualRamp(), true},
		{"ramp2_deviation_mhz", req.GetRamp2DeviationMhz(), int32(700)},
		{"ramp2_time_us", req.GetRamp2TimeUs(), uint32(20)},
		{"fast_ramp", req.GetFastRamp(), true},
		{"fast_ramp_down_time_us", req.GetFastRampDownTimeUs(), uint32(12)},
		{"fsk_on_ramp_khz", req.GetFskOnRampKhz(), uint32(250)},
		{"delayed_start", req.GetDelayedStart(), true},
		{"ramp_delay", req.GetRampDelay(), true},
		{"delay_us", req.GetDelayUs(), uint32(40)},
		{"triangular_delay", req.GetTriangularDelay(), true},
		{"txdata_trigger_delay", req.GetTxdataTriggerDelay(), true},
		{"external_step_clock", req.GetExternalStepClock(), true},
		{"txdata_invert", req.GetTxdataInvert(), true},
		{"muxout_ramp_complete", req.GetMuxoutRampComplete(), true},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("SetChirpEx %s = %v, want %v", c.field, c.got, c.want)
		}
	}

	// Classic five-argument form: extended fields must all stay zero.
	fx = &fakeTransport{reply: reply}
	if _, err := client.New(fx).SetChirp(11700, 1500, 35, pb.ChirpMode_CHIRP_MODE_SAWTOOTH_CONTINUOUS, true); err != nil {
		t.Fatalf("SetChirp: %v", err)
	}
	classic := fx.got.GetSetChirpRequest()
	if classic.GetStartFreqMhz() != 11700 || classic.GetDeviationMhz() != 1500 ||
		classic.GetRampTimeUs() != 35 || !classic.GetEnabled() {
		t.Errorf("SetChirp did not forward the classic fields: %+v", classic)
	}
	if !proto.Equal(classic, &pb.SetChirpRequest{
		StartFreqMhz: 11700, DeviationMhz: 1500, RampTimeUs: 35, Enabled: true,
	}) {
		t.Errorf("SetChirp set an extended field; want only the classic five: %+v", classic)
	}
}

// The remaining new requests must map their arguments onto the right fields.
func TestAdfRequestFieldMapping(t *testing.T) {
	fx := &fakeTransport{reply: &pb.Packet{MessageId: &pb.Packet_SetFskResponse{SetFskResponse: &pb.SetFskResponse{}}}}
	if err := client.New(fx).SetFsk(11700, 750, true); err != nil {
		t.Fatalf("SetFsk: %v", err)
	}
	if fsk := fx.got.GetSetFskRequest(); fsk.GetCenterFreqMhz() != 11700 ||
		fsk.GetDeviationKhz() != 750 || !fsk.GetEnabled() {
		t.Errorf("SetFsk sent %+v", fx.got.GetSetFskRequest())
	}

	fx = &fakeTransport{reply: &pb.Packet{MessageId: &pb.Packet_SetPhaseResponse{SetPhaseResponse: &pb.SetPhaseResponse{}}}}
	if err := client.New(fx).SetPhase(pb.PhaseMode_PHASE_MODE_STATIC, -45000); err != nil {
		t.Fatalf("SetPhase: %v", err)
	}
	if ph := fx.got.GetSetPhaseRequest(); ph.GetMode() != pb.PhaseMode_PHASE_MODE_STATIC ||
		ph.GetPhaseMillidegrees() != -45000 {
		t.Errorf("SetPhase sent %+v", fx.got.GetSetPhaseRequest())
	}

	fx = &fakeTransport{reply: &pb.Packet{MessageId: &pb.Packet_SetAdfRefConfigResponse{SetAdfRefConfigResponse: &pb.SetAdfRefConfigResponse{}}}}
	if err := client.New(fx).SetAdfRefConfig(client.AdfRefConfig{
		RCounter: 4, RefDoubler: true, RefDiv2: true, Prescaler89: true, CpCurrentCode: 7,
	}); err != nil {
		t.Fatalf("SetAdfRefConfig: %v", err)
	}
	if r := fx.got.GetSetAdfRefConfigRequest(); r.GetRCounter() != 4 || !r.GetRefDoubler() ||
		!r.GetRefDiv2() || !r.GetPrescaler_8_9() || r.GetCpCurrentCode() != 7 {
		t.Errorf("SetAdfRefConfig sent %+v", fx.got.GetSetAdfRefConfigRequest())
	}

	fx = &fakeTransport{reply: &pb.Packet{MessageId: &pb.Packet_SetAdfLoopConfigResponse{SetAdfLoopConfigResponse: &pb.SetAdfLoopConfigResponse{}}}}
	if err := client.New(fx).SetAdfLoopConfig(client.AdfLoopConfig{
		Csr: true, NegativeBleed: true, NegativeBleedCode: 5, LolDisable: true, IntegerNMode: true,
	}); err != nil {
		t.Fatalf("SetAdfLoopConfig: %v", err)
	}
	if l := fx.got.GetSetAdfLoopConfigRequest(); !l.GetCsr() || !l.GetNegativeBleed() ||
		l.GetNegativeBleedCode() != 5 || !l.GetLolDisable() || !l.GetIntegerNMode() {
		t.Errorf("SetAdfLoopConfig sent %+v", fx.got.GetSetAdfLoopConfigRequest())
	}

	// Full-state: a zero config must still be sent, turning everything off.
	fx = &fakeTransport{reply: &pb.Packet{MessageId: &pb.Packet_SetAdfPowerResponse{SetAdfPowerResponse: &pb.SetAdfPowerResponse{}}}}
	if err := client.New(fx).SetAdfPower(client.AdfPowerConfig{}); err != nil {
		t.Fatalf("SetAdfPower: %v", err)
	}
	if p := fx.got.GetSetAdfPowerRequest(); p == nil {
		t.Error("SetAdfPower with a zero config sent no request")
	} else if p.GetPowerDown() || p.GetCpThreeState() || p.GetCounterReset() {
		t.Errorf("SetAdfPower zero config sent %+v, want all false", p)
	}
}

// A firmware refusal must reach the caller as a *client.DeviceError carrying
// the code and detail, so the CLI can print the constraint the device named.
func TestAdfDeviceErrorSurfaced(t *testing.T) {
	fx := &fakeTransport{reply: &pb.Packet{MessageId: &pb.Packet_ErrorResponse{
		ErrorResponse: &pb.ErrorResponse{
			Code:   pb.ErrorCode_ERROR_CODE_INVALID_REQUEST,
			Detail: "ref config requires a programmed frequency",
		},
	}}}
	err := client.New(fx).SetAdfRefConfig(client.AdfRefConfig{RCounter: 1})
	var de *client.DeviceError
	if !errors.As(err, &de) {
		t.Fatalf("SetAdfRefConfig error is %T (%v), want *client.DeviceError", err, err)
	}
	if de.Code != pb.ErrorCode_ERROR_CODE_INVALID_REQUEST {
		t.Errorf("code = %v, want INVALID_REQUEST", de.Code)
	}
	if !strings.Contains(err.Error(), "requires a programmed frequency") {
		t.Errorf("error %q does not carry the device's detail string", err)
	}
	if classifyExit("set-adf-ref", err) != exitDevice {
		t.Errorf("classifyExit = %d, want exitDevice (%d)", classifyExit("set-adf-ref", err), exitDevice)
	}
}

// Client-side validation for the new verbs. Every case here must be rejected
// before the command tries to open a transport, so these run without a device.
func TestAdfVerbFlagValidation(t *testing.T) {
	cases := []struct {
		name string
		run  func([]string) error
		args []string
		want string // substring the message must contain
	}{
		{"chirp parabolic + dual ramp", cmdSetChirp, []string{"--parabolic", "--dual-ramp"}, "--parabolic excludes"},
		{"chirp parabolic + fast ramp", cmdSetChirp, []string{"--parabolic", "--fast-ramp"}, "--parabolic excludes"},
		{"chirp triangular delay alone", cmdSetChirp, []string{"--triangular-delay"}, "requires --ramp-delay"},
		{"chirp triangular delay on sawtooth", cmdSetChirp, []string{"--triangular-delay", "--ramp-delay"}, "requires --mode triangle"},
		{"chirp negative delay", cmdSetChirp, []string{"--delay-us=-5"}, "--delay-us must be >= 0"},
		{"chirp negative ramp2 time", cmdSetChirp, []string{"--ramp2-time=-1"}, "--ramp2-time must be >= 0"},
		{"chirp negative fsk", cmdSetChirp, []string{"--fsk-khz=-1"}, "--fsk-khz must be >= 0"},
		{"chirp bad mode", cmdSetChirp, []string{"--mode", "spiral"}, "invalid --mode"},

		{"fsk without deviation", cmdSetFsk, nil, "--dev-khz must be > 0"},
		{"fsk negative deviation", cmdSetFsk, []string{"--dev-khz=-1"}, "--dev-khz must be >= 0"},

		{"phase no args", cmdSetPhase, nil, "usage: set-phase"},
		{"phase bad mode", cmdSetPhase, []string{"spin"}, "invalid phase mode"},
		{"phase psk without degrees", cmdSetPhase, []string{"psk"}, "needs a phase in degrees"},
		{"phase non-numeric", cmdSetPhase, []string{"static", "ninety"}, "invalid phase"},
		{"phase out of range", cmdSetPhase, []string{"static", "90000"}, "out of range"},

		{"ref r-counter zero", cmdSetAdfRef, []string{"--r-counter=0", "--cp-code=3"}, "expected 1-32"},
		{"ref r-counter too big", cmdSetAdfRef, []string{"--r-counter=33", "--cp-code=3"}, "expected 1-32"},
		{"ref r-counter negative via alias", cmdSetAdfRef, []string{"--r=-1"}, "expected 1-32"},
		{"ref cp-code too big", cmdSetAdfRef, []string{"--r-counter=1", "--cp-code=16"}, "expected 0-15"},
		{"ref cp-code negative via alias", cmdSetAdfRef, []string{"--cp=-1"}, "expected 0-15"},
		{"ref bad prescaler", cmdSetAdfRef, []string{"--r-counter=1", "--cp-code=0", "--prescaler", "6/7"}, "invalid --prescaler"},

		{"loop bleed code too big", cmdSetAdfLoop, []string{"--bleed-code=8"}, "expected 0-7"},
		{"loop bleed code negative", cmdSetAdfLoop, []string{"--bleed-code=-1"}, "expected 0-7"},
	}
	for _, c := range cases {
		err := c.run(c.args)
		if err == nil {
			t.Errorf("%s: got nil error, want a usage error", c.name)
			continue
		}
		var ue *usageError
		if !errors.As(err, &ue) {
			t.Errorf("%s: error is %T (%v), want *usageError", c.name, err, err)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not contain %q", c.name, err, c.want)
		}
		if classifyExit("cmd", err) != exitUsage {
			t.Errorf("%s: classifyExit = %d, want exitUsage (%d)", c.name, classifyExit("cmd", err), exitUsage)
		}
	}
}

// The mirror of the table above: valid flag combinations — including the range
// boundaries and the bare full-state calls — must get *past* validation. They
// then fail on the deliberately bogus --usb path, which proves validation
// accepted them without needing a device.
func TestAdfVerbFlagsAccepted(t *testing.T) {
	const noSuchPort = "/dev/null/not-a-serial-port"
	cases := []struct {
		name string
		run  func([]string) error
		args []string
	}{
		{"chirp classic", cmdSetChirp, nil},
		{"chirp parabolic", cmdSetChirp, []string{"--parabolic"}},
		{"chirp dual ramp", cmdSetChirp, []string{"--dual-ramp", "--ramp2-deviation=700", "--ramp2-time=20"}},
		{"chirp fast ramp", cmdSetChirp, []string{"--fast-ramp", "--fast-down-time=12"}},
		{"chirp triangular delay", cmdSetChirp, []string{"--mode", "triangle", "--ramp-delay", "--triangular-delay", "--delay-us=40"}},
		{"chirp txdata options", cmdSetChirp, []string{"--external-step-clock", "--txdata-invert", "--trigger-delay", "--delay-us=5"}},
		{"chirp ramp complete out", cmdSetChirp, []string{"--ramp-complete-out"}},
		{"fsk on", cmdSetFsk, []string{"--center=11700", "--dev-khz=500"}},
		{"fsk off needs no deviation", cmdSetFsk, []string{"--off"}},
		{"phase off without degrees", cmdSetPhase, []string{"off"}},
		{"phase psk", cmdSetPhase, []string{"psk", "90"}},
		{"phase negative static", cmdSetPhase, []string{"static", "-45.5"}},
		{"phase at range edge", cmdSetPhase, []string{"static", "360"}},
		{"ref bare full-state defaults", cmdSetAdfRef, nil},
		{"ref lowest", cmdSetAdfRef, []string{"--r-counter=1", "--cp-code=0"}},
		{"ref highest", cmdSetAdfRef, []string{"--r-counter=32", "--cp-code=15"}},
		{"ref 4/5 prescaler", cmdSetAdfRef, []string{"--r-counter=2", "--cp-code=7", "--prescaler", "4/5", "--ref-doubler", "--ref-div2"}},
		{"ref control_tool aliases", cmdSetAdfRef, []string{"--r=4", "--cp=3", "--doubler", "--div2"}},
		{"loop all off", cmdSetAdfLoop, nil},
		{"loop bleed at top", cmdSetAdfLoop, []string{"--negative-bleed", "--bleed-code=7", "--csr", "--lol-disable", "--integer-n"}},
		{"loop bleed alias", cmdSetAdfLoop, []string{"--bleed", "--bleed-code=0"}},
		{"power all off", cmdSetAdfPower, nil},
		{"power all on", cmdSetAdfPower, []string{"--power-down", "--cp-three-state", "--counter-reset"}},
		{"power tristate alias", cmdSetAdfPower, []string{"--cp-tristate"}},
	}
	for _, c := range cases {
		args := append([]string{"--usb", noSuchPort}, c.args...)
		err := c.run(args)
		if err == nil {
			t.Errorf("%s: got nil error, want a transport failure on the bogus port", c.name)
			continue
		}
		var ue *usageError
		if errors.As(err, &ue) {
			t.Errorf("%s: rejected valid flags: %v", c.name, err)
			continue
		}
		var te *client.TransportError
		if !errors.As(err, &te) {
			t.Errorf("%s: error is %T (%v), want *client.TransportError", c.name, err, err)
		}
	}
}
