// Package gui contains the transport-agnostic application service used by the
// Wails desktop frontend. It deliberately exposes only customer-safe board
// controls plus common discovery, status, and network management.
package gui

import (
	"fmt"
	"math"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OcupointInc/rf-control/client"
	pb "github.com/OcupointInc/rf-control/controlpb"
)

const (
	defaultControlPort = 5000
	defaultScanTimeout = 5 * time.Second
	lmxRegisterCount   = 113
	lmxRegisterBatch   = 16
)

// Endpoint identifies one way to reach a device.
type Endpoint struct {
	Kind    string `json:"kind"`
	Address string `json:"address"`
	Port    int    `json:"port"`
}

// DiscoveredDevice combines USB and Ethernet endpoints when the serial or MAC
// identity shows they belong to the same physical unit.
type DiscoveredDevice struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	BoardType   string     `json:"boardType"`
	Serial      string     `json:"serial"`
	Firmware    string     `json:"firmware"`
	IPAddress   string     `json:"ipAddress"`
	MACAddress  string     `json:"macAddress"`
	Connections []Endpoint `json:"connections"`
}

type DiscoveryResult struct {
	Devices  []DiscoveredDevice `json:"devices"`
	Warnings []string           `json:"warnings"`
	TimedOut bool               `json:"timedOut"`
}

type NetworkConfig struct {
	IPAddress  string `json:"ipAddress"`
	Gateway    string `json:"gateway"`
	Subnet     string `json:"subnet"`
	Hostname   string `json:"hostname"`
	MACAddress string `json:"macAddress"`
	Serial     string `json:"serial"`
	Firmware   string `json:"firmware"`
	BoardID    string `json:"boardId"`
}

type DeviceStatus struct {
	BoardType               string  `json:"boardType"`
	BoardLabel              string  `json:"boardLabel"`
	Barracuda               bool    `json:"barracuda"`
	Whalepod                bool    `json:"whalepod"`
	Airshark                bool    `json:"airshark"`
	AirsharkBand            string  `json:"airsharkBand"`
	Mode                    string  `json:"mode"`
	IFFrequencyMHz          int32   `json:"ifFrequencyMHz"`
	SweepStopIFMHz          int32   `json:"sweepStopIfMHz"`
	SweepTime               string  `json:"sweepTime"`
	Clock                   string  `json:"clock"`
	ReferenceLockApplicable bool    `json:"referenceLockApplicable"`
	ReferenceLocked         bool    `json:"referenceLocked"`
	SignalLockApplicable    bool    `json:"signalLockApplicable"`
	SignalLocked            bool    `json:"signalLocked"`
	AttenuationDB           float64 `json:"attenuationDb"`
	MaximumAttenuation      bool    `json:"maximumAttenuation"`
	RFEnabled               bool    `json:"rfEnabled"`
	OutputEstimateAvailable bool    `json:"outputEstimateAvailable"`
	NominalOutputDBm        float64 `json:"nominalOutputDbm"`
	TemperatureAvailable    bool    `json:"temperatureAvailable"`
	TemperatureC            float64 `json:"temperatureC"`
	TemperatureBootSample   bool    `json:"temperatureBootSample"`
	ChannelsEnabled         bool    `json:"channelsEnabled"`
	CalibrationEnabled      bool    `json:"calibrationEnabled"`
	CalSourceInternal       bool    `json:"calSourceInternal"`
	CalAttenuationDB        int32   `json:"calAttenuationDb"`
	LOFrequencyMHz          int32   `json:"loFrequencyMHz"`
	RFSwitch                string  `json:"rfSwitch"`
	MixerSwitch             string  `json:"mixerSwitch"`
	IFSwitch                string  `json:"ifSwitch"`
	RFSwitchChannel         int32   `json:"rfSwitchChannel"`
}

type DeviceSnapshot struct {
	Connected       bool          `json:"connected"`
	Endpoint        Endpoint      `json:"endpoint"`
	Network         NetworkConfig `json:"network"`
	Status          DeviceStatus  `json:"status"`
	CustomerControl bool          `json:"customerControl"`
}

type CWRequest struct {
	FrequencyMHz int32   `json:"frequencyMHz"`
	Attenuation  float64 `json:"attenuation"`
	Clock        string  `json:"clock"`
	RFEnabled    bool    `json:"rfEnabled"`
}

type SweepRequest struct {
	StartMHz    int32   `json:"startMHz"`
	StopMHz     int32   `json:"stopMHz"`
	SweepTime   string  `json:"sweepTime"`
	Attenuation float64 `json:"attenuation"`
	Clock       string  `json:"clock"`
	RFEnabled   bool    `json:"rfEnabled"`
}

// WhalepodRequest is a complete pending Whalepod control state. The GUI sends
// it only when Apply is pressed, so changing a selector never changes hardware
// by itself.
type WhalepodRequest struct {
	AttenuationDB      int32 `json:"attenuationDb"`
	CalAttenuationDB   int32 `json:"calAttenuationDb"`
	ChannelsEnabled    bool  `json:"channelsEnabled"`
	CalibrationEnabled bool  `json:"calibrationEnabled"`
	CalSourceInternal  bool  `json:"calSourceInternal"`
}

// AirsharkRequest is a complete pending Airshark (firmware board type
// "straps") control state. A band preset programs the three switch banks and
// LO together; the remaining fields are then applied as one transaction.
type AirsharkRequest struct {
	Band               string `json:"band"`
	AttenuationDB      int32  `json:"attenuationDb"`
	CalAttenuationDB   int32  `json:"calAttenuationDb"`
	ChannelsEnabled    bool   `json:"channelsEnabled"`
	CalibrationEnabled bool   `json:"calibrationEnabled"`
}

