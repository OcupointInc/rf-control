package client

import (
	"fmt"
	"net"
	"sort"
	"time"

	pb "github.com/OcupointInc/rf-control/controlpb"
	"google.golang.org/protobuf/proto"
)

const DiscoveryPort = 5001

// DiscoverEthernet broadcasts the firmware discovery request and collects
// unicast replies until timeout. Devices are de-duplicated by MAC address and
// IP, and the returned list is sorted by IP for stable UI/CLI presentation.
func DiscoverEthernet(timeout time.Duration) ([]*pb.DiscoveryResponse, error) {
	if timeout <= 0 {
		return nil, fmt.Errorf("discovery timeout must be positive")
	}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return nil, fmt.Errorf("open discovery socket: %w", err)
	}
	defer conn.Close()

	payload, err := proto.Marshal(&pb.Packet{MessageId: &pb.Packet_DiscoveryRequest{
		DiscoveryRequest: &pb.DiscoveryRequest{},
	}})
	if err != nil {
		return nil, err
	}
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	if _, err := conn.WriteToUDP(payload, &net.UDPAddr{IP: net.IPv4bcast, Port: DiscoveryPort}); err != nil {
		return nil, fmt.Errorf("broadcast discovery request: %w", err)
	}
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}

	byID := make(map[string]*pb.DiscoveryResponse)
	buffer := make([]byte, 2048)
	for {
		n, _, err := conn.ReadFromUDP(buffer)
		if err != nil {
			if networkError, ok := err.(net.Error); ok && networkError.Timeout() {
				break
			}
			return nil, fmt.Errorf("read discovery response: %w", err)
		}
		packet := &pb.Packet{}
		if err := proto.Unmarshal(buffer[:n], packet); err != nil {
			continue
		}
		response := packet.GetDiscoveryResponse()
		if response == nil || len(response.Ip) != net.IPv4len {
			continue
		}
		key := fmt.Sprintf("%x/%s", response.Mac, net.IP(response.Ip).String())
		byID[key] = response
	}

	devices := make([]*pb.DiscoveryResponse, 0, len(byID))
	for _, device := range byID {
		devices = append(devices, device)
	}
	sort.Slice(devices, func(i, j int) bool {
		return net.IP(devices[i].Ip).String() < net.IP(devices[j].Ip).String()
	})
	return devices, nil
}
