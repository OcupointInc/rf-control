package mockfirmware

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	pb "github.com/OcupointInc/rf-control/controlpb"
	"google.golang.org/protobuf/proto"
)

const (
	mockBarracudaLOMHz        = int32(9600)
	mockBarracudaLMXPowerCode = uint32(50)
)

// Barracuda is a stateful loopback simulator for GUI development without RF
// hardware. It implements the customer CW/sweep controls and live lock status.
type Barracuda struct {
	listener net.Listener
	mu       sync.Mutex
	config   *pb.GetConfigResponse
	status   *pb.GetStatusResponse
	calls    []string
}

func ListenBarracuda(address string) (*Barracuda, error) {
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, err
	}
	return &Barracuda{
		listener: listener,
		config: &pb.GetConfigResponse{
			StaticIp: []byte{127, 0, 0, 1}, StaticGateway: []byte{127, 0, 0, 1},
			StaticSubnet: []byte{255, 0, 0, 0}, MdnsHostname: "barracuda-mock",
			MacAddress: []byte{0x02, 0, 0, 0, 0, 1}, SerialNumber: "BARR-MOCK-001",
			FirmwareVersion: "mock-1.0.0", UniqueBoardId: "MOCK-BARRACUDA",
		},
		status: &pb.GetStatusResponse{
			BoardType: "barracuda", PllLocked: true, McuTemperatureC: 32,
			Barracuda: &pb.BarracudaDiagnostics{
				LmxRequestedFrequencyHz: uint64(mockBarracudaLOMHz) * 1_000_000,
				LmxActualFrequencyHz:    uint64(mockBarracudaLOMHz) * 1_000_000,
				LmxLocked:               true,
				LmxLockState:            2,
				LmxOutputPowerCode:      mockBarracudaLMXPowerCode,
				AdfState: &pb.Adf4159State{
					Ready: true, FrequencyMhz: mockBarracudaLOMHz + 400,
					LockDetectAvailable: true,
				},
			},
		},
	}, nil
}

func (m *Barracuda) Addr() net.Addr { return m.listener.Addr() }
func (m *Barracuda) Close() error   { return m.listener.Close() }

func (m *Barracuda) Status() *pb.GetStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return proto.Clone(m.status).(*pb.GetStatusResponse)
}

func (m *Barracuda) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

func (m *Barracuda) Serve() error {
	for {
		connection, err := m.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		go m.handle(connection)
	}
}

func (m *Barracuda) handle(connection net.Conn) {
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	raw := make([]byte, 4096)
	n, err := connection.Read(raw)
	if err != nil {
		return
	}
	request := &pb.Packet{}
	if proto.Unmarshal(raw[:n], request) != nil {
		return
	}

	m.mu.Lock()
	response := &pb.Packet{}
	switch message := request.MessageId.(type) {
	case *pb.Packet_GetConfigRequest:
		response.MessageId = &pb.Packet_GetConfigResponse{GetConfigResponse: proto.Clone(m.config).(*pb.GetConfigResponse)}
	case *pb.Packet_GetStatusRequest:
		response.MessageId = &pb.Packet_GetStatusResponse{GetStatusResponse: proto.Clone(m.status).(*pb.GetStatusResponse)}
	case *pb.Packet_SetDsaAttenuationRequest:
		m.calls = append(m.calls, "attenuation")
		m.status.AttenuationDb = message.SetDsaAttenuationRequest.GetQuarterDb() / 4
		response.MessageId = &pb.Packet_SetDsaAttenuationResponse{SetDsaAttenuationResponse: &pb.SetDsaAttenuationResponse{}}
	case *pb.Packet_SetClockSourceRequest:
		m.calls = append(m.calls, "clock")
		external := message.SetClockSourceRequest.GetExternal()
		m.status.ClockSourceExternal = external
		m.status.RefLocked = external
		response.MessageId = &pb.Packet_SetClockSourceResponse{SetClockSourceResponse: &pb.SetClockSourceResponse{
			External: external, ReferenceValid: external, ReferenceSelected: external,
			DpllFrequencyLocked: external, DpllPhaseLocked: external,
		}}
	case *pb.Packet_SetLoFrequencyRequest:
		m.calls = append(m.calls, "lmx-frequency")
		frequencyMHz := message.SetLoFrequencyRequest.GetFrequencyMhz()
		details := m.status.Barracuda
		details.LmxRequestedFrequencyHz = uint64(frequencyMHz) * 1_000_000
		details.LmxActualFrequencyHz = details.LmxRequestedFrequencyHz
		details.LmxLocked = frequencyMHz != 0
		if details.LmxLocked {
			details.LmxLockState = 2
		} else {
			details.LmxLockState = 0
		}
		response.MessageId = &pb.Packet_SetLoFrequencyResponse{SetLoFrequencyResponse: &pb.SetLoFrequencyResponse{}}
	case *pb.Packet_SetLmxOutputPowerRequest:
		m.calls = append(m.calls, "lmx-power")
		m.status.Barracuda.LmxOutputPowerCode = message.SetLmxOutputPowerRequest.GetPowerCode()
		response.MessageId = &pb.Packet_SetLmxOutputPowerResponse{SetLmxOutputPowerResponse: &pb.SetLmxOutputPowerResponse{}}
	case *pb.Packet_SetPllFrequencyRequest:
		m.calls = append(m.calls, "adf-cw")
		adf := m.status.Barracuda.AdfState
		adf.FrequencyMhz = message.SetPllFrequencyRequest.GetFrequencyMhz()
		adf.RampEnabled = false
		m.status.PllLocked = true
		response.MessageId = &pb.Packet_SetPllFrequencyResponse{SetPllFrequencyResponse: &pb.SetPllFrequencyResponse{}}
	case *pb.Packet_SetChirpRequest:
		m.calls = append(m.calls, "adf-sweep")
		adf := m.status.Barracuda.AdfState
		adf.FrequencyMhz = message.SetChirpRequest.GetStartFreqMhz()
		adf.RampEnabled = message.SetChirpRequest.GetEnabled()
		adf.RampMode = message.SetChirpRequest.GetMode()
		m.status.PllLocked = true
		response.MessageId = &pb.Packet_SetChirpResponse{SetChirpResponse: &pb.SetChirpResponse{Locked: true}}
	default:
		response.MessageId = &pb.Packet_ErrorResponse{ErrorResponse: &pb.ErrorResponse{
			Code: pb.ErrorCode_ERROR_CODE_UNSUPPORTED, Detail: fmt.Sprintf("mock firmware does not support %T", request.MessageId),
		}}
	}
	m.mu.Unlock()

	if encoded, err := proto.Marshal(response); err == nil {
		_, _ = connection.Write(encoded)
	}
}
