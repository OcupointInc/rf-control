package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/OcupointInc/rf-control/internal/buildinfo"
	controlgui "github.com/OcupointInc/rf-control/internal/gui"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the narrow Wails binding surface. All device access remains in the
// testable internal/gui service rather than in frontend handlers.
type App struct {
	service *controlgui.Service
	ctx     context.Context
}

func NewApp() *App { return &App{service: controlgui.NewService()} }

func (a *App) startup(ctx context.Context) { a.ctx = ctx }

func (a *App) shutdown(context.Context) { a.service.Close() }

// Version returns the same build identifier exposed by the unified binary's
// CLI `version` command.
func (a *App) Version() string { return buildinfo.Version }

func (a *App) Discover() controlgui.DiscoveryResult { return a.service.Discover() }

func (a *App) Connect(endpoint controlgui.Endpoint) (controlgui.DeviceSnapshot, error) {
	return a.service.Connect(endpoint)
}

func (a *App) Disconnect() { a.service.Disconnect() }

func (a *App) GetStatus() (controlgui.DeviceSnapshot, error) { return a.service.Status() }

func (a *App) ConfigureCW(request controlgui.CWRequest) (controlgui.DeviceSnapshot, error) {
	return a.service.ConfigureCW(request)
}

func (a *App) ConfigureSweep(request controlgui.SweepRequest) (controlgui.DeviceSnapshot, error) {
	return a.service.ConfigureSweep(request)
}

func (a *App) ConfigureWhalepod(request controlgui.WhalepodRequest) (controlgui.DeviceSnapshot, error) {
	return a.service.ConfigureWhalepod(request)
}

func (a *App) ConfigureAirshark(request controlgui.AirsharkRequest) (controlgui.DeviceSnapshot, error) {
	return a.service.ConfigureAirshark(request)
}

func (a *App) ConfigureBlackCanyon(request controlgui.BlackCanyonRequest) (controlgui.DeviceSnapshot, error) {
	return a.service.ConfigureBlackCanyon(request)
}

func (a *App) SetMaximumAttenuation() (controlgui.DeviceSnapshot, error) {
	return a.service.MaximumAttenuation()
}

func (a *App) SetRFEnabled(enabled bool) (controlgui.DeviceSnapshot, error) {
	return a.service.SetRFEnabled(enabled)
}

func (a *App) SaveTuningProfile(profile controlgui.TuningProfile, product string) (string, error) {
	if err := controlgui.ValidateTuningProfile(profile); err != nil {
		return "", err
	}
	if a.ctx == nil {
		return "", fmt.Errorf("application is not ready")
	}
	name := "whalepod-control.json"
	if product == "black-canyon" {
		name = "black-canyon-control.json"
	}
	if profile.RFBand != nil {
		name = fmt.Sprintf("airshark-%s.json", strings.ReplaceAll(*profile.RFBand, "-", "_"))
	}
	if config := profile.Barracuda; config != nil {
		name = "barracuda-tuning.json"
		if strings.EqualFold(config.Mode, "cw") {
			name = fmt.Sprintf("barracuda-cw-%dmhz.json", config.IFFrequencyMHz)
		} else {
			name = fmt.Sprintf("barracuda-sweep-%d-%dmhz.json", config.StartIFMHz, config.StopIFMHz)
		}
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "Save control profile", DefaultFilename: name,
		Filters: []runtime.FileFilter{{DisplayName: "JSON control profile (*.json)", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return path, err
	}
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		path += ".json"
	}
	encoded, err := json.MarshalIndent(profile, "", "  ")
	if err != nil {
		return "", err
	}
	encoded = append(encoded, '\n')
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		return "", fmt.Errorf("save tuning profile: %w", err)
	}
	return path, nil
}

func (a *App) LoadTuningProfile() (controlgui.TuningProfile, error) {
	if a.ctx == nil {
		return controlgui.TuningProfile{}, fmt.Errorf("application is not ready")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Open control profile",
		Filters: []runtime.FileFilter{{DisplayName: "JSON control profile (*.json)", Pattern: "*.json"}},
	})
	if err != nil || path == "" {
		return controlgui.TuningProfile{}, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return controlgui.TuningProfile{}, fmt.Errorf("open tuning profile: %w", err)
	}
	var profile controlgui.TuningProfile
	if err := json.Unmarshal(raw, &profile); err != nil {
		return controlgui.TuningProfile{}, fmt.Errorf("parse tuning profile: %w", err)
	}
	if err := controlgui.ValidateTuningProfile(profile); err != nil {
		return controlgui.TuningProfile{}, err
	}
	return profile, nil
}

func (a *App) PreviewNetwork(address string) (controlgui.NetworkPlan, error) {
	return controlgui.PreviewNetwork(address)
}

func (a *App) SetIPAddress(address string) (controlgui.NetworkChangeResult, error) {
	return a.service.SetIPAddress(address)
}
