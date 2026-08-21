// Package mockfirmware provides local, stateful firmware simulators for GUI
// development and end-to-end transport tests. They speak the same protobuf TCP
// protocol as the hardware; no GUI-specific shortcuts are involved.
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

// Whalepod is a stateful Whalepod firmware simulator.
type Whalepod struct {
	listener net.Listener
	mu       sync.Mutex
	config   *pb.GetConfigResponse
	status   *pb.GetStatusResponse
	calls    []string
}

// ListenWhalepod creates a simulator on address. Use 127.0.0.1:0 in tests to
// select a free port, or 127.0.0.1:5000 for manual GUI testing.
func ListenWhalepod(address string) (*Whalepod, error) {
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, err
	}
	return &Whalepod{
		listener: listener,
		config: &pb.GetConfigResponse{
			StaticIp: []byte{127, 0, 0, 1}, StaticGateway: []byte{127, 0, 0, 1},
			StaticSubnet: []byte{255, 0, 0, 0}, MdnsHostname: "whalepod-mock",
			MacAddress: []byte{0x02, 0, 0, 0, 0, 3}, SerialNumber: "WHALE-MOCK-001",
			FirmwareVersion: "mock-1.0.0", UniqueBoardId: "MOCK-WHALEPOD",
		},
		status: &pb.GetStatusResponse{
			BoardType: "whalepod", AttenuationDb: 2, CalAttenuationDb: 4,
			ChannelsEnabled: false, CalibrationEnabled: false, CalSourceInternal: true,
			McuTemperatureC: 31.5,
		},
	}, nil
}

func (m *Whalepod) Addr() net.Addr { return m.listener.Addr() }
func (m *Whalepod) Close() error   { return m.listener.Close() }

// Status returns a copy of the simulated live state.
func (m *Whalepod) Status() *pb.GetStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return proto.Clone(m.status).(*pb.GetStatusResponse)
}

// Calls returns the ordered setter operations received by the simulator.
func (m *Whalepod) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

// Serve accepts requests until Close is called or the listener fails.
func (m *Whalepod) Serve() error {
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

func (m *Whalepod) handle(connection net.Conn) {
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
	case *pb.Packet_SetFrontendAttenuationRequest:
		m.calls = append(m.calls, "attenuation")
		m.status.AttenuationDb = message.SetFrontendAttenuationRequest.GetAttenuationDb()
		response.MessageId = &pb.Packet_SetFrontendAttenuationResponse{SetFrontendAttenuationResponse: &pb.SetFrontendAttenuationResponse{}}
	case *pb.Packet_SetCalAttenuationRequest:
		m.calls = append(m.calls, "cal-attenuation")
		m.status.CalAttenuationDb = message.SetCalAttenuationRequest.GetAttenuationDb()
		response.MessageId = &pb.Packet_SetCalAttenuationResponse{SetCalAttenuationResponse: &pb.SetCalAttenuationResponse{}}
	case *pb.Packet_SetChannelsEnabledRequest:
		m.calls = append(m.calls, "power")
		m.status.ChannelsEnabled = message.SetChannelsEnabledRequest.GetEnabled()
		response.MessageId = &pb.Packet_SetChannelsEnabledResponse{SetChannelsEnabledResponse: &pb.SetChannelsEnabledResponse{}}
	case *pb.Packet_SetCalSourceRequest:
		m.calls = append(m.calls, "cal-source")
		m.status.CalSourceInternal = message.SetCalSourceRequest.GetInternal()
		response.MessageId = &pb.Packet_SetCalSourceResponse{SetCalSourceResponse: &pb.SetCalSourceResponse{}}
	case *pb.Packet_SetCalEnabledRequest:
		m.calls = append(m.calls, "path")
		m.status.CalibrationEnabled = message.SetCalEnabledRequest.GetEnabled()
		response.MessageId = &pb.Packet_SetCalEnabledResponse{SetCalEnabledResponse: &pb.SetCalibrationEnabledResponse{}}
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
