package gui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OcupointInc/rf-control/client"
	pb "github.com/OcupointInc/rf-control/controlpb"
	"github.com/OcupointInc/rf-control/internal/mockfirmware"
)

type fakeDevice struct {
	config         *pb.GetConfigResponse
	status         *pb.GetStatusResponse
	saved          *pb.SaveConfigRequest
	cw             *client.BarracudaCWConfig
	sweep          *client.BarracudaSweepConfig
	dsa            int32
	loFrequency    int32
	lmxPowerCode   uint32
	lmxReadBatches [][]uint32
	whalepodCalls  []string
	closed         int
	statusDelay    time.Duration
	statusActive   atomic.Int32
	statusMax      atomic.Int32
}

func (f *fakeDevice) Close() error                              { f.closed++; return nil }
func (f *fakeDevice) GetConfig() (*pb.GetConfigResponse, error) { return f.config, nil }
func (f *fakeDevice) GetStatus() (*pb.GetStatusResponse, error) {
	active := f.statusActive.Add(1)
	defer f.statusActive.Add(-1)
	for {
		max := f.statusMax.Load()
		if active <= max || f.statusMax.CompareAndSwap(max, active) {
			break
		}
	}
	time.Sleep(f.statusDelay)
	return f.status, nil
}
func (f *fakeDevice) SaveConfig(request *pb.SaveConfigRequest) error {
	f.saved = request
	return nil
}
func (f *fakeDevice) ConfigureBarracudaCW(config client.BarracudaCWConfig) (*client.BarracudaConfiguration, error) {
	f.cw = &config
	f.status.PllLocked = true
	f.status.ClockSourceExternal = config.ExternalClock
	f.status.RefLocked = config.ExternalClock
	f.status.AttenuationDb = int32(config.AttenuationDB)
	f.status.Barracuda.AdfState = &pb.Adf4159State{FrequencyMhz: client.BarracudaFixedLOMHz + config.IFFrequencyMHz}
	return &client.BarracudaConfiguration{
		Mode: "cw", StartIFMHz: config.IFFrequencyMHz, StopIFMHz: config.IFFrequencyMHz,
		AttenuationDB: config.AttenuationDB, NominalOutputDBm: client.BarracudaNominalOutputDBm - config.AttenuationDB,
		ExternalClock: config.ExternalClock, SignalLocked: true,
	}, nil
}
func (f *fakeDevice) ConfigureBarracudaSweep(config client.BarracudaSweepConfig) (*client.BarracudaConfiguration, error) {
	f.sweep = &config
	return &client.BarracudaConfiguration{
		Mode: "sweep", StartIFMHz: config.StartIFMHz, StopIFMHz: config.StopIFMHz,
		SweepTime: config.SweepTime, AttenuationDB: config.AttenuationDB, SignalLocked: true,
	}, nil
}
func (f *fakeDevice) SetDsaAttenuation(value int32) error { f.dsa = value; return nil }
func (f *fakeDevice) SetLoFrequency(value int32) error {
	f.loFrequency = value
	f.status.Barracuda.LmxRequestedFrequencyHz = uint64(value) * 1_000_000
	if value == 0 {
		f.status.Barracuda.LmxOutputPowerCode = 0
	}
	return nil
}
func (f *fakeDevice) SetLMXOutputPower(value uint32) error {
	f.lmxPowerCode = value
	f.status.Barracuda.LmxOutputPowerCode = value
	return nil
}
func (f *fakeDevice) ReadLMX2595Registers(addresses []uint32) (*pb.Lmx2595RegisterReadResponse, error) {
	f.lmxReadBatches = append(f.lmxReadBatches, append([]uint32(nil), addresses...))
	values := make([]uint32, len(addresses))
	for i, address := range addresses {
		values[i] = 0xA000 + address
	}
	return &pb.Lmx2595RegisterReadResponse{Addresses: append([]uint32(nil), addresses...), Values: values, Success: true}, nil
}
func (f *fakeDevice) SetAttenuation(value int32) error {
	f.whalepodCalls = append(f.whalepodCalls, "attenuation")
	f.status.AttenuationDb = value
	return nil
}
func (f *fakeDevice) SetCalAttenuation(value int32) error {
	f.whalepodCalls = append(f.whalepodCalls, "cal-attenuation")
	f.status.CalAttenuationDb = value
	return nil
}
func (f *fakeDevice) SetChannelsEnabled(value bool) error {
	f.whalepodCalls = append(f.whalepodCalls, "power")
	f.status.ChannelsEnabled = value
	return nil
}
func (f *fakeDevice) SetCalEnabled(value bool) error {
	f.whalepodCalls = append(f.whalepodCalls, "path")
	f.status.CalibrationEnabled = value
	return nil
}
func (f *fakeDevice) SetCalSource(value bool) error {
	f.whalepodCalls = append(f.whalepodCalls, "cal-source")
	f.status.CalSourceInternal = value
	return nil
}
func (f *fakeDevice) SetRfBand(value pb.RfBand) error {
	f.whalepodCalls = append(f.whalepodCalls, "band")
	applyAirsharkPreset(f.status, value)
	return nil
}

