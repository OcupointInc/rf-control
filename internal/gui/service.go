// Package gui contains the transport-agnostic application service used by the
// Wails desktop frontend. It deliberately exposes only customer-safe
// Barracuda operations plus common discovery, status, and network management.
package gui

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/OcupointInc/rf-control/client"
	pb "github.com/OcupointInc/rf-control/controlpb"
)

const defaultControlPort = 5000

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
}

type SweepRequest struct {
	StartMHz    int32   `json:"startMHz"`
	StopMHz     int32   `json:"stopMHz"`
	SweepTime   string  `json:"sweepTime"`
	Attenuation float64 `json:"attenuation"`
	Clock       string  `json:"clock"`
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

type deviceClient interface {
	Close() error
	GetConfig() (*pb.GetConfigResponse, error)
	GetStatus() (*pb.GetStatusResponse, error)
	SaveConfig(*pb.SaveConfigRequest) error
	ConfigureBarracudaCW(client.BarracudaCWConfig) (*client.BarracudaConfiguration, error)
	ConfigureBarracudaSweep(client.BarracudaSweepConfig) (*client.BarracudaConfiguration, error)
	SetDsaAttenuation(int32) error
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
}

func NewService() *Service {
	return &Service{
		open:             openDevice,
		listUSB:          client.ListCandidatePorts,
		discoverEthernet: client.DiscoverEthernet,
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
	s.mu.Lock()
	defer s.mu.Unlock()

	byID := make(map[string]*DiscoveredDevice)
	var warnings []string
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

	activeUSB := ""
	if s.active != nil {
		add(discoveredFromSnapshot(s.snapshotLocked()))
		if s.active.endpoint.Kind == "usb" {
			activeUSB = s.active.endpoint.Address
		}
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

func (s *Service) snapshotLocked() DeviceSnapshot {
	if s.active == nil {
		return DeviceSnapshot{}
	}
	return DeviceSnapshot{
		Connected: true, Endpoint: s.active.endpoint,
		Network:         networkFromConfig(s.active.config),
		Status:          statusFromResponse(s.active),
		CustomerControl: s.active.status.GetBoardType() == "barracuda",
	}
}

func statusFromResponse(current *session) DeviceStatus {
	status := current.status
	board := status.GetBoardType()
	out := DeviceStatus{
		BoardType: board, BoardLabel: boardLabel(board), Barracuda: board == "barracuda",
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

func boardLabel(board string) string {
	switch board {
	case "barracuda":
		return "Barracuda"
	case "whalepod":
		return "Whalepod"
	case "whalepod_automation":
		return "Whalepod Automation"
	case "straps":
		return "STRAPS"
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
