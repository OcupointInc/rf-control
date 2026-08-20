package client

import (
	"fmt"
	"net"
)

// DeriveCustomerNetwork returns the simplified customer network plan for a
// device address: a /24 subnet and a gateway at A.B.C.1. The device itself must
// use host .2 through .254 so it cannot collide with the derived gateway,
// network, or broadcast address.
func DeriveCustomerNetwork(address string) (ip, gateway, subnet []byte, err error) {
	parsed := net.ParseIP(address)
	if parsed == nil || parsed.To4() == nil {
		return nil, nil, nil, fmt.Errorf("not an IPv4 address: %q", address)
	}
	ip = append([]byte(nil), parsed.To4()...)
	if ip[0] == 0 || ip[0] == 127 || ip[0] >= 224 {
		return nil, nil, nil, fmt.Errorf("customer IP must be a unicast address (got %q)", address)
	}
	switch ip[3] {
	case 0:
		return nil, nil, nil, fmt.Errorf("customer IP %q is the derived /24 network address; choose host .2 through .254", address)
	case 1:
		return nil, nil, nil, fmt.Errorf("customer IP %q conflicts with the derived .1 gateway; choose host .2 through .254", address)
	case 255:
		return nil, nil, nil, fmt.Errorf("customer IP %q is the derived /24 broadcast address; choose host .2 through .254", address)
	}
	gateway = append([]byte(nil), ip...)
	gateway[3] = 1
	subnet = []byte{255, 255, 255, 0}
	return ip, gateway, subnet, nil
}