func applyAirsharkPreset(status *pb.GetStatusResponse, band pb.RfBand) {
	switch band {
	case pb.RfBand_RF_BAND_10_900MHZ:
		status.LoFrequencyMhz = 0
		status.RfSwitch = pb.RfSwitchOption_RF_SWITCH_OPTION_2GHZ_LPF
		status.MixerSwitch = pb.MixerSwitchOption_MIXER_SWITCH_OPTION_BYPASS
		status.IfSwitch = pb.IfSwitchOption_IF_SWITCH_OPTION_900MHZ_LPF
	case pb.RfBand_RF_BAND_900_1800MHZ:
		status.LoFrequencyMhz = 1800
		status.RfSwitch = pb.RfSwitchOption_RF_SWITCH_OPTION_2GHZ_LPF
		status.MixerSwitch = pb.MixerSwitchOption_MIXER_SWITCH_OPTION_MIXER
		status.IfSwitch = pb.IfSwitchOption_IF_SWITCH_OPTION_900MHZ_LPF
	case pb.RfBand_RF_BAND_1800_2700MHZ:
		status.LoFrequencyMhz = 3500
		status.RfSwitch = pb.RfSwitchOption_RF_SWITCH_OPTION_4GHZ_LPF
		status.MixerSwitch = pb.MixerSwitchOption_MIXER_SWITCH_OPTION_MIXER
		status.IfSwitch = pb.IfSwitchOption_IF_SWITCH_OPTION_1_2GHZ_BANDPASS
	case pb.RfBand_RF_BAND_2700_3600MHZ:
		status.LoFrequencyMhz = 4300
		status.RfSwitch = pb.RfSwitchOption_RF_SWITCH_OPTION_4GHZ_LPF
		status.MixerSwitch = pb.MixerSwitchOption_MIXER_SWITCH_OPTION_MIXER
		status.IfSwitch = pb.IfSwitchOption_IF_SWITCH_OPTION_1_2GHZ_BANDPASS
	case pb.RfBand_RF_BAND_3600_4500MHZ:
		status.LoFrequencyMhz = 5100
		status.RfSwitch = pb.RfSwitchOption_RF_SWITCH_OPTION_4GHZ_LPF
		status.MixerSwitch = pb.MixerSwitchOption_MIXER_SWITCH_OPTION_MIXER
		status.IfSwitch = pb.IfSwitchOption_IF_SWITCH_OPTION_1_2GHZ_BANDPASS
	}
}

func barracudaFake() *fakeDevice {
	return &fakeDevice{
		config: &pb.GetConfigResponse{
			StaticIp: []byte{192, 168, 50, 25}, StaticGateway: []byte{192, 168, 50, 1},
			StaticSubnet: []byte{255, 255, 255, 0}, MdnsHostname: "barracuda-lab",
			MacAddress: []byte{0x02, 0, 0, 0, 0, 1}, SerialNumber: "BAR-001",
			FirmwareVersion: "1.2.3", UniqueBoardId: "0123456789ABCDEF", EnableGatewayCheck: true,
		},
		status: &pb.GetStatusResponse{
			BoardType: "barracuda", PllLocked: true, AttenuationDb: 0, LoFrequencyMhz: 12345,
			Barracuda: &pb.BarracudaDiagnostics{
				LmxRequestedFrequencyHz: uint64(client.BarracudaFixedLOMHz) * 1_000_000,
				LmxOutputPowerCode:      client.BarracudaCalibratedLMXPowerCode,
				AdfState:                &pb.Adf4159State{FrequencyMhz: client.BarracudaFixedLOMHz + 400},
			},
		},
	}
}

func whalepodFake() *fakeDevice {
	return &fakeDevice{
		config: &pb.GetConfigResponse{
			StaticIp: []byte{127, 0, 0, 1}, StaticGateway: []byte{127, 0, 0, 1},
			StaticSubnet: []byte{255, 0, 0, 0}, MdnsHostname: "whalepod-mock",
			MacAddress: []byte{0x02, 0, 0, 0, 0, 2}, SerialNumber: "WHALE-MOCK-001",
			FirmwareVersion: "mock-1.0.0", UniqueBoardId: "MOCK-WHALEPOD",
		},
		status: &pb.GetStatusResponse{
			BoardType: "whalepod", AttenuationDb: 3, CalAttenuationDb: 7,
			ChannelsEnabled: true, CalibrationEnabled: false, CalSourceInternal: true,
		},
	}
}

