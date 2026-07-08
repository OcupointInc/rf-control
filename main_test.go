package main

import (
	"encoding/json"
	"errors"
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