// TuningProfile is intentionally identical to the CLI `apply` command's
// customer-facing Barracuda JSON block, so a file saved in the GUI can be used
// later by either the GUI or this same executable in CLI mode.
type TuningProfile struct {
	Barracuda          *BarracudaTuningProfile `json:"barracuda,omitempty"`
	AttenuationDB      *int32                  `json:"attenuation_db,omitempty"`
	CalAttenuationDB   *int32                  `json:"cal_attenuation_db,omitempty"`
	ChannelsEnabled    *bool                   `json:"channels_enabled,omitempty"`
	CalibrationEnabled *bool                   `json:"cal_enabled,omitempty"`
	CalSourceInternal  *bool                   `json:"cal_source_internal,omitempty"`
	RFBand             *string                 `json:"rf_band,omitempty"`
}

type BarracudaTuningProfile struct {
	Mode           string  `json:"mode"`
	IFFrequencyMHz int32   `json:"if_frequency_mhz,omitempty"`
	StartIFMHz     int32   `json:"start_if_mhz,omitempty"`
	StopIFMHz      int32   `json:"stop_if_mhz,omitempty"`
	SweepTime      string  `json:"sweep_time,omitempty"`
	AttenuationDB  float64 `json:"attenuation_db"`
	Clock          string  `json:"clock"`
	RFEnabled      bool    `json:"rf_enabled"`
}

func ValidateTuningProfile(profile TuningProfile) error {
	config := profile.Barracuda
	sharedFields := profile.AttenuationDB != nil || profile.CalAttenuationDB != nil ||
		profile.ChannelsEnabled != nil || profile.CalibrationEnabled != nil || profile.CalSourceInternal != nil
	if config == nil {
		if !sharedFields && profile.RFBand == nil {
			return fmt.Errorf("configuration profile does not contain supported controls")
		}
		for _, value := range []struct {
			name string
			db   *int32
		}{
			{"frontend attenuation", profile.AttenuationDB},
			{"calibration attenuation", profile.CalAttenuationDB},
		} {
			if value.db != nil && (*value.db < 0 || *value.db > 31) {
				return fmt.Errorf("%s must be 0–31 dB", value.name)
			}
		}
		if profile.RFBand != nil {
			if profile.CalSourceInternal != nil {
				return fmt.Errorf("an Airshark profile cannot include Whalepod calibration-source control")
			}
			if _, err := parseAirsharkBand(*profile.RFBand); err != nil {
				return err
			}
		}
		return nil
	}
	if sharedFields || profile.RFBand != nil {
		return fmt.Errorf("a Barracuda profile cannot include Airshark or Whalepod controls")
	}
	config.Mode = strings.ToLower(strings.TrimSpace(config.Mode))
	if _, err := externalClock(config.Clock); err != nil {
		return err
	}
	if config.AttenuationDB < 0 || config.AttenuationDB > client.BarracudaMaxAttenuationDB ||
		math.Abs(config.AttenuationDB*4-math.Round(config.AttenuationDB*4)) > 0.000001 {
		return fmt.Errorf("attenuation must be 0–31.75 dB in 0.25 dB steps")
	}
	switch config.Mode {
	case "cw":
		if config.IFFrequencyMHz < 50 || config.IFFrequencyMHz > 1500 {
			return fmt.Errorf("CW IF frequency must be 50–1500 MHz")
		}
	case "sweep":
		if config.StartIFMHz < 50 || config.StopIFMHz > 1500 || config.StopIFMHz <= config.StartIFMHz {
			return fmt.Errorf("sweep must be an ascending range within 50–1500 MHz")
		}
		duration, err := time.ParseDuration(strings.TrimSpace(config.SweepTime))
		if err != nil || duration <= 0 {
			return fmt.Errorf("invalid sweep time %q: include a positive unit such as 10s, 20ms, or 35us", config.SweepTime)
		}
	default:
		return fmt.Errorf("unsupported tuning mode %q (expected cw or sweep)", config.Mode)
	}
	return nil
}

type NetworkPlan struct {
	IPAddress string `json:"ipAddress"`
	Gateway   string `json:"gateway"`
	Subnet    string `json:"subnet"`
}

type NetworkChangeResult struct {
	Plan      NetworkPlan `json:"plan"`
	Rebooting bool        `json:"rebooting"`
}

// LMXRegister is one live 16-bit LMX2595 register readback. Address is kept as
// a number so callers can export it as JSON, CSV, or the CLI's R<n> notation.
type LMXRegister struct {
	Address uint32 `json:"address"`
	Value   uint32 `json:"value"`
}

type LMXRegisterReadResult struct {
	Registers []LMXRegister `json:"registers"`
}

type deviceClient interface {
	Close() error
	GetConfig() (*pb.GetConfigResponse, error)
	GetStatus() (*pb.GetStatusResponse, error)
	SaveConfig(*pb.SaveConfigRequest) error
	ConfigureBarracudaCW(client.BarracudaCWConfig) (*client.BarracudaConfiguration, error)
	ConfigureBarracudaSweep(client.BarracudaSweepConfig) (*client.BarracudaConfiguration, error)
	SetDsaAttenuation(int32) error
	SetLoFrequency(int32) error
	SetLMXOutputPower(uint32) error
	ReadLMX2595Registers([]uint32) (*pb.Lmx2595RegisterReadResponse, error)
	SetAttenuation(int32) error
	SetCalAttenuation(int32) error
	SetChannelsEnabled(bool) error
	SetCalEnabled(bool) error
	SetCalSource(bool) error
	SetRfBand(pb.RfBand) error
}

