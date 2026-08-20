package main

import (
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/OcupointInc/rf-control/client"
	pb "github.com/OcupointInc/rf-control/controlpb"
)

func TestParseCustomerCW(t *testing.T) {
	common, cfg, err := parseCustomerCW([]string{
		"--ip", "192.168.1.253", "--frequency", "10000", "--attenuation", "6.25", "--clock", "external",
	})
	if err != nil {
		t.Fatalf("parseCustomerCW: %v", err)
	}
	if common.ip != "192.168.1.253" || cfg.FrequencyMHz != 10000 ||
		cfg.AttenuationDB != 6.25 || !cfg.ExternalClock {
		t.Errorf("common=%+v cfg=%+v", common, cfg)
	}

	_, defaults, err := parseCustomerCW([]string{"--frequency", "9500"})
	if err != nil {
		t.Fatalf("defaults: %v", err)
	}
	if defaults.AttenuationDB != 0 || defaults.ExternalClock {
		t.Errorf("defaults = %+v", defaults)
	}
}

func TestParseCustomerSweep(t *testing.T) {
	_, cfg, err := parseCustomerSweep([]string{
		"--start", "9600", "--stop", "11100", "--time", "10s",
	})
	if err != nil {
		t.Fatalf("parseCustomerSweep: %v", err)
	}
	if cfg.StartMHz != 9600 || cfg.StopMHz != 11100 || cfg.SweepTime != 10*time.Second ||
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
		{"CW range", func() error { _, _, err := parseCustomerCW([]string{"--frequency", "9000"}); return err }, "9500..11500"},
		{"CW integer wrap", func() error { _, _, err := parseCustomerCW([]string{"--frequency", "4294977296"}); return err }, "9500..11500"},
		{"clock", func() error {
			_, _, err := parseCustomerCW([]string{"--frequency", "10000", "--clock", "gps"})
			return err
		}, "internal or external"},
		{"attenuation range", func() error {
			_, _, err := parseCustomerCW([]string{"--frequency", "10000", "--attenuation", "32"})
			return err
		}, "0..31.75"},
		{"attenuation step", func() error {
			_, _, err := parseCustomerCW([]string{"--frequency", "10000", "--attenuation", "0.1"})
			return err
		}, "0.25 dB steps"},
		{"attenuation NaN", func() error {
			_, _, err := parseCustomerCW([]string{"--frequency", "10000", "--attenuation", "NaN"})
			return err
		}, "0..31.75"},
		{"sweep required", func() error { _, _, err := parseCustomerSweep(nil); return err }, "requires --start, --stop, and --time"},
		{"sweep order", func() error {
			_, _, err := parseCustomerSweep([]string{"--start", "11000", "--stop", "10000", "--time", "1s"})
			return err
		}, "greater than start"},
		{"sweep units", func() error {
			_, _, err := parseCustomerSweep([]string{"--start", "9600", "--stop", "11100", "--time", "10"})
			return err
		}, "include a unit"},
		{"sweep resolution", func() error {
			_, _, err := parseCustomerSweep([]string{"--start", "9600", "--stop", "11100", "--time", "1ns"})
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
		Mode: "cw", LOFrequencyMHz: 9600, StartMHz: 10000,
		AttenuationDB: 6.25, NominalOutputDBm: client.BarracudaNominalOutputDBm - 6.25,
	}
	if math.Abs(result.NominalOutputDBm-(-31.25)) > 1e-9 {
		t.Errorf("nominal output = %v", result.NominalOutputDBm)
	}
}

func TestBarracudaBatchValidation(t *testing.T) {
	frequency, start, stop := 10000, 9600, 11100
	valid := []*batchConfig{
		{Barracuda: &barracudaBatch{Mode: "cw", FrequencyMHz: &frequency}},
		{Barracuda: &barracudaBatch{Mode: "sweep", StartMHz: &start, StopMHz: &stop, SweepTime: "10s", Clock: "external"}},
	}
	for _, cfg := range valid {
		if err := cfg.validate(); err != nil {
			t.Errorf("valid config %+v: %v", cfg.Barracuda, err)
		}
	}

	badFrequency, wrappedFrequency := 9000, 4294977296
	invalid := []batchConfig{
		{Barracuda: &barracudaBatch{Mode: "cw"}},
		{Barracuda: &barracudaBatch{Mode: "cw", FrequencyMHz: &badFrequency}},
		{Barracuda: &barracudaBatch{Mode: "cw", FrequencyMHz: &wrappedFrequency}},
		{Barracuda: &barracudaBatch{Mode: "sweep", StartMHz: &start, StopMHz: &stop}},
		{Barracuda: &barracudaBatch{Mode: "pulse", FrequencyMHz: &frequency}},
		{Barracuda: &barracudaBatch{Mode: "cw", FrequencyMHz: &frequency}, AttenuationDb: ptrInt(1)},
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
