//go:build !windows

package client

func listDevicePortsByUSBID() ([]string, bool) { return nil, false }
