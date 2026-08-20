package main

import (
	"context"

	controlgui "github.com/OcupointInc/rf-control/internal/gui"
)

// App is the narrow Wails binding surface. All device access remains in the
// testable internal/gui service rather than in frontend handlers.
type App struct {
	service *controlgui.Service
}

func NewApp() *App { return &App{service: controlgui.NewService()} }

func (a *App) startup(context.Context) {}

func (a *App) shutdown(context.Context) { a.service.Close() }

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

func (a *App) SetMaximumAttenuation() (controlgui.DeviceSnapshot, error) {
	return a.service.MaximumAttenuation()
}

func (a *App) PreviewNetwork(address string) (controlgui.NetworkPlan, error) {
	return controlgui.PreviewNetwork(address)
}

func (a *App) SetIPAddress(address string) (controlgui.NetworkChangeResult, error) {
	return a.service.SetIPAddress(address)
}
