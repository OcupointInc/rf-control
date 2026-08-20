package gui

import (
	"bytes"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OcupointInc/rf-control/client"
	pb "github.com/OcupointInc/rf-control/controlpb"
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
