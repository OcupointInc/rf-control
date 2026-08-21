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

// Airshark is a stateful simulator for hardware that identifies itself to the
// shared firmware protocol as board type "straps".
type Airshark struct {
	listener net.Listener
	mu       sync.Mutex
	config   *pb.GetConfigResponse
	status   *pb.GetStatusResponse
	calls    []string
}

func ListenAirshark(address string) (*Airshark, error) {
	listener, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, err
	}
	status := &pb.GetStatusResponse{
		BoardType: "straps", AttenuationDb: 2, CalAttenuationDb: 4,
		ChannelsEnabled: false, CalibrationEnabled: false, McuTemperatureC: 30.5,
	}
	applyAirsharkBand(status, pb.RfBand_RF_BAND_900_1800MHZ)
	return &Airshark{
		listener: listener,
		config: &pb.GetConfigResponse{
			StaticIp: []byte{127, 0, 0, 1}, StaticGateway: []byte{127, 0, 0, 1},
			StaticSubnet: []byte{255, 0, 0, 0}, MdnsHostname: "airshark-mock",
			MacAddress: []byte{0x02, 0, 0, 0, 0, 4}, SerialNumber: "AIR-MOCK-001",
			FirmwareVersion: "mock-1.0.0", UniqueBoardId: "MOCK-AIRSHARK",
		},
		status: status,
	}, nil
}

func (m *Airshark) Addr() net.Addr { return m.listener.Addr() }
func (m *Airshark) Close() error   { return m.listener.Close() }

func (m *Airshark) Status() *pb.GetStatusResponse {
	m.mu.Lock()
	defer m.mu.Unlock()
	return proto.Clone(m.status).(*pb.GetStatusResponse)
}

func (m *Airshark) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.calls...)
}

func (m *Airshark) Serve() error {
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

func (m *Airshark) handle(connection net.Conn) {
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
	case *pb.Packet_SetRfBandRequest:
		m.calls = append(m.calls, "band")
		applyAirsharkBand(m.status, message.SetRfBandRequest.GetBand())
		response.MessageId = &pb.Packet_SetRfBandResponse{SetRfBandResponse: &pb.SetRfBandResponse{}}
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

func applyAirsharkBand(status *pb.GetStatusResponse, band pb.RfBand) {
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
