package client

import (
	"bytes"
	"testing"
)

func TestDeriveCustomerNetwork(t *testing.T) {
	ip, gateway, subnet, err := DeriveCustomerNetwork("192.168.50.25")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ip, []byte{192, 168, 50, 25}) ||
		!bytes.Equal(gateway, []byte{192, 168, 50, 1}) ||
		!bytes.Equal(subnet, []byte{255, 255, 255, 0}) {
		t.Fatalf("got ip=%v gateway=%v subnet=%v", ip, gateway, subnet)
	}
	gateway[3] = 99
	if ip[3] != 25 {
		t.Fatal("gateway aliases IP storage")
	}

	for _, address := range []string{
		"not-an-ip", "2001:db8::1", "0.10.20.30", "127.0.0.2", "224.0.0.2",
		"192.168.50.0", "192.168.50.1", "192.168.50.255",
	} {
		if _, _, _, err := DeriveCustomerNetwork(address); err == nil {
			t.Errorf("DeriveCustomerNetwork(%q) succeeded, want error", address)
		}
	}
}
