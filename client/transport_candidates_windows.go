package client

import "go.bug.st/serial/enumerator"

func listDevicePortsByUSBID() ([]string, bool) {
	detailed, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, false
	}
	ports := make([]usbPortDetails, 0, len(detailed))
	for _, detail := range detailed {
		if detail == nil {
			continue
		}
		ports = append(ports, usbPortDetails{
			name: detail.Name, isUSB: detail.IsUSB, vid: detail.VID, pid: detail.PID,
		})
	}
	matches := matchingDevicePorts(ports)
	return matches, matches != nil
}
