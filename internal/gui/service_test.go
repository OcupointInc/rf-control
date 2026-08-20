package gui

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/OcupointInc/rf-control/client"
	pb "github.com/OcupointInc/rf-control/controlpb"
)

type fakeDevice struct {
	config       *pb.GetConfigResponse
	status       *pb.GetStatusResponse
	saved        *pb.SaveConfigRequest
	cw           *client.BarracudaCWConfig
	sweep        *client.BarracudaSweepConfig
	dsa          int32
	closed       int
	statusDelay  time.Duration
	statusActive atomic.Int32
	statusMax    atomic.Int32
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

	snapshot, err = service.ConfigureCW(CWRequest{FrequencyMHz: 900, Attenuation: 6.25, Clock: "external"})
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