func airsharkFake() *fakeDevice {
	status := &pb.GetStatusResponse{
		BoardType: "straps", AttenuationDb: 4, CalAttenuationDb: 8,
		ChannelsEnabled: true, CalibrationEnabled: false,
	}
	applyAirsharkPreset(status, pb.RfBand_RF_BAND_900_1800MHZ)
	return &fakeDevice{
		config: &pb.GetConfigResponse{
			StaticIp: []byte{127, 0, 0, 1}, StaticGateway: []byte{127, 0, 0, 1},
			StaticSubnet: []byte{255, 0, 0, 0}, MdnsHostname: "airshark-mock",
			MacAddress: []byte{0x02, 0, 0, 0, 0, 4}, SerialNumber: "AIR-MOCK-001",
			FirmwareVersion: "mock-1.0.0", UniqueBoardId: "MOCK-AIRSHARK",
		},
		status: status,
	}
}

func blackCanyonFake() *fakeDevice {
	return &fakeDevice{
		config: &pb.GetConfigResponse{
			StaticIp: []byte{127, 0, 0, 1}, StaticGateway: []byte{127, 0, 0, 1},
			StaticSubnet: []byte{255, 0, 0, 0}, MdnsHostname: "black-canyon-mock",
			MacAddress: []byte{0x02, 0, 0, 0, 0, 5}, SerialNumber: "BC-MOCK-001",
			FirmwareVersion: "mock-1.0.0", UniqueBoardId: "MOCK-BLACK-CANYON",
		},
		status: &pb.GetStatusResponse{
			BoardType: "bc", AttenuationDb: 3, ChannelsEnabled: true, CalibrationEnabled: false,
		},
	}
}

func serviceWithFake(fake *fakeDevice) *Service {
	service := NewService()
	service.probeUSB = true
	service.open = func(Endpoint) (deviceClient, error) { return fake, nil }
	service.listUSB = func() ([]string, error) { return nil, nil }
	service.discoverEthernet = func(time.Duration) ([]*pb.DiscoveryResponse, error) { return nil, nil }
	return service
}

func TestPreviewNetwork(t *testing.T) {
	plan, err := PreviewNetwork("192.168.60.44")
	if err != nil {
		t.Fatal(err)
	}
	if plan.IPAddress != "192.168.60.44" || plan.Gateway != "192.168.60.1" || plan.Subnet != "255.255.255.0" {
		t.Fatalf("plan = %+v", plan)
	}
	if _, err := PreviewNetwork("192.168.60.1"); err == nil {
		t.Fatal("gateway-conflicting address succeeded")
	}
}

func TestValidateTuningProfileMatchesCLIApplyShape(t *testing.T) {
	profile := TuningProfile{Barracuda: &BarracudaTuningProfile{
		Mode: "cw", IFFrequencyMHz: 400, AttenuationDB: 6.25, Clock: "internal", RFEnabled: false,
	}}
	if err := ValidateTuningProfile(profile); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range [][]byte{[]byte(`"barracuda"`), []byte(`"if_frequency_mhz":400`), []byte(`"rf_enabled":false`)} {
		if !bytes.Contains(encoded, field) {
			t.Fatalf("profile JSON %s is missing %s", encoded, field)
		}
	}
	profile.Barracuda.AttenuationDB = 6.1
	if err := ValidateTuningProfile(profile); err == nil {
		t.Fatal("non-quarter-dB attenuation was accepted")
	}
}

func TestValidateWhalepodProfileMatchesCLIApplyShape(t *testing.T) {
	attenuation, calAttenuation := int32(12), int32(5)
	channels, calEnabled, internal := true, true, false
	profile := TuningProfile{
		AttenuationDB: &attenuation, CalAttenuationDB: &calAttenuation,
		ChannelsEnabled: &channels, CalibrationEnabled: &calEnabled, CalSourceInternal: &internal,
	}
	if err := ValidateTuningProfile(profile); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range [][]byte{
		[]byte(`"attenuation_db":12`), []byte(`"cal_attenuation_db":5`),
		[]byte(`"channels_enabled":true`), []byte(`"cal_enabled":true`),
		[]byte(`"cal_source_internal":false`),
	} {
		if !bytes.Contains(encoded, field) {
			t.Fatalf("profile JSON %s is missing %s", encoded, field)
		}
	}
	attenuation = 32
	if err := ValidateTuningProfile(profile); err == nil {
		t.Fatal("out-of-range Whalepod attenuation was accepted")
	}
	profile.Barracuda = &BarracudaTuningProfile{Mode: "cw", IFFrequencyMHz: 400, Clock: "internal"}
	if err := ValidateTuningProfile(profile); err == nil {
		t.Fatal("mixed Barracuda and Whalepod profile was accepted")
	}
}

