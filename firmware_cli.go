package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/OcupointInc/rf-control/client"
)

func cmdImageInfo(args []string) error {
	fs := flag.NewFlagSet("image-info", flag.ExitOnError)
	fs.BoolVar(&verbose, "v", false, "Verbose")
	fs.BoolVar(&verbose, "verbose", false, "Verbose")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: image-info <firmware.bin>")
	}
	image, err := client.LoadFirmwareImage(fs.Arg(0))
	if err != nil {
		return err
	}
	printFirmwareImage(fs.Arg(0), image)
	return nil
}

func printFirmwareImage(path string, image *client.FirmwareImage) {
	fmt.Println("--- Firmware image ---")
	fmt.Printf("File        : %s\n", path)
	fmt.Printf("Board       : %s\n", image.Board)
	fmt.Printf("Version     : %s\n", image.Version)
	fmt.Printf("Build ID    : %s\n", image.BuildID)
	fmt.Printf("Size        : %d bytes\n", image.Size())
	fmt.Printf("CRC-32      : 0x%08X\n", image.CRC32)
	fmt.Printf("Info offset : %d\n", image.InfoOffset)
}

func cmdFlash(args []string) error {
	fs := flag.NewFlagSet("flash", flag.ExitOnError)
	ip := fs.String("ip", "", "Device IPv4 address (uses Ethernet OTA)")
	usbPath := fs.String("usb", "", "USB control device; omit with --ip for USB auto-discovery")
	port := fs.Int("port", client.FirmwareUpdatePort, "Firmware-update TCP port")
	controlPort := fs.Int("control-port", 5000, "Control port used to verify the reboot")
	yes := fs.Bool("yes", false, "Skip confirmation")
	noWait := fs.Bool("no-wait", false, "Do not wait for the rebooted device")
	fs.BoolVar(&verbose, "v", false, "Verbose")
	fs.BoolVar(&verbose, "verbose", false, "Verbose")
	_ = fs.Parse(args)
	if fs.NArg() != 1 {
		return errors.New("usage: flash [--ip ADDRESS|--usb DEVICE] [--yes] <firmware.bin>")
	}
	if *ip != "" && *usbPath != "" {
		return errors.New("choose either --ip or --usb, not both")
	}
	image, err := client.LoadFirmwareImage(fs.Arg(0))
	if err != nil {
		return err
	}
	printFirmwareImage(fs.Arg(0), image)

	target := "USB (auto-discover)"
	if *ip != "" {
		target = fmt.Sprintf("%s:%d", *ip, *port)
	} else if *usbPath != "" {
		target = "USB " + *usbPath
	}
	if !*yes {
		if !isTerminal(os.Stdin) {
			return errors.New("refusing to flash without confirmation; pass --yes")
		}
		fmt.Fprintf(os.Stderr, "Flash Barracuda firmware %s to %s and reboot? [y/N] ", image.Version, target)
		answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if normalized := strings.ToLower(strings.TrimSpace(answer)); normalized != "y" && normalized != "yes" {
			return errors.New("flash cancelled")
		}
	}

	lastPercent := -1
	progress := func(done, total uint32) {
		percent := 100
		if total > 0 {
			percent = int(uint64(done) * 100 / uint64(total))
		}
		if percent/10 != lastPercent/10 || done == total {
			fmt.Fprintf(os.Stderr, "flashing: %3d%% (%d/%d bytes)\n", percent, done, total)
			lastPercent = percent
		}
	}

	if *ip != "" {
		err = client.UpdateFirmwareTCP(*ip, *port, image, progress)
	} else {
		path := *usbPath
		if path == "" {
			path, err = client.DiscoverUSBPort()
			if err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "[auto] using USB %s\n", path)
		}
		usb, openErr := client.NewUSBTransport(path)
		if openErr != nil {
			return openErr
		}
		err = client.UpdateFirmwareUSB(usb, image, progress)
		_ = usb.Close()
	}
	if err != nil {
		return err
	}
	fmt.Println("Commit verified; device is rebooting into the new firmware.")
	if *noWait {
		return nil
	}

	deadline := time.Now().Add(45 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		if *ip != "" {
			statusClient := client.New(client.NewTCPTransport(*ip, *controlPort))
			config, configErr := statusClient.GetConfig()
			_ = statusClient.Close()
			if configErr == nil && config.FirmwareVersion == image.Version {
				fmt.Printf("Device is back: board=%s firmware=%s\n", image.Board, config.FirmwareVersion)
				return nil
			}
			if configErr != nil {
				lastErr = configErr
			} else {
				lastErr = fmt.Errorf("device reports firmware %s", config.FirmwareVersion)
			}
		} else {
			path, discoverErr := client.DiscoverUSBPort()
			if discoverErr == nil {
				usb, openErr := client.NewUSBTransport(path)
				if openErr == nil {
					statusClient := client.New(usb)
					config, configErr := statusClient.GetConfig()
					_ = statusClient.Close()
					if configErr == nil && config.FirmwareVersion == image.Version {
						fmt.Printf("Device is back: board=%s firmware=%s\n", image.Board, config.FirmwareVersion)
						return nil
					}
					lastErr = configErr
				}
			} else {
				lastErr = discoverErr
			}
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("device did not return with firmware %s within 45s (last result: %v)", image.Version, lastErr)
}

func isTerminal(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}
