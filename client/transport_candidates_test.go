package client

import (
	"reflect"
	"testing"
)

func TestMatchingDevicePorts(t *testing.T) {
	details := []usbPortDetails{
		{name: "COM9", isUSB: true, vid: "1234", pid: "5678"},
		{name: "COM5", isUSB: true, vid: "2e8a", pid: "000a"},
		{name: "COM4", isUSB: true, vid: "2E8A", pid: "000A"},
		{name: "COM4", isUSB: true, vid: "2E8A", pid: "000A"},
		{name: "COM1", isUSB: false},
	}
	want := []string{"COM4", "COM5"}
	if got := matchingDevicePorts(details); !reflect.DeepEqual(got, want) {
		t.Fatalf("matchingDevicePorts() = %v, want %v", got, want)
	}
}

func TestMatchingDevicePortsDistinguishesNoIDsFromNoMatch(t *testing.T) {
	if got := matchingDevicePorts([]usbPortDetails{{name: "COM1"}}); got != nil {
		t.Fatalf("ports without USB IDs = %v, want nil fallback marker", got)
	}
	got := matchingDevicePorts([]usbPortDetails{{name: "COM8", isUSB: true, vid: "1234", pid: "5678"}})
	if got == nil || len(got) != 0 {
		t.Fatalf("non-matching USB IDs = %v, want non-nil empty result", got)
	}
}
