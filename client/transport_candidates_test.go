package client

import (
	"reflect"
	"testing"

	"go.bug.st/serial/enumerator"
)

func TestMatchingDevicePorts(t *testing.T) {
	details := []*enumerator.PortDetails{
		{Name: "COM9", IsUSB: true, VID: "1234", PID: "5678"},
		{Name: "COM5", IsUSB: true, VID: "2e8a", PID: "000a"},
		{Name: "COM4", IsUSB: true, VID: "2E8A", PID: "000A"},
		{Name: "COM4", IsUSB: true, VID: "2E8A", PID: "000A"},
		{Name: "COM1", IsUSB: false},
	}
	want := []string{"COM4", "COM5"}
	if got := matchingDevicePorts(details); !reflect.DeepEqual(got, want) {
		t.Fatalf("matchingDevicePorts() = %v, want %v", got, want)
	}
}

func TestMatchingDevicePortsDistinguishesNoIDsFromNoMatch(t *testing.T) {
	if got := matchingDevicePorts([]*enumerator.PortDetails{{Name: "COM1"}}); got != nil {
		t.Fatalf("ports without USB IDs = %v, want nil fallback marker", got)
	}
	got := matchingDevicePorts([]*enumerator.PortDetails{{Name: "COM8", IsUSB: true, VID: "1234", PID: "5678"}})
	if got == nil || len(got) != 0 {
		t.Fatalf("non-matching USB IDs = %v, want non-nil empty result", got)
	}
}
