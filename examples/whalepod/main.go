// Command whalepod-example shows how to drive a Whalepod board directly
// from Go, using the client package instead of the rf-control CLI.
//
// It walks through a typical calibration measurement: read the current
// config, put the board into calibration mode with the internal noise
// source selected (the noise-source amp only turns on when both of those
// are true), sweep the calibration attenuator, then restore the board to
// its normal through-path state.
//
// Usage:
//
//	go run . --usb /dev/ttyACM1
//	go run . --ip 192.168.1.50
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/OcupointInc/rf-control/client"
)

func main() {
	usb := flag.String("usb", "", "USB serial device, e.g. /dev/ttyACM1 (Linux) or /dev/cu.usbmodem101 (macOS)")
	ip := flag.String("ip", "", "Device IPv4 address (uses TCP port 5000)")
	flag.Parse()

	tx, err := connect(*usb, *ip)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	defer tx.Close()

	c := client.New(tx)

	cfg, err := c.GetConfig()
	if err != nil {
		log.Fatalf("get config: %v", err)
	}
	fmt.Printf("Connected to serial %s (firmware %s), IP %d.%d.%d.%d\n",
		cfg.SerialNumber, cfg.FirmwareVersion,
		cfg.StaticIp[0], cfg.StaticIp[1], cfg.StaticIp[2], cfg.StaticIp[3])

	printStatus(c, "Before calibration")

	// Select the on-board noise source and enter calibration mode. Order
	// doesn't matter — the amp only actually turns on once both are true —
	// but setting the source first means status already reflects the final
	// state as soon as SetCalEnabled(true) returns.
	if err := c.SetCalSource(true /* internal */); err != nil {
		log.Fatalf("set cal source: %v", err)
	}
	if err := c.SetCalEnabled(true); err != nil {
		log.Fatalf("enable calibration mode: %v", err)
	}
	// Give the noise-source amp a moment to settle before taking a reading.
	time.Sleep(50 * time.Millisecond)

	printStatus(c, "Calibrating (internal noise source)")

	fmt.Println("\nSweeping calibration attenuator 0 -> 20 dB:")
	for db := int32(0); db <= 20; db += 5 {
		if err := c.SetCalAttenuation(db); err != nil {
			log.Fatalf("set cal attenuation %d dB: %v", db, err)
		}
		fmt.Printf("  cal attenuation = %2d dB (take your measurement here)\n", db)
		time.Sleep(20 * time.Millisecond)
	}

	// Always leave the board back in its normal through-path state — don't
	// strand it in calibration mode if this program exits early.
	if err := c.SetCalEnabled(false); err != nil {
		log.Fatalf("disable calibration mode: %v", err)
	}
	printStatus(c, "After calibration (through path restored)")
}

func connect(usb, ip string) (client.Transport, error) {
	switch {
	case usb != "":
		return client.NewUSBTransport(usb)
	case ip != "":
		return client.NewTCPTransport(ip, 5000), nil
	default:
		port, err := client.DiscoverUSBPort()
		if err != nil {
			return nil, fmt.Errorf("no --usb or --ip given, and USB auto-discovery failed: %w", err)
		}
		fmt.Printf("[auto] using USB %s\n", port)
		return client.NewUSBTransport(port)
	}
}

func printStatus(c *client.Client, label string) {
	s, err := c.GetStatus()
	if err != nil {
		log.Fatalf("get status: %v", err)
	}
	fmt.Printf("\n%s:\n", label)
	fmt.Printf("  channels enabled    : %v\n", s.ChannelsEnabled)
	fmt.Printf("  calibration enabled : %v\n", s.CalibrationEnabled)
	fmt.Printf("  frontend atten (dB) : %d\n", s.AttenuationDb)
}
