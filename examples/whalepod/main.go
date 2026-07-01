// Command whalepod-example shows how to drive a Whalepod board over the
// network from Go with the client package: build up a client.Whalepod, set
// the fields you want, and Write() them to the device.
//
// It runs a typical calibration sweep — enter calibration mode with the
// internal noise source, step the calibration attenuator, then restore the
// normal through path — reusing one Whalepod object throughout.
package main

import (
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/OcupointInc/rf-control/client"
)

// deviceIP is the Whalepod's static IP address. Set this to your board, or
// override it at runtime with the -ip flag.
const deviceIP = "192.168.1.50"

func main() {
	ip := flag.String("ip", deviceIP, "Whalepod device IPv4 address (TCP port 5000)")
	flag.Parse()

	wp := client.NewWhalepod(client.NewTCPTransport(*ip, 5000))
	defer wp.Close()

	cfg, err := wp.GetConfig()
	if err != nil {
		log.Fatalf("connect to %s: %v", *ip, err)
	}
	fmt.Printf("Connected to %s — serial %s (firmware %s)\n", *ip, cfg.SerialNumber, cfg.FirmwareVersion)

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
