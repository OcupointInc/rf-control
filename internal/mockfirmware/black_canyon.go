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

// BlackCanyon is a stateful simulator for firmware board type "bc".
type BlackCanyon struct {
	listener net.Listener
	mu       sync.Mutex
	config   *pb.GetConfigResponse
	status   *pb.GetStatusResponse
	calls    []string
}

func ListenBlackCanyon(address string) (*BlackCanyon, error) {
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, err
	}
	return &BlackCanyon{
		listener: listener,
		config: &pb.GetConfigResponse{
			StaticIp: []byte{127, 0, 0, 1}, StaticGateway: []byte{127, 0, 0, 1},
			StaticSubnet: []byte{255, 0, 0, 0}, MdnsHostname: "black-canyon-mock",
			MacAddress: []byte{0x02, 0, 0, 0, 0, 5}, SerialNumber: "BC-MOCK-001",
			FirmwareVersion: "mock-1.0.0", UniqueBoardId: "MOCK-BLACK-CANYON",
		},
		status: &pb.GetStatusResponse{
			BoardType: "bc", AttenuationDb: 3, ChannelsEnabled: false,
			CalibrationEnabled: false, McuTemperatureC: 29.5,
		},
	}, nil
}

func (m *BlackCanyon) Addr() net.Addr { return m.listener.Addr() }
func (m *BlackCanyon) Close() error   { return m.listener.Close() }

func (m *BlackCanyon) Status() *pb.GetStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return proto.Clone(m.status).(*pb.GetStatusResponse)
}

func (m *BlackCanyon) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

func (m *BlackCanyon) Serve() error {
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

func (m *BlackCanyon) handle(connection net.Conn) {
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
	case *pb.Packet_SetChannelsEnabledRequest:
		m.calls = append(m.calls, "power")
		m.status.ChannelsEnabled = message.SetChannelsEnabledRequest.GetEnabled()
		response.MessageId = &pb.Packet_SetChannelsEnabledResponse{SetChannelsEnabledResponse: &pb.SetChannelsEnabledResponse{}}
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
