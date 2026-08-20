package cli

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/OcupointInc/rf-control/client"
	pb "github.com/OcupointInc/rf-control/controlpb"
)

func TestGUIProfileJSONIsAcceptedByApply(t *testing.T) {
	raw := []byte(`{"barracuda":{"mode":"cw","if_frequency_mhz":400,"attenuation_db":6.25,"clock":"internal","rf_enabled":false}}`)
	var config batchConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if err := config.validate(); err != nil {
		t.Fatal(err)
	}
	if config.Barracuda == nil || config.Barracuda.RFEnabled == nil || *config.Barracuda.RFEnabled {
		t.Fatalf("rf_enabled was not decoded as false: %+v", config.Barracuda)
	}
}

func TestParseCustomerCW(t *testing.T) {
	common, cfg, err := parseCustomerCW([]string{
		"--ip", "192.168.1.253", "--frequency", "400", "--attenuation", "6.25", "--clock", "external",
	})
	if err != nil {
		t.Fatalf("parseCustomerCW: %v", err)
	}
	if common.ip != "192.168.1.253" || cfg.IFFrequencyMHz != 400 ||
		cfg.AttenuationDB != 6.25 || !cfg.ExternalClock {
		t.Errorf("common=%+v cfg=%+v", common, cfg)
	}

	_, defaults, err := parseCustomerCW([]string{"--frequency", "50"})
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if defaults.AttenuationDB != 0 || defaults.ExternalClock {
		t.Errorf("defaults = %+v", defaults)
	}
}

func TestParseCustomerSweep(t *testing.T) {
	_, cfg, err := parseCustomerSweep([]string{
		"--start", "50", "--stop", "1500", "--time", "10s",
	})
	if err != nil {
		t.Fatalf("parseCustomerSweep: %v", err)
	}
	if cfg.StartIFMHz != 50 || cfg.StopIFMHz != 1500 || cfg.SweepTime != 10*time.Second ||
		cfg.AttenuationDB != 0 || cfg.ExternalClock {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestCustomerFlagValidation(t *testing.T) {
	tests := []struct {
		name string
		call func() error
		want string
	}{
		{"CW frequency required", func() error { _, _, err := parseCustomerCW(nil); return err }, "requires --frequency"},
		{"CW range", func() error { _, _, err := parseCustomerCW([]string{"--frequency", "49"}); return err }, "50..1500"},
		{"CW integer wrap", func() error { _, _, err := parseCustomerCW([]string{"--frequency", "4294977296"}); return err }, "50..1500"},
		{"clock", func() error {
			_, _, err := parseCustomerCW([]string{"--frequency", "400", "--clock", "gps"})
			return err
		}, "internal or external"},
		{"attenuation range", func() error {
			_, _, err := parseCustomerCW([]string{"--frequency", "400", "--attenuation", "32"})
			return err
		}, "0..31.75"},
		{"attenuation step", func() error {
			_, _, err := parseCustomerCW([]string{"--frequency", "400", "--attenuation", "0.1"})
			return err
		}, "0.25 dB steps"},
		{"attenuation NaN", func() error {
			_, _, err := parseCustomerCW([]string{"--frequency", "400", "--attenuation", "NaN"})
			return err
		}, "0..31.75"},
		{"sweep required", func() error { _, _, err := parseCustomerSweep(nil); return err }, "requires --start, --stop, and --time"},
		{"sweep order", func() error {
			_, _, err := parseCustomerSweep([]string{"--start", "1000", "--stop", "500", "--time", "1s"})
			return err
		}, "greater than start"},
		{"sweep units", func() error {
			_, _, err := parseCustomerSweep([]string{"--start", "50", "--stop", "1500", "--time", "10"})
			return err
		}, "include a unit"},
		{"sweep resolution", func() error {
			_, _, err := parseCustomerSweep([]string{"--start", "50", "--stop", "1500", "--time", "1ns"})
			return err
		}, "whole microseconds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if err == nil {
				t.Fatal("got nil error")
			}
			var usage *usageError
			if !errors.As(err, &usage) {
				t.Fatalf("error = %T, want usageError", err)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Errorf("error %q does not contain %q", err, test.want)
			}
		})
	}
}

