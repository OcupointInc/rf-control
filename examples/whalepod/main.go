// Command whalepod-example shows how to drive a Whalepod board directly
// from Go with the client package: build up a client.Whalepod, set the
// fields you want, and Write() them to the device.
//
// It runs a typical calibration sweep — enter calibration mode with the
// internal noise source, step the calibration attenuator, then restore the
// normal through path — reusing one Whalepod object throughout.
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

	wp := client.NewWhalepod(tx)
	defer wp.Close()

	cfg, err := wp.GetConfig()
	if err != nil {
		log.Fatalf("get config: %v", err)
	}
	fmt.Printf("Connected to serial %s (firmware %s)\n", cfg.SerialNumber, cfg.FirmwareVersion)

	// Load the current channel/attenuation/cal-mode state so we only change
	// what we mean to below (Write pushes every field).
	if err := wp.Read(); err != nil {
		log.Fatalf("read status: %v", err)
	}

	// Enter calibration mode with the internal noise source. The noise-source
	// amp only turns on when cal mode and the internal source are both set.
	wp.CalSourceInternal = true
	wp.CalEnabled = true
	wp.CalAttenuation = 0
	if err := wp.Write(); err != nil {
		log.Fatalf("apply calibration settings: %v", err)
	}
	time.Sleep(50 * time.Millisecond) // let the amp settle

	// Sweep the calibration attenuator: change the field, Write again.
	fmt.Println("Sweeping calibration attenuator 0 -> 20 dB:")
	for db := int32(0); db <= 20; db += 5 {
		wp.CalAttenuation = db
		if err := wp.Write(); err != nil {
			log.Fatalf("set cal attenuation %d dB: %v", db, err)
		}
		fmt.Printf("  cal attenuation = %2d dB (take your measurement here)\n", db)
		time.Sleep(20 * time.Millisecond)
	}

	// Restore the normal through path.
	wp.CalEnabled = false
	if err := wp.Write(); err != nil {
		log.Fatalf("restore through path: %v", err)
	}
	fmt.Println("Done — calibration mode off, through path restored.")
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