type session struct {
	endpoint  Endpoint
	client    deviceClient
	config    *pb.GetConfigResponse
	status    *pb.GetStatusResponse
	lastMode  string
	lastStart int32
	lastStop  int32
	lastTime  string
	lastAtt   *float64
}

// Service owns the selected-device session. Its mutex is intentionally held
// across each request because the firmware and USB transport accept only one
// request at a time; this also prevents status polling from interleaving with a
// mute-first RF configuration transaction.
type Service struct {
	mu               sync.Mutex
	active           *session
	open             func(Endpoint) (deviceClient, error)
	listUSB          func() ([]string, error)
	discoverEthernet func(time.Duration) ([]*pb.DiscoveryResponse, error)
	probeUSB         bool
	scanTimeout      time.Duration
}

func NewService() *Service {
	return &Service{
		open:             openDevice,
		listUSB:          client.ListCandidatePorts,
		discoverEthernet: client.DiscoverEthernet,
		probeUSB:         true,
		scanTimeout:      defaultScanTimeout,
	}
}

func openDevice(endpoint Endpoint) (deviceClient, error) {
	switch endpoint.Kind {
	case "usb":
		if strings.TrimSpace(endpoint.Address) == "" {
			return nil, fmt.Errorf("USB device path is required")
		}
		tx, err := client.NewUSBTransport(endpoint.Address)
		if err != nil {
			return nil, err
		}
		return client.New(tx), nil
	case "ethernet":
		ip := net.ParseIP(strings.TrimSpace(endpoint.Address))
		if ip == nil || ip.To4() == nil {
			return nil, fmt.Errorf("invalid device IPv4 address %q", endpoint.Address)
		}
		port := endpoint.Port
		if port == 0 {
			port = defaultControlPort
		}
		if port < 1 || port > 65535 {
			return nil, fmt.Errorf("invalid control port %d", port)
		}
		return client.New(client.NewTCPTransport(ip.String(), port)), nil
	default:
		return nil, fmt.Errorf("connection kind must be usb or ethernet")
	}
}

func (s *Service) Discover() DiscoveryResult {
	// Enforce the deadline in Go as well as in the frontend. Native serial-port
	// enumeration and open calls are owned by the host driver and can block a
	// Wails binding long enough that a JavaScript-only timer is not sufficient.
	done := make(chan DiscoveryResult, 1)
	go func() { done <- s.discover() }()

	timeout := s.scanTimeout
	if timeout <= 0 {
		timeout = defaultScanTimeout
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case result := <-done:
		return result
	case <-timer.C:
		return DiscoveryResult{
			Devices:  []DiscoveredDevice{},
			Warnings: []string{"Device scan timed out after 5 seconds. You can connect directly using its COM port or IP address."},
			TimedOut: true,
		}
	}
}

func (s *Service) discover() DiscoveryResult {
	// Only hold the session lock long enough to copy the active snapshot. USB
	// enumeration is host/driver controlled and may occasionally stall; a scan
	// must not prevent the user from connecting directly by IP after the GUI's
	// five-second scan timeout expires.
	s.mu.Lock()
	activeUSB := ""
	var activeSnapshot *DeviceSnapshot
	if s.active != nil {
		snapshot := s.snapshotLocked()
		activeSnapshot = &snapshot
		if s.active.endpoint.Kind == "usb" {
			activeUSB = s.active.endpoint.Address
		}
	}
	s.mu.Unlock()

	byID := make(map[string]*DiscoveredDevice)
	warnings := make([]string, 0)
	add := func(device DiscoveredDevice) {
		key := deviceKey(device.Serial, device.MACAddress, device.IPAddress, device.Connections[0])
		for existingKey, existing := range byID {
			sameSerial := device.Serial != "" && existing.Serial != "" && strings.EqualFold(device.Serial, existing.Serial)
			sameMAC := device.MACAddress != "" && existing.MACAddress != "" && strings.EqualFold(device.MACAddress, existing.MACAddress)
			if sameSerial || sameMAC {
				key = existingKey
				break
			}
		}
		if existing, ok := byID[key]; ok {
			existing.Connections = appendEndpoint(existing.Connections, device.Connections[0])
			if existing.IPAddress == "" {
				existing.IPAddress = device.IPAddress
			}
			if existing.BoardType == "" {
				existing.BoardType = device.BoardType
			}
			if existing.Name == "" {
				existing.Name = device.Name
			}
			if existing.Serial == "" {
				existing.Serial = device.Serial
			}
			if existing.MACAddress == "" {
				existing.MACAddress = device.MACAddress
			}
			if existing.Firmware == "" {
				existing.Firmware = device.Firmware
			}
			return
		}
		device.ID = key
		copy := device
		byID[key] = &copy
	}

	if activeSnapshot != nil {
		add(discoveredFromSnapshot(*activeSnapshot))
	}

	ports, err := s.listUSB()
	if err != nil {
		warnings = append(warnings, "USB enumeration: "+err.Error())
	} else {
		for _, port := range ports {
			if port == activeUSB {
				continue
			}
			endpoint := Endpoint{Kind: "usb", Address: port}
			if !s.probeUSB {
				add(DiscoveredDevice{
					Name: "USB serial device", Connections: []Endpoint{endpoint},
				})
				continue
			}
			dev, err := s.open(endpoint)
			if err != nil {
				continue
			}
			cfg, cfgErr := dev.GetConfig()
			status, statusErr := dev.GetStatus()
			_ = dev.Close()
			if cfgErr != nil || statusErr != nil {
				continue
			}
			add(discoveredFromResponses(endpoint, cfg, status))
		}
	}

	ethernet, err := s.discoverEthernet(900 * time.Millisecond)
	if err != nil {
		warnings = append(warnings, "Ethernet discovery: "+err.Error())
	} else {
		for _, found := range ethernet {
			port := int(found.GetControlPort())
			if port == 0 {
				port = defaultControlPort
			}
			endpoint := Endpoint{Kind: "ethernet", Address: ipString(found.GetIp()), Port: port}
			add(DiscoveredDevice{
				Name: found.GetName(), BoardType: found.GetBoard(), Serial: found.GetSerial(),
				Firmware: found.GetFirmwareVersion(), IPAddress: endpoint.Address,
				MACAddress: macString(found.GetMac()), Connections: []Endpoint{endpoint},
			})
		}
	}

	devices := make([]DiscoveredDevice, 0, len(byID))
	for _, device := range byID {
		sort.SliceStable(device.Connections, func(i, j int) bool {
			return device.Connections[i].Kind == "usb" && device.Connections[j].Kind != "usb"
		})
		devices = append(devices, *device)
	}
	sort.Slice(devices, func(i, j int) bool {
		if devices[i].BoardType != devices[j].BoardType {
			return devices[i].BoardType < devices[j].BoardType
		}
		return devices[i].ID < devices[j].ID
	})
	return DiscoveryResult{Devices: devices, Warnings: warnings}
}