func TestCustomerPowerEstimate(t *testing.T) {
	result := &client.BarracudaConfiguration{
		Mode: "cw", StartIFMHz: 400, StopIFMHz: 400,
		AttenuationDB: 6.25, NominalOutputDBm: client.BarracudaNominalOutputDBm - 6.25,
	}
	if math.Abs(result.NominalOutputDBm-(-31.25)) > 1e-9 {
		t.Errorf("nominal output = %v", result.NominalOutputDBm)
	}
}

func TestBarracudaBatchValidation(t *testing.T) {
	frequency, start, stop := 400, 50, 1500
	valid := []*batchConfig{
		{Barracuda: &barracudaBatch{Mode: "cw", IFFrequencyMHz: &frequency}},
		{Barracuda: &barracudaBatch{Mode: "sweep", StartIFMHz: &start, StopIFMHz: &stop, SweepTime: "10s", Clock: "external"}},
	}
	for _, cfg := range valid {
		if err := cfg.validate(); err != nil {
			t.Errorf("valid config %+v: %v", cfg.Barracuda, err)
		}
	}

	badFrequency, wrappedFrequency := 49, 4294977296
	invalid := []batchConfig{
		{Barracuda: &barracudaBatch{Mode: "cw"}},
		{Barracuda: &barracudaBatch{Mode: "cw", IFFrequencyMHz: &badFrequency}},
		{Barracuda: &barracudaBatch{Mode: "cw", IFFrequencyMHz: &wrappedFrequency}},
		{Barracuda: &barracudaBatch{Mode: "sweep", StartIFMHz: &start, StopIFMHz: &stop}},
		{Barracuda: &barracudaBatch{Mode: "pulse", IFFrequencyMHz: &frequency}},
		{Barracuda: &barracudaBatch{Mode: "cw", IFFrequencyMHz: &frequency}, AttenuationDb: ptrInt(1)},
	}
	for _, cfg := range invalid {
		if err := cfg.validate(); err == nil {
			t.Errorf("invalid config %+v passed validation", cfg)
		}
	}
}

func TestBarracudaStatusJSONIncludesShippingTelemetry(t *testing.T) {
	status := statusToJSON(&pb.GetStatusResponse{
		BoardType: "barracuda", PllLocked: true, RefLocked: true,
		McuTemperatureC: 42.5, McuTemperatureIsBootSample: true,
		Barracuda: &pb.BarracudaDiagnostics{LmxOutputPowerCode: 50},
	})
	if status.McuTemperatureC != 42.5 || !status.McuTemperatureBootSample ||
		status.Barracuda.GetLmxOutputPowerCode() != 50 || !status.PLLLocked || !status.RefLocked {
		t.Errorf("status = %+v", status)
	}
}

func TestCustomerOutputUsesIFAndHidesSynthesizers(t *testing.T) {
	status := &pb.GetStatusResponse{
		BoardType: "barracuda", PllLocked: true, AttenuationDb: 6,
		Barracuda: &pb.BarracudaDiagnostics{
			LmxRequestedFrequencyHz: 9_600_000_000,
			LmxActualFrequencyHz:    9_600_000_000,
			LmxLocked:               true,
			LmxOutputPowerCode:      50,
			AdfState:                &pb.Adf4159State{FrequencyMhz: 10_000},
		},
	}
	out := captureStdout(t, func() { printBarracudaCustomerStatus(status) })
	if !strings.Contains(out, "IF frequency        : 400 MHz") {
		t.Errorf("customer status did not report IF frequency:\n%s", out)
	}
	for _, hidden := range []string{"9600", "LO requested", "LMX", "ADF"} {
		if strings.Contains(out, hidden) {
			t.Errorf("customer status exposed %q:\n%s", hidden, out)
		}
	}

	status.Barracuda.LmxRequestedFrequencyHz = 10_000_000_000
	out = captureStdout(t, func() { printBarracudaCustomerStatus(status) })
	if strings.Contains(out, "IF frequency") || !strings.Contains(out, "Nominal output      : unavailable") {
		t.Errorf("non-customer internal plan produced a customer estimate:\n%s", out)
	}

	out = captureStdout(t, func() {
		printCustomerConfiguration(&client.BarracudaConfiguration{
			Mode: "cw", StartIFMHz: 400, StopIFMHz: 400, SignalLocked: true,
		})
	})
	if !strings.Contains(out, "CW configured: 400 MHz IF") || strings.Contains(out, "LO") {
		t.Errorf("customer configuration output =\n%s", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = write
	defer func() { os.Stdout = original }()
	fn()
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	return string(out)
}
