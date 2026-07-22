package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	pb "github.com/OcupointInc/rf-control/controlpb"
)

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