func (s *Service) Connect(endpoint Endpoint) (DeviceSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil && s.active.endpoint == endpoint {
		cfg, err := s.active.client.GetConfig()
		if err != nil {
			return DeviceSnapshot{}, fmt.Errorf("read device configuration: %w", err)
		}
		status, err := s.active.client.GetStatus()
		if err != nil {
			return DeviceSnapshot{}, fmt.Errorf("read device status: %w", err)
		}
		s.active.config = cfg
		s.active.status = status
		return s.snapshotLocked(), nil
	}
	dev, err := s.open(endpoint)
	if err != nil {
		return DeviceSnapshot{}, err
	}
	cfg, err := dev.GetConfig()
	if err != nil {
		_ = dev.Close()
		return DeviceSnapshot{}, fmt.Errorf("read device configuration: %w", err)
	}
	status, err := dev.GetStatus()
	if err != nil {
		_ = dev.Close()
		return DeviceSnapshot{}, fmt.Errorf("read device status: %w", err)
	}
	if endpoint.Kind == "ethernet" && endpoint.Port == 0 {
		endpoint.Port = defaultControlPort
	}
	if s.active != nil {
		_ = s.active.client.Close()
	}
	s.active = &session{endpoint: endpoint, client: dev, config: cfg, status: status}
	return s.snapshotLocked(), nil
}

func (s *Service) Disconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil {
		_ = s.active.client.Close()
		s.active = nil
	}
}

func (s *Service) Status() (DeviceSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return DeviceSnapshot{}, fmt.Errorf("no device is connected")
	}
	status, err := s.active.client.GetStatus()
	if err != nil {
		return DeviceSnapshot{}, err
	}
	s.active.status = status
	return s.snapshotLocked(), nil
}

func (s *Service) ConfigureCW(request CWRequest) (DeviceSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireBarracudaLocked(); err != nil {
		return DeviceSnapshot{}, err
	}
	external, err := externalClock(request.Clock)
	if err != nil {
		return DeviceSnapshot{}, err
	}
	result, err := s.active.client.ConfigureBarracudaCW(client.BarracudaCWConfig{
		IFFrequencyMHz: request.FrequencyMHz, AttenuationDB: request.Attenuation, ExternalClock: external,
	})
	if err != nil {
		return DeviceSnapshot{}, err
	}
	s.active.lastMode = "cw"
	s.active.lastStart = result.StartIFMHz
	s.active.lastStop = result.StopIFMHz
	s.active.lastTime = ""
	s.active.lastAtt = floatPointer(result.AttenuationDB)
	if err := s.setRFEnabledLocked(request.RFEnabled); err != nil {
		return DeviceSnapshot{}, err
	}
	return s.refreshLocked()
}