func TestConfigureWhalepodAppliesCompleteStateWithSafePowerOrder(t *testing.T) {
	fake := whalepodFake()
	service := serviceWithFake(fake)
	snapshot, err := service.Connect(Endpoint{Kind: "usb", Address: "COM7"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.CustomerControl || !snapshot.Status.Whalepod {
		t.Fatalf("Whalepod did not receive customer controls: %+v", snapshot)
	}

	snapshot, err = service.ConfigureWhalepod(WhalepodRequest{
		AttenuationDB: 12, CalAttenuationDB: 5, ChannelsEnabled: false,
		CalibrationEnabled: true, CalSourceInternal: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOff := []string{"power", "attenuation", "cal-attenuation", "cal-source", "path"}
	if fmt.Sprint(fake.whalepodCalls) != fmt.Sprint(wantOff) {
		t.Fatalf("power-off call order = %v, want %v", fake.whalepodCalls, wantOff)
	}
	if snapshot.Status.ChannelsEnabled || !snapshot.Status.CalibrationEnabled || snapshot.Status.CalSourceInternal ||
		snapshot.Status.AttenuationDB != 12 || snapshot.Status.CalAttenuationDB != 5 {
		t.Fatalf("applied Whalepod state = %+v", snapshot.Status)
	}

	fake.whalepodCalls = nil
	_, err = service.ConfigureWhalepod(WhalepodRequest{
		AttenuationDB: 8, CalAttenuationDB: 4, ChannelsEnabled: true,
		CalibrationEnabled: false, CalSourceInternal: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOn := []string{"attenuation", "cal-attenuation", "cal-source", "path", "power"}
	if fmt.Sprint(fake.whalepodCalls) != fmt.Sprint(wantOn) {
		t.Fatalf("power-on call order = %v, want %v", fake.whalepodCalls, wantOn)
	}
}

func TestWhalepodGUIServiceAgainstMockFirmware(t *testing.T) {
	firmware, err := mockfirmware.ListenWhalepod("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = firmware.Serve() }()
	t.Cleanup(func() { _ = firmware.Close() })
	port := firmware.Addr().(*net.TCPAddr).Port
	service := NewService()
	service.listUSB = func() ([]string, error) { return nil, nil }
	service.discoverEthernet = func(time.Duration) ([]*pb.DiscoveryResponse, error) { return nil, nil }

	snapshot, err := service.Connect(Endpoint{Kind: "ethernet", Address: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status.BoardLabel != "Whalepod" || snapshot.Network.Firmware != "mock-1.0.0" {
		t.Fatalf("mock firmware identity = %+v", snapshot)
	}

	snapshot, err = service.ConfigureWhalepod(WhalepodRequest{
		AttenuationDB: 19, CalAttenuationDB: 11, ChannelsEnabled: true,
		CalibrationEnabled: true, CalSourceInternal: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"attenuation", "cal-attenuation", "cal-source", "path", "power"}
	calls := firmware.Calls()
	if fmt.Sprint(calls) != fmt.Sprint(wantCalls) {
		t.Fatalf("wire requests = %v, want %v", calls, wantCalls)
	}
	if !snapshot.Status.ChannelsEnabled || !snapshot.Status.CalibrationEnabled || snapshot.Status.CalSourceInternal ||
		snapshot.Status.AttenuationDB != 19 || snapshot.Status.CalAttenuationDB != 11 {
		t.Fatalf("wire readback = %+v", snapshot.Status)
	}
}

func TestValidateAirsharkProfileMatchesCLIApplyShape(t *testing.T) {
	band := "2700-3600"
	attenuation, calAttenuation := int32(9), int32(3)
	channels, calEnabled := true, false
	profile := TuningProfile{
		RFBand: &band, AttenuationDB: &attenuation, CalAttenuationDB: &calAttenuation,
		ChannelsEnabled: &channels, CalibrationEnabled: &calEnabled,
	}
	if err := ValidateTuningProfile(profile); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range [][]byte{
		[]byte(`"rf_band":"2700-3600"`), []byte(`"attenuation_db":9`),
		[]byte(`"cal_attenuation_db":3`), []byte(`"channels_enabled":true`),
		[]byte(`"cal_enabled":false`),
	} {
		if !bytes.Contains(encoded, field) {
			t.Fatalf("profile JSON %s is missing %s", encoded, field)
		}
	}
	badBand := "2-20GHz"
	profile.RFBand = &badBand
	if err := ValidateTuningProfile(profile); err == nil {
		t.Fatal("invalid Airshark band was accepted")
	}
}

func TestConfigureAirsharkAppliesCompleteStateWithSafePowerOrder(t *testing.T) {
	fake := airsharkFake()
	service := serviceWithFake(fake)
	snapshot, err := service.Connect(Endpoint{Kind: "usb", Address: "COM8"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.CustomerControl || !snapshot.Status.Airshark || snapshot.Status.BoardLabel != "Airshark" {
		t.Fatalf("Airshark did not receive customer controls: %+v", snapshot)
	}

	snapshot, err = service.ConfigureAirshark(AirsharkRequest{
		Band: "2700-3600", AttenuationDB: 13, CalAttenuationDB: 6,
		ChannelsEnabled: false, CalibrationEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOff := []string{"power", "band", "attenuation", "cal-attenuation", "path"}
	if fmt.Sprint(fake.whalepodCalls) != fmt.Sprint(wantOff) {
		t.Fatalf("power-off call order = %v, want %v", fake.whalepodCalls, wantOff)
	}
	if snapshot.Status.ChannelsEnabled || !snapshot.Status.CalibrationEnabled ||
		snapshot.Status.AttenuationDB != 13 || snapshot.Status.CalAttenuationDB != 6 ||
		snapshot.Status.AirsharkBand != "2700-3600" || snapshot.Status.LOFrequencyMHz != 4300 {
		t.Fatalf("applied Airshark state = %+v", snapshot.Status)
	}

	fake.whalepodCalls = nil
	_, err = service.ConfigureAirshark(AirsharkRequest{
		Band: "3600-4500", AttenuationDB: 7, CalAttenuationDB: 2,
		ChannelsEnabled: true, CalibrationEnabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOn := []string{"band", "attenuation", "cal-attenuation", "path", "power"}
	if fmt.Sprint(fake.whalepodCalls) != fmt.Sprint(wantOn) {
		t.Fatalf("power-on call order = %v, want %v", fake.whalepodCalls, wantOn)
	}
}

func TestAirsharkGUIServiceAgainstMockFirmware(t *testing.T) {
	firmware, err := mockfirmware.ListenAirshark("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = firmware.Serve() }()
	t.Cleanup(func() { _ = firmware.Close() })
	port := firmware.Addr().(*net.TCPAddr).Port
	service := NewService()
	service.listUSB = func() ([]string, error) { return nil, nil }
	service.discoverEthernet = func(time.Duration) ([]*pb.DiscoveryResponse, error) { return nil, nil }

	snapshot, err := service.Connect(Endpoint{Kind: "ethernet", Address: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status.BoardLabel != "Airshark" || snapshot.Network.Firmware != "mock-1.0.0" {
		t.Fatalf("mock firmware identity = %+v", snapshot)
	}

	snapshot, err = service.ConfigureAirshark(AirsharkRequest{
		Band: "1800-2700", AttenuationDB: 17, CalAttenuationDB: 10,
		ChannelsEnabled: true, CalibrationEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"band", "attenuation", "cal-attenuation", "path", "power"}
	if fmt.Sprint(firmware.Calls()) != fmt.Sprint(wantCalls) {
		t.Fatalf("wire requests = %v, want %v", firmware.Calls(), wantCalls)
	}
	if !snapshot.Status.ChannelsEnabled || !snapshot.Status.CalibrationEnabled ||
		snapshot.Status.AttenuationDB != 17 || snapshot.Status.CalAttenuationDB != 10 ||
		snapshot.Status.AirsharkBand != "1800-2700" || snapshot.Status.LOFrequencyMHz != 3500 {
		t.Fatalf("wire readback = %+v", snapshot.Status)
	}
}

func TestValidateBlackCanyonProfileMatchesCLIApplyShape(t *testing.T) {
	attenuation := int32(11)
	channels, calEnabled := true, true
	profile := TuningProfile{
		AttenuationDB: &attenuation, ChannelsEnabled: &channels, CalibrationEnabled: &calEnabled,
	}
	if err := ValidateTuningProfile(profile); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range [][]byte{
		[]byte(`"attenuation_db":11`), []byte(`"channels_enabled":true`), []byte(`"cal_enabled":true`),
	} {
		if !bytes.Contains(raw, expected) {
			t.Fatalf("profile JSON %s does not contain %s", raw, expected)
		}
	}
	for _, unavailable := range [][]byte{[]byte(`"cal_attenuation_db"`), []byte(`"cal_source_internal"`), []byte(`"rf_band"`)} {
		if bytes.Contains(raw, unavailable) {
			t.Fatalf("Black Canyon profile JSON %s contains unavailable field %s", raw, unavailable)
		}
	}
}

func TestConfigureBlackCanyonAppliesCompleteStateWithSafePowerOrder(t *testing.T) {
	fake := blackCanyonFake()
	service := serviceWithFake(fake)
	snapshot, err := service.Connect(Endpoint{Kind: "usb", Address: "COM9"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.CustomerControl || !snapshot.Status.BlackCanyon || snapshot.Status.BoardLabel != "Black Canyon" {
		t.Fatalf("Black Canyon did not receive customer controls: %+v", snapshot)
	}

	snapshot, err = service.ConfigureBlackCanyon(BlackCanyonRequest{
		AttenuationDB: 15, ChannelsEnabled: false, CalibrationEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOff := []string{"power", "attenuation", "path"}
	if fmt.Sprint(fake.whalepodCalls) != fmt.Sprint(wantOff) {
		t.Fatalf("power-off call order = %v, want %v", fake.whalepodCalls, wantOff)
	}
	if snapshot.Status.ChannelsEnabled || !snapshot.Status.CalibrationEnabled || snapshot.Status.AttenuationDB != 15 {
		t.Fatalf("applied Black Canyon state = %+v", snapshot.Status)
	}

	fake.whalepodCalls = nil
	_, err = service.ConfigureBlackCanyon(BlackCanyonRequest{
		AttenuationDB: 6, ChannelsEnabled: true, CalibrationEnabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantOn := []string{"attenuation", "path", "power"}
	if fmt.Sprint(fake.whalepodCalls) != fmt.Sprint(wantOn) {
		t.Fatalf("power-on call order = %v, want %v", fake.whalepodCalls, wantOn)
	}
}

func TestBlackCanyonGUIServiceAgainstMockFirmware(t *testing.T) {
	firmware, err := mockfirmware.ListenBlackCanyon("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = firmware.Serve() }()
	defer firmware.Close()

	port := firmware.Addr().(*net.TCPAddr).Port
	service := NewService()
	snapshot, err := service.Connect(Endpoint{Kind: "ethernet", Address: "127.0.0.1", Port: port})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Status.BoardLabel != "Black Canyon" || !snapshot.Status.BlackCanyon || snapshot.Network.Firmware != "mock-1.0.0" {
		t.Fatalf("mock firmware identity = %+v", snapshot)
	}

	snapshot, err = service.ConfigureBlackCanyon(BlackCanyonRequest{
		AttenuationDB: 21, ChannelsEnabled: true, CalibrationEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantCalls := []string{"attenuation", "path", "power"}
	if fmt.Sprint(firmware.Calls()) != fmt.Sprint(wantCalls) {
		t.Fatalf("wire requests = %v, want %v", firmware.Calls(), wantCalls)
	}
	if !snapshot.Status.ChannelsEnabled || !snapshot.Status.CalibrationEnabled || snapshot.Status.AttenuationDB != 21 {
		t.Fatalf("wire readback = %+v", snapshot.Status)
	}
}

func TestReadLMXRegistersNormalizesAndBatches(t *testing.T) {
	fake := barracudaFake()
	service := serviceWithFake(fake)
	if _, err := service.Connect(Endpoint{Kind: "usb", Address: "COM4"}); err != nil {
		t.Fatal(err)
	}

	requested := []uint32{32, 1, 16, 0, 16, 31, 2, 30, 29, 28, 27, 26, 25, 24, 23, 22, 21, 20, 19, 18, 17}
	result, err := service.ReadLMXRegisters(requested)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Registers) != 20 {
		t.Fatalf("register count = %d, want 20", len(result.Registers))
	}
	if len(fake.lmxReadBatches) != 2 || len(fake.lmxReadBatches[0]) != 16 || len(fake.lmxReadBatches[1]) != 4 {
		t.Fatalf("read batches = %#v", fake.lmxReadBatches)
	}
	for index, register := range result.Registers {
		if register.Address != uint32(index) && !(index >= 3 && register.Address == uint32(index+13)) {
			t.Fatalf("registers are not sorted: %#v", result.Registers)
		}
		if register.Value != 0xA000+register.Address {
			t.Fatalf("R%d value = %#x", register.Address, register.Value)
		}
	}
}

func TestReadAllLMXRegistersUsesCompleteMap(t *testing.T) {
	fake := barracudaFake()
	service := serviceWithFake(fake)
	if _, err := service.Connect(Endpoint{Kind: "usb", Address: "COM4"}); err != nil {
		t.Fatal(err)
	}
	result, err := service.ReadLMXRegisters(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Registers) != 113 || len(fake.lmxReadBatches) != 8 {
		t.Fatalf("registers=%d batches=%d, want 113 and 8", len(result.Registers), len(fake.lmxReadBatches))
	}
	if result.Registers[0].Address != 0 || result.Registers[112].Address != 112 {
		t.Fatalf("register range = R%d..R%d", result.Registers[0].Address, result.Registers[112].Address)
	}
}

func TestReadLMXRegistersRejectsInvalidAddress(t *testing.T) {
	service := serviceWithFake(barracudaFake())
	if _, err := service.ReadLMXRegisters([]uint32{113}); err == nil {
		t.Fatal("R113 was accepted")
	}
}

func TestDiscoverCombinesUSBAndEthernetByMAC(t *testing.T) {
	fake := barracudaFake()
	fake.config.SerialNumber = ""
	service := serviceWithFake(fake)
	service.listUSB = func() ([]string, error) { return []string{"/dev/ttyACM1"}, nil }
	service.discoverEthernet = func(time.Duration) ([]*pb.DiscoveryResponse, error) {
		return []*pb.DiscoveryResponse{{
			Board: "barracuda", FirmwareVersion: "1.2.3", Serial: "0123456789ABCDEF",
			Mac: []byte{0x02, 0, 0, 0, 0, 1}, Ip: []byte{192, 168, 50, 25}, ControlPort: 5000,
		}}, nil
	}
	result := service.Discover()
	if len(result.Devices) != 1 || len(result.Devices[0].Connections) != 2 {
		t.Fatalf("discovery = %+v", result)
	}
	if result.Devices[0].Connections[0].Kind != "usb" || result.Devices[0].Connections[1].Kind != "ethernet" {
		t.Fatalf("connections = %+v", result.Devices[0].Connections)
	}
}

func TestStalledDiscoveryDoesNotBlockDirectConnection(t *testing.T) {
	fake := barracudaFake()
	service := serviceWithFake(fake)
	started := make(chan struct{})
	release := make(chan struct{})
	service.listUSB = func() ([]string, error) {
		close(started)
		<-release
		return nil, nil
	}
	discoveryDone := make(chan struct{})
	go func() {
		service.Discover()
		close(discoveryDone)
	}()
	<-started

	connectDone := make(chan error, 1)
	go func() {
		_, err := service.Connect(Endpoint{Kind: "ethernet", Address: "192.168.0.246", Port: 5000})
		connectDone <- err
	}()
	select {
	case err := <-connectDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("direct connection blocked behind stalled discovery")
	}

	close(release)
	select {
	case <-discoveryDone:
	case <-time.After(time.Second):
		t.Fatal("discovery did not finish after USB enumeration resumed")
	}
}

func TestDiscoverReturnsAtBackendDeadline(t *testing.T) {
	service := serviceWithFake(barracudaFake())
	service.scanTimeout = 25 * time.Millisecond
	release := make(chan struct{})
	service.listUSB = func() ([]string, error) {
		<-release
		return nil, nil
	}

	started := time.Now()
	result := service.Discover()
	if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
		close(release)
		t.Fatalf("discovery exceeded backend deadline: %v", elapsed)
	}
	if !result.TimedOut || len(result.Warnings) != 1 {
		close(release)
		t.Fatalf("timeout result = %+v", result)
	}
	close(release)
}

func TestDiscoverCanListUSBWithoutOpeningPorts(t *testing.T) {
	service := serviceWithFake(barracudaFake())
	service.probeUSB = false
	service.listUSB = func() ([]string, error) { return []string{"COM5"}, nil }
	service.open = func(Endpoint) (deviceClient, error) {
		t.Fatal("discovery opened a COM candidate")
		return nil, nil
	}

	result := service.Discover()
	if result.TimedOut || len(result.Devices) != 1 {
		t.Fatalf("discovery = %+v", result)
	}
	device := result.Devices[0]
	if device.Name != "USB serial device" || len(device.Connections) != 1 ||
		device.Connections[0] != (Endpoint{Kind: "usb", Address: "COM5"}) {
		t.Fatalf("COM candidate = %+v", device)
	}
}

func TestDiscoverIdentifiesResponsiveUSBDevice(t *testing.T) {
	fake := barracudaFake()
	service := serviceWithFake(fake)
	service.listUSB = func() ([]string, error) { return []string{"COM4"}, nil }

	result := service.Discover()
	if len(result.Devices) != 1 {
		t.Fatalf("discovery = %+v", result)
	}
	device := result.Devices[0]
	if device.BoardType != "barracuda" || device.Name != "barracuda-lab" ||
		len(device.Connections) != 1 || device.Connections[0].Address != "COM4" {
		t.Fatalf("identified USB device = %+v", device)
	}
}

func TestDiscoveryResultJSONUsesArraysForEmptyCollections(t *testing.T) {
	service := serviceWithFake(barracudaFake())
	result := service.Discover()
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"devices":[]`)) || !bytes.Contains(encoded, []byte(`"warnings":[]`)) {
		t.Fatalf("empty discovery collections must be JSON arrays, got %s", encoded)
	}
}

func TestBarracudaCWUsesCustomerPlanAndHidesEngineeringState(t *testing.T) {
	fake := barracudaFake()
	service := serviceWithFake(fake)
	snapshot, err := service.Connect(Endpoint{Kind: "usb", Address: "/dev/ttyACM1"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.CustomerControl || snapshot.Status.LOFrequencyMHz != 0 {
		t.Fatalf("initial snapshot exposed the wrong profile: %+v", snapshot.Status)
	}

	snapshot, err = service.ConfigureCW(CWRequest{FrequencyMHz: 900, Attenuation: 6.25, Clock: "external", RFEnabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if fake.cw == nil || fake.cw.IFFrequencyMHz != 900 || fake.cw.AttenuationDB != 6.25 || !fake.cw.ExternalClock {
		t.Fatalf("CW request = %+v", fake.cw)
	}
	status := snapshot.Status
	if status.Mode != "cw" || status.IFFrequencyMHz != 900 || status.AttenuationDB != 6.25 ||
		status.NominalOutputDBm != -31.25 || !status.SignalLocked || !status.ReferenceLocked {
		t.Fatalf("status = %+v", status)
	}
}

func TestConfigureCWAppliesPendingRFStateAtomically(t *testing.T) {
	fake := barracudaFake()
	service := serviceWithFake(fake)
	if _, err := service.Connect(Endpoint{Kind: "usb", Address: "COM4"}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.ConfigureCW(CWRequest{
		FrequencyMHz: 400, Attenuation: 0, Clock: "internal", RFEnabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.cw == nil || fake.loFrequency != 0 || snapshot.Status.RFEnabled {
		t.Fatalf("applied CW with RF off: cw=%+v lo=%d status=%+v", fake.cw, fake.loFrequency, snapshot.Status)
	}
}

func TestSetRFEnabledPowersDownAndRestoresLMX(t *testing.T) {
	fake := barracudaFake()
	service := serviceWithFake(fake)
	if _, err := service.Connect(Endpoint{Kind: "usb", Address: "COM4"}); err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.SetRFEnabled(false)
	if err != nil {
		t.Fatal(err)
	}
	if fake.loFrequency != 0 || snapshot.Status.RFEnabled {
		t.Fatalf("RF off: lo=%d status=%+v", fake.loFrequency, snapshot.Status)
	}

	snapshot, err = service.SetRFEnabled(true)
	if err != nil {
		t.Fatal(err)
	}
	if fake.loFrequency != client.BarracudaFixedLOMHz ||
		fake.lmxPowerCode != client.BarracudaCalibratedLMXPowerCode || !snapshot.Status.RFEnabled {
		t.Fatalf("RF on: lo=%d power=%d status=%+v", fake.loFrequency, fake.lmxPowerCode, snapshot.Status)
	}
}

func TestSetIPAddressPreservesIdentityAndDisconnects(t *testing.T) {
	fake := barracudaFake()
	service := serviceWithFake(fake)
	if _, err := service.Connect(Endpoint{Kind: "usb", Address: "/dev/ttyACM1"}); err != nil {
		t.Fatal(err)
	}
	result, err := service.SetIPAddress("10.20.30.40")
	if err != nil {
		t.Fatal(err)
	}
	if !result.Rebooting || result.Plan.Gateway != "10.20.30.1" {
		t.Fatalf("result = %+v", result)
	}
	if fake.saved == nil || fake.saved.GetMdnsHostname() != "barracuda-lab" ||
		fake.saved.GetSerialNumber() != "BAR-001" || !fake.saved.GetEnableGatewayCheck() {
		t.Fatalf("saved config did not preserve identity: %+v", fake.saved)
	}
	if got := ipString(fake.saved.GetStaticIp()); got != "10.20.30.40" {
		t.Fatalf("saved IP = %s", got)
	}
	if fake.closed != 1 || service.active != nil {
		t.Fatalf("closed=%d active=%v", fake.closed, service.active)
	}
}

func TestStatusRequestsAreSerialized(t *testing.T) {
	fake := barracudaFake()
	fake.statusDelay = 15 * time.Millisecond
	service := serviceWithFake(fake)
	if _, err := service.Connect(Endpoint{Kind: "ethernet", Address: "192.168.50.25", Port: 5000}); err != nil {
		t.Fatal(err)
	}
	fake.statusMax.Store(0)
	var wait sync.WaitGroup
	wait.Add(2)
	for range 2 {
		go func() {
			defer wait.Done()
			if _, err := service.Status(); err != nil {
				t.Errorf("Status: %v", err)
			}
		}()
	}
	wait.Wait()
	if got := fake.statusMax.Load(); got != 1 {
		t.Fatalf("maximum concurrent status calls = %d, want 1", got)
	}
}