func (s *Service) ConfigureSweep(request SweepRequest) (DeviceSnapshot, error) {
	duration, err := time.ParseDuration(strings.TrimSpace(request.SweepTime))
	if err != nil {
		return DeviceSnapshot{}, fmt.Errorf("invalid sweep time %q: include a unit such as 10s, 20ms, or 35us", request.SweepTime)
	}
	external, err := externalClock(request.Clock)
	if err != nil {
		return DeviceSnapshot{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireBarracudaLocked(); err != nil {
		return DeviceSnapshot{}, err
	}
	result, err := s.active.client.ConfigureBarracudaSweep(client.BarracudaSweepConfig{
		StartIFMHz: request.StartMHz, StopIFMHz: request.StopMHz, SweepTime: duration,
		AttenuationDB: request.Attenuation, ExternalClock: external,
	})
	if err != nil {
		return DeviceSnapshot{}, err
	}
	s.active.lastMode = "sweep"
	s.active.lastStart = result.StartIFMHz
	s.active.lastStop = result.StopIFMHz
	s.active.lastTime = result.SweepTime.String()
	s.active.lastAtt = floatPointer(result.AttenuationDB)
	if err := s.setRFEnabledLocked(request.RFEnabled); err != nil {
		return DeviceSnapshot{}, err
	}
	return s.refreshLocked()
}

// ConfigureWhalepod applies the complete Whalepod control state. Power-off is
// sent first when requested; power-on is sent last so newly enabled frontends
// never briefly expose stale attenuation or calibration routing.
func (s *Service) ConfigureWhalepod(request WhalepodRequest) (DeviceSnapshot, error) {
	if request.AttenuationDB < 0 || request.AttenuationDB > 31 {
		return DeviceSnapshot{}, fmt.Errorf("frontend attenuation must be 0–31 dB")
	}
	if request.CalAttenuationDB < 0 || request.CalAttenuationDB > 31 {
		return DeviceSnapshot{}, fmt.Errorf("calibration attenuation must be 0–31 dB")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireWhalepodLocked(); err != nil {
		return DeviceSnapshot{}, err
	}
	if !request.ChannelsEnabled {
		if err := s.active.client.SetChannelsEnabled(false); err != nil {
			return DeviceSnapshot{}, fmt.Errorf("turn frontend power off: %w", err)
		}
	}
	if err := s.active.client.SetAttenuation(request.AttenuationDB); err != nil {
		return DeviceSnapshot{}, fmt.Errorf("set frontend attenuation: %w", err)
	}
	if err := s.active.client.SetCalAttenuation(request.CalAttenuationDB); err != nil {
		return DeviceSnapshot{}, fmt.Errorf("set calibration attenuation: %w", err)
	}
	// Source must be selected before entering calibration mode: the firmware
	// gates the internal noise-source amplifier with both states.
	if err := s.active.client.SetCalSource(request.CalSourceInternal); err != nil {
		return DeviceSnapshot{}, fmt.Errorf("select calibration source: %w", err)
	}
	if err := s.active.client.SetCalEnabled(request.CalibrationEnabled); err != nil {
		return DeviceSnapshot{}, fmt.Errorf("select RF path: %w", err)
	}
	if request.ChannelsEnabled {
		if err := s.active.client.SetChannelsEnabled(true); err != nil {
			return DeviceSnapshot{}, fmt.Errorf("turn frontend power on: %w", err)
		}
	}
	return s.refreshLocked()
}

// ConfigureAirshark applies a customer-facing Airshark band preset and the
// complete frontend state. Power-off happens first and power-on last so routing
// and attenuation settle before an enabled frontend can pass RF.
func (s *Service) ConfigureAirshark(request AirsharkRequest) (DeviceSnapshot, error) {
	band, err := parseAirsharkBand(request.Band)
	if err != nil {
		return DeviceSnapshot{}, err
	}
	if request.AttenuationDB < 0 || request.AttenuationDB > 31 {
		return DeviceSnapshot{}, fmt.Errorf("frontend attenuation must be 0–31 dB")
	}
	if request.CalAttenuationDB < 0 || request.CalAttenuationDB > 31 {
		return DeviceSnapshot{}, fmt.Errorf("calibration attenuation must be 0–31 dB")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireAirsharkLocked(); err != nil {
		return DeviceSnapshot{}, err
	}
	if !request.ChannelsEnabled {
		if err := s.active.client.SetChannelsEnabled(false); err != nil {
			return DeviceSnapshot{}, fmt.Errorf("turn frontend power off: %w", err)
		}
	}
	if err := s.active.client.SetRfBand(band); err != nil {
		return DeviceSnapshot{}, fmt.Errorf("select RF band: %w", err)
	}
	if err := s.active.client.SetAttenuation(request.AttenuationDB); err != nil {
		return DeviceSnapshot{}, fmt.Errorf("set frontend attenuation: %w", err)
	}
	if err := s.active.client.SetCalAttenuation(request.CalAttenuationDB); err != nil {
		return DeviceSnapshot{}, fmt.Errorf("set calibration attenuation: %w", err)
	}
	if err := s.active.client.SetCalEnabled(request.CalibrationEnabled); err != nil {
		return DeviceSnapshot{}, fmt.Errorf("select RF path: %w", err)
	}
	if request.ChannelsEnabled {
		if err := s.active.client.SetChannelsEnabled(true); err != nil {
			return DeviceSnapshot{}, fmt.Errorf("turn frontend power on: %w", err)
		}
	}
	return s.refreshLocked()
}

// MaximumAttenuation applies the Barracuda DSA's 31.75 dB maximum. It does not
// claim to electrically disconnect the output; the UI labels it accordingly.
func (s *Service) MaximumAttenuation() (DeviceSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireBarracudaLocked(); err != nil {
		return DeviceSnapshot{}, err
	}
	if err := s.active.client.SetDsaAttenuation(127); err != nil {
		return DeviceSnapshot{}, fmt.Errorf("set maximum attenuation: %w", err)
	}
	s.active.lastAtt = floatPointer(client.BarracudaMaxAttenuationDB)
	return s.refreshLocked()
}

// SetRFEnabled controls the Barracuda LMX2595 output. A zero-MHz request is
// the firmware's explicit synthesizer power-down command; enabling restores
// the fixed customer-plan LO without disturbing the configured ADF/DSA state.
func (s *Service) SetRFEnabled(enabled bool) (DeviceSnapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireBarracudaLocked(); err != nil {
		return DeviceSnapshot{}, err
	}
	if err := s.setRFEnabledLocked(enabled); err != nil {
		return DeviceSnapshot{}, err
	}
	return s.refreshLocked()
}

func (s *Service) setRFEnabledLocked(enabled bool) error {
	frequencyMHz := int32(0)
	action := "off"
	if enabled {
		frequencyMHz = client.BarracudaFixedLOMHz
		action = "on"
	}
	if err := s.active.client.SetLoFrequency(frequencyMHz); err != nil {
		return fmt.Errorf("turn RF %s: %w", action, err)
	}
	if enabled {
		if err := s.active.client.SetLMXOutputPower(client.BarracudaCalibratedLMXPowerCode); err != nil {
			// Do not leave an uncalibrated output enabled if restoring the known
			// customer power code failed.
			_ = s.active.client.SetLoFrequency(0)
			return fmt.Errorf("turn RF on: restore LMX output power: %w", err)
		}
	}
	return nil
}

// ReadLMXRegisters returns live LMX2595 register values without changing the
// device. An empty address list means the complete R0..R112 register map. The
// firmware accepts at most 16 addresses per transaction, so larger GUI reads
// are split into ordered batches here.
func (s *Service) ReadLMXRegisters(addresses []uint32) (LMXRegisterReadResult, error) {
	normalized, err := normalizeLMXAddresses(addresses)
	if err != nil {
		return LMXRegisterReadResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireBarracudaLocked(); err != nil {
		return LMXRegisterReadResult{}, err
	}

	registers := make([]LMXRegister, 0, len(normalized))
	for start := 0; start < len(normalized); start += lmxRegisterBatch {
		end := start + lmxRegisterBatch
		if end > len(normalized) {
			end = len(normalized)
		}
		response, err := s.active.client.ReadLMX2595Registers(normalized[start:end])
		if err != nil {
			return LMXRegisterReadResult{}, fmt.Errorf("read LMX2595 registers R%d..R%d: %w", normalized[start], normalized[end-1], err)
		}
		if len(response.GetAddresses()) != len(response.GetValues()) || len(response.GetAddresses()) != end-start {
			return LMXRegisterReadResult{}, fmt.Errorf("LMX2595 register response contained %d addresses and %d values for a %d-register request", len(response.GetAddresses()), len(response.GetValues()), end-start)
		}
		for i, address := range response.GetAddresses() {
			if address >= lmxRegisterCount {
				return LMXRegisterReadResult{}, fmt.Errorf("device returned invalid LMX2595 register R%d", address)
			}
			if address != normalized[start+i] {
				return LMXRegisterReadResult{}, fmt.Errorf("device returned LMX2595 register R%d where R%d was requested", address, normalized[start+i])
			}
			registers = append(registers, LMXRegister{Address: address, Value: response.GetValues()[i]})
		}
	}
	return LMXRegisterReadResult{Registers: registers}, nil
}

func normalizeLMXAddresses(addresses []uint32) ([]uint32, error) {
	if len(addresses) == 0 {
		all := make([]uint32, lmxRegisterCount)
		for address := range all {
			all[address] = uint32(address)
		}
		return all, nil
	}
	seen := make(map[uint32]struct{}, len(addresses))
	for _, address := range addresses {
		if address >= lmxRegisterCount {
			return nil, fmt.Errorf("invalid LMX2595 register R%d (expected R0..R112)", address)
		}
		seen[address] = struct{}{}
	}
	normalized := make([]uint32, 0, len(seen))
	for address := range seen {
		normalized = append(normalized, address)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i] < normalized[j] })
	return normalized, nil
}

func PreviewNetwork(address string) (NetworkPlan, error) {
	ip, gateway, subnet, err := client.DeriveCustomerNetwork(strings.TrimSpace(address))
	if err != nil {
		return NetworkPlan{}, err
	}
	return NetworkPlan{IPAddress: ipString(ip), Gateway: ipString(gateway), Subnet: ipString(subnet)}, nil
}

func (s *Service) SetIPAddress(address string) (NetworkChangeResult, error) {
	plan, err := PreviewNetwork(address)
	if err != nil {
		return NetworkChangeResult{}, err
	}
	ip, gateway, subnet, _ := client.DeriveCustomerNetwork(plan.IPAddress)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil {
		return NetworkChangeResult{}, fmt.Errorf("no device is connected")
	}
	cfg, err := s.active.client.GetConfig()
	if err != nil {
		return NetworkChangeResult{}, fmt.Errorf("read current network configuration: %w", err)
	}
	err = s.active.client.SaveConfig(&pb.SaveConfigRequest{
		StaticIp: ip, StaticGateway: gateway, StaticSubnet: subnet,
		MdnsHostname: cfg.GetMdnsHostname(), EnableGatewayCheck: cfg.GetEnableGatewayCheck(),
		MacAddress: append([]byte(nil), cfg.GetMacAddress()...), SerialNumber: cfg.GetSerialNumber(),
	})
	if err != nil {
		return NetworkChangeResult{}, err
	}
	_ = s.active.client.Close()
	s.active = nil
	return NetworkChangeResult{Plan: plan, Rebooting: true}, nil
}

func (s *Service) Close() { s.Disconnect() }

func (s *Service) refreshLocked() (DeviceSnapshot, error) {
	status, err := s.active.client.GetStatus()
	if err != nil {
		return DeviceSnapshot{}, err
	}
	s.active.status = status
	return s.snapshotLocked(), nil
}

func (s *Service) requireBarracudaLocked() error {
	if s.active == nil {
		return fmt.Errorf("no device is connected")
	}
	if s.active.status.GetBoardType() != "barracuda" {
		return fmt.Errorf("Barracuda controls are unavailable for %s", boardLabel(s.active.status.GetBoardType()))
	}
	return nil
}

func (s *Service) requireWhalepodLocked() error {
	if s.active == nil {
		return fmt.Errorf("no device is connected")
	}
	if s.active.status.GetBoardType() != "whalepod" {
		return fmt.Errorf("Whalepod controls are unavailable for %s", boardLabel(s.active.status.GetBoardType()))
	}
	return nil
}

func (s *Service) requireAirsharkLocked() error {
	if s.active == nil {
		return fmt.Errorf("no device is connected")
	}
	if s.active.status.GetBoardType() != "straps" {
		return fmt.Errorf("Airshark controls are unavailable for %s", boardLabel(s.active.status.GetBoardType()))
	}
	return nil
}

func (s *Service) snapshotLocked() DeviceSnapshot {
	if s.active == nil {
		return DeviceSnapshot{}
	}
	return DeviceSnapshot{
		Connected: true, Endpoint: s.active.endpoint,
		Network:         networkFromConfig(s.active.config),
		Status:          statusFromResponse(s.active),
		CustomerControl: s.active.status.GetBoardType() == "barracuda" || s.active.status.GetBoardType() == "whalepod" || s.active.status.GetBoardType() == "straps",
	}
}

func statusFromResponse(current *session) DeviceStatus {
	status := current.status
	board := status.GetBoardType()
	out := DeviceStatus{
		BoardType: board, BoardLabel: boardLabel(board), Barracuda: board == "barracuda", Whalepod: board == "whalepod", Airshark: board == "straps",
		AttenuationDB:         float64(status.GetAttenuationDb()),
		TemperatureAvailable:  status.GetMcuTemperatureC() != 0,
		TemperatureC:          float64(status.GetMcuTemperatureC()),
		TemperatureBootSample: status.GetMcuTemperatureIsBootSample(),
		ChannelsEnabled:       status.GetChannelsEnabled(), CalibrationEnabled: status.GetCalibrationEnabled(),
		CalSourceInternal: status.GetCalSourceInternal(), CalAttenuationDB: status.GetCalAttenuationDb(),
	}
	if board != "barracuda" {
		out.LOFrequencyMHz = status.GetLoFrequencyMhz()
		out.RFSwitch = status.GetRfSwitch().String()
		out.MixerSwitch = status.GetMixerSwitch().String()
		out.IFSwitch = status.GetIfSwitch().String()
		out.RFSwitchChannel = status.GetRfSwitchChannel()
		if board == "straps" {
			out.AirsharkBand = airsharkBandFromStatus(status)
		}
		return out
	}
	out.Clock = "internal"
	if status.GetClockSourceExternal() {
		out.Clock = "external"
		out.ReferenceLockApplicable = true
		out.ReferenceLocked = status.GetRefLocked()
	}
	out.SignalLockApplicable = true
	out.SignalLocked = status.GetPllLocked()
	details := status.GetBarracuda()
	out.RFEnabled = details != nil && details.GetLmxRequestedFrequencyHz() != 0
	customerPlan := details != nil &&
		details.GetLmxRequestedFrequencyHz() == uint64(client.BarracudaFixedLOMHz)*1_000_000 &&
		details.GetLmxOutputPowerCode() == client.BarracudaCalibratedLMXPowerCode
	if current.lastAtt != nil {
		out.AttenuationDB = *current.lastAtt
	}
	out.MaximumAttenuation = out.AttenuationDB >= client.BarracudaMaxAttenuationDB
	if customerPlan {
		out.OutputEstimateAvailable = true
		out.NominalOutputDBm = client.BarracudaNominalOutputDBm - out.AttenuationDB
		if adf := details.GetAdfState(); adf != nil {
			out.Mode = "cw"
			if adf.GetRampEnabled() {
				out.Mode = "sweep"
			}
			frequency := adf.GetFrequencyMhz() - client.BarracudaFixedLOMHz
			if frequency >= client.BarracudaMinIFFrequencyMHz && frequency <= client.BarracudaMaxIFFrequencyMHz {
				out.IFFrequencyMHz = frequency
			}
		}
	}
	if current.lastMode != "" {
		out.Mode = current.lastMode
		out.IFFrequencyMHz = current.lastStart
		out.SweepStopIFMHz = current.lastStop
		out.SweepTime = current.lastTime
	}
	return out
}

func networkFromConfig(cfg *pb.GetConfigResponse) NetworkConfig {
	return NetworkConfig{
		IPAddress: ipString(cfg.GetStaticIp()), Gateway: ipString(cfg.GetStaticGateway()),
		Subnet: ipString(cfg.GetStaticSubnet()), Hostname: cfg.GetMdnsHostname(),
		MACAddress: macString(cfg.GetMacAddress()), Serial: cfg.GetSerialNumber(),
		Firmware: cfg.GetFirmwareVersion(), BoardID: cfg.GetUniqueBoardId(),
	}
}

func discoveredFromResponses(endpoint Endpoint, cfg *pb.GetConfigResponse, status *pb.GetStatusResponse) DiscoveredDevice {
	return DiscoveredDevice{
		Name: cfg.GetMdnsHostname(), BoardType: status.GetBoardType(), Serial: cfg.GetSerialNumber(),
		Firmware: cfg.GetFirmwareVersion(), IPAddress: ipString(cfg.GetStaticIp()),
		MACAddress: macString(cfg.GetMacAddress()), Connections: []Endpoint{endpoint},
	}
}

func discoveredFromSnapshot(snapshot DeviceSnapshot) DiscoveredDevice {
	return DiscoveredDevice{
		Name: snapshot.Network.Hostname, BoardType: snapshot.Status.BoardType,
		Serial: snapshot.Network.Serial, Firmware: snapshot.Network.Firmware,
		IPAddress: snapshot.Network.IPAddress, MACAddress: snapshot.Network.MACAddress,
		Connections: []Endpoint{snapshot.Endpoint},
	}
}

func deviceKey(serial, mac, ip string, endpoint Endpoint) string {
	if serial != "" {
		return "serial:" + strings.ToLower(serial)
	}
	if mac != "" && mac != "<unset>" {
		return "mac:" + strings.ToLower(mac)
	}
	if ip != "" && ip != "<unset>" {
		return "ip:" + ip
	}
	return endpoint.Kind + ":" + endpoint.Address
}

func appendEndpoint(endpoints []Endpoint, candidate Endpoint) []Endpoint {
	for _, endpoint := range endpoints {
		if endpoint == candidate {
			return endpoints
		}
	}
	return append(endpoints, candidate)
}

func externalClock(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "internal", "int", "":
		return false, nil
	case "external", "ext":
		return true, nil
	default:
		return false, fmt.Errorf("clock must be internal or external")
	}
}

var airsharkBands = map[string]pb.RfBand{
	"10-900":    pb.RfBand_RF_BAND_10_900MHZ,
	"0-900":     pb.RfBand_RF_BAND_10_900MHZ,
	"900-1800":  pb.RfBand_RF_BAND_900_1800MHZ,
	"1800-2700": pb.RfBand_RF_BAND_1800_2700MHZ,
	"2700-3600": pb.RfBand_RF_BAND_2700_3600MHZ,
	"3600-4500": pb.RfBand_RF_BAND_3600_4500MHZ,
}

func parseAirsharkBand(value string) (pb.RfBand, error) {
	key := strings.ToLower(strings.TrimSpace(value))
	key = strings.TrimSuffix(key, "mhz")
	key = strings.TrimSpace(key)
	if band, ok := airsharkBands[key]; ok {
		return band, nil
	}
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "RF_BAND_10_900MHZ":
		return pb.RfBand_RF_BAND_10_900MHZ, nil
	case "RF_BAND_900_1800MHZ":
		return pb.RfBand_RF_BAND_900_1800MHZ, nil
	case "RF_BAND_1800_2700MHZ":
		return pb.RfBand_RF_BAND_1800_2700MHZ, nil
	case "RF_BAND_2700_3600MHZ":
		return pb.RfBand_RF_BAND_2700_3600MHZ, nil
	case "RF_BAND_3600_4500MHZ":
		return pb.RfBand_RF_BAND_3600_4500MHZ, nil
	}
	return 0, fmt.Errorf("Airshark RF band must be 10-900, 900-1800, 1800-2700, 2700-3600, or 3600-4500 MHz")
}

func airsharkBandFromStatus(status *pb.GetStatusResponse) string {
	type preset struct {
		lo    int32
		rf    pb.RfSwitchOption
		mixer pb.MixerSwitchOption
		ifSw  pb.IfSwitchOption
	}
	presets := map[string]preset{
		"10-900":    {0, pb.RfSwitchOption_RF_SWITCH_OPTION_2GHZ_LPF, pb.MixerSwitchOption_MIXER_SWITCH_OPTION_BYPASS, pb.IfSwitchOption_IF_SWITCH_OPTION_900MHZ_LPF},
		"900-1800":  {1800, pb.RfSwitchOption_RF_SWITCH_OPTION_2GHZ_LPF, pb.MixerSwitchOption_MIXER_SWITCH_OPTION_MIXER, pb.IfSwitchOption_IF_SWITCH_OPTION_900MHZ_LPF},
		"1800-2700": {3500, pb.RfSwitchOption_RF_SWITCH_OPTION_4GHZ_LPF, pb.MixerSwitchOption_MIXER_SWITCH_OPTION_MIXER, pb.IfSwitchOption_IF_SWITCH_OPTION_1_2GHZ_BANDPASS},
		"2700-3600": {4300, pb.RfSwitchOption_RF_SWITCH_OPTION_4GHZ_LPF, pb.MixerSwitchOption_MIXER_SWITCH_OPTION_MIXER, pb.IfSwitchOption_IF_SWITCH_OPTION_1_2GHZ_BANDPASS},
		"3600-4500": {5100, pb.RfSwitchOption_RF_SWITCH_OPTION_4GHZ_LPF, pb.MixerSwitchOption_MIXER_SWITCH_OPTION_MIXER, pb.IfSwitchOption_IF_SWITCH_OPTION_1_2GHZ_BANDPASS},
	}
	for name, preset := range presets {
		if status.GetLoFrequencyMhz() == preset.lo && status.GetRfSwitch() == preset.rf &&
			status.GetMixerSwitch() == preset.mixer && status.GetIfSwitch() == preset.ifSw {
			return name
		}
	}
	return ""
}

func boardLabel(board string) string {
	switch board {
	case "barracuda":
		return "Barracuda"
	case "whalepod":
		return "Whalepod"
	case "whalepod_automation":
		return "Whalepod Automation"
	case "straps":
		return "Airshark"
	case "bc":
		return "Black Canyon"
	case "rf_switch":
		return "RF Switch"
	case "":
		return "Unknown device"
	default:
		return board
	}
}

func ipString(value []byte) string {
	if len(value) != net.IPv4len {
		return ""
	}
	return net.IP(value).String()
}

func macString(value []byte) string {
	if len(value) != 6 {
		return ""
	}
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", value[0], value[1], value[2], value[3], value[4], value[5])
}

func floatPointer(value float64) *float64 { return &value }
