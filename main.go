// rf-control is a single-binary CLI for configuring the WIZnet Pico RF
// control board over either TCP or USB (CDC1). It's a thin wrapper around
// the client package (see client/client.go) — import that package directly
// if you want to drive a device from your own Go program instead of
// shelling out to this binary.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/OcupointInc/rf-control/client"
	pb "github.com/OcupointInc/rf-control/controlpb"
)

// verbose is set by the -v / --verbose flag (see addCommonFlags). When true,
// transports hex-dump bytes sent and received and log timings.
var verbose bool

func vlogf(format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, "[v] "+format+"\n", args...)
	}
}

// ----- transport selection ---------------------------------------------------

type commonFlags struct {
	ip   string
	port int
	usb  string
}

func (c *commonFlags) makeTransport() (client.Transport, error) {
	// 1. Explicit USB path.
	if c.usb != "" {
		tx, err := client.NewUSBTransport(c.usb)
		if err != nil {
			return nil, err
		}
		tx.Verbose = vlogf
		return tx, nil
	}
	// 2. Explicit TCP. Only used when --ip is set — we never silently fall
	// back to TCP, since timing out on a non-existent network address is a
	// poor experience.
	if c.ip != "" {
		tx := client.NewTCPTransport(c.ip, c.port)
		tx.Verbose = vlogf
		return tx, nil
	}
	// 3. Auto-discover USB. Fail clearly if no device responds.
	portName, err := client.DiscoverUSBPort()
	if err != nil {
		return nil, fmt.Errorf("USB auto-discovery failed: %w (pass --usb /dev/... or --ip ADDRESS to be explicit)", err)
	}
	fmt.Fprintf(os.Stderr, "[auto] using USB %s\n", portName)
	tx, err := client.NewUSBTransport(portName)
	if err != nil {
		return nil, err
	}
	tx.Verbose = vlogf
	return tx, nil
}

func addCommonFlags(fs *flag.FlagSet, c *commonFlags) {
	fs.StringVar(&c.ip, "ip", "", "Device IPv4 address. If set, TCP is used. Otherwise USB is auto-discovered.")
	fs.IntVar(&c.port, "port", 5000, "Device TCP port")
	fs.StringVar(&c.usb, "usb", "", "USB serial device, e.g. /dev/ttyACM1. If set, USB is used.")
	fs.BoolVar(&verbose, "v", false, "Verbose: hex-dump USB bytes and log timing")
	fs.BoolVar(&verbose, "verbose", false, "Verbose: hex-dump USB bytes and log timing")
}

// ----- helpers ---------------------------------------------------------------

func parseIPv4(s string) ([]byte, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return nil, fmt.Errorf("not an IPv4 address: %q", s)
	}
	out := make([]byte, 4)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return nil, fmt.Errorf("not an IPv4 address: %q", s)
		}
		out[i] = byte(n)
	}
	return out, nil
}

func ipString(b []byte) string {
	if len(b) != 4 {
		return "<invalid>"
	}
	return fmt.Sprintf("%d.%d.%d.%d", b[0], b[1], b[2], b[3])
}

// ----- subcommands -----------------------------------------------------------

func cmdGet(args []string) error {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	cfg, err := c.GetConfig()
	if err != nil {
		return err
	}

	mac := cfg.MacAddress
	var macStr string
	if len(mac) == 6 {
		macStr = fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
	} else {
		macStr = "<unset>"
	}

	fmt.Println("--- Current Configuration ---")
	fmt.Printf("IP Address:    %s\n", ipString(cfg.StaticIp))
	fmt.Printf("Gateway:       %s\n", ipString(cfg.StaticGateway))
	fmt.Printf("Subnet:        %s\n", ipString(cfg.StaticSubnet))
	fmt.Printf("mDNS Host:     %s\n", cfg.MdnsHostname)
	fmt.Printf("MAC:           %s\n", macStr)
	fmt.Printf("Serial No:     %s\n", cfg.SerialNumber)
	fmt.Printf("Firmware:      %s\n", cfg.FirmwareVersion)
	return nil
}

func cmdSetIP(args []string) error {
	fs := flag.NewFlagSet("set-ip", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	newIP := fs.String("address", "", "New IPv4 address (e.g. 192.168.1.50)")
	newGW := fs.String("gateway", "", "New gateway IPv4 address")
	newSN := fs.String("subnet", "", "New subnet mask (e.g. 255.255.255.0)")
	newHost := fs.String("hostname", "", "New mDNS hostname (without .local)")
	_ = fs.Parse(args)

	if *newIP == "" && *newGW == "" && *newSN == "" && *newHost == "" {
		return errors.New("provide at least one of --address, --gateway, --subnet, --hostname")
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	cur, err := c.GetConfig()
	if err != nil {
		return fmt.Errorf("read current config: %w", err)
	}

	pickIP := func(override string, fallback []byte) ([]byte, error) {
		if override == "" {
			return fallback, nil
		}
		return parseIPv4(override)
	}

	ipBytes, err := pickIP(*newIP, cur.StaticIp)
	if err != nil {
		return err
	}
	gwBytes, err := pickIP(*newGW, cur.StaticGateway)
	if err != nil {
		return err
	}
	snBytes, err := pickIP(*newSN, cur.StaticSubnet)
	if err != nil {
		return err
	}
	host := cur.MdnsHostname
	if *newHost != "" {
		host = *newHost
	}

	fmt.Println("Applying network change:")
	fmt.Printf("  IP       : %s  ->  %s\n", ipString(cur.StaticIp), ipString(ipBytes))
	fmt.Printf("  Gateway  : %s  ->  %s\n", ipString(cur.StaticGateway), ipString(gwBytes))
	fmt.Printf("  Subnet   : %s  ->  %s\n", ipString(cur.StaticSubnet), ipString(snBytes))
	fmt.Printf("  Hostname : %s  ->  %s\n", cur.MdnsHostname, host)

	err = c.SaveConfig(&pb.SaveConfigRequest{
		StaticIp:      ipBytes,
		StaticGateway: gwBytes,
		StaticSubnet:  snBytes,
		MdnsHostname:  host,
		MacAddress:    cur.MacAddress,
		SerialNumber:  cur.SerialNumber,
	})
	if err != nil {
		return err
	}
	fmt.Println("Success! Device is rebooting to apply changes.")
	return nil
}

// ----- discovery subcommand --------------------------------------------------

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	fmt.Println("Candidate USB serial ports (firmware uses VID:PID " + client.DeviceVID + ":" + client.DevicePID + "):")
	cands, err := client.ListCandidatePorts()
	if err != nil {
		fmt.Printf("  (enumeration error: %v)\n", err)
	} else if len(cands) == 0 {
		fmt.Println("  (none found)")
	} else {
		for _, name := range cands {
			role := "debug/stdio or unrelated (no control-protocol response)"
			if client.IsControlPort(name) {
				if tx, err := client.NewUSBTransport(name); err == nil {
					if cfg, err := client.New(tx).GetConfig(); err == nil {
						role = fmt.Sprintf("CONTROL  ->  IP=%s, host=%s, serial=%s",
							ipString(cfg.StaticIp), cfg.MdnsHostname, cfg.SerialNumber)
					} else {
						role = "CONTROL (probe ok, summary failed: " + err.Error() + ")"
					}
					tx.Close()
				}
			}
			fmt.Printf("  %s\n     %s\n", name, role)
		}
	}

	// TCP probe only when the user explicitly asked for one (--ip set).
	if common.ip != "" {
		fmt.Printf("\nTCP probe at %s:%d\n", common.ip, common.port)
		tcp := client.NewTCPTransport(common.ip, common.port)
		if cfg, err := client.New(tcp).GetConfig(); err != nil {
			fmt.Printf("  not reachable (%v)\n", err)
		} else {
			fmt.Printf("  reachable  ->  IP=%s, host=%s, serial=%s\n",
				ipString(cfg.StaticIp), cfg.MdnsHostname, cfg.SerialNumber)
		}
	}
	return nil
}

// ----- RF control subcommands -----------------------------------------------

func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "on", "1", "true", "enable", "enabled", "yes":
		return true, nil
	case "off", "0", "false", "disable", "disabled", "no":
		return false, nil
	}
	return false, fmt.Errorf("expected on/off (got %q)", s)
}

func cmdStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	s, err := c.GetStatus()
	if err != nil {
		return err
	}

	// "whalepod" hardware only has attenuators — no PLL, no RF/mixer/IF
	// switches. Suppress those fields so the output reflects the device.
	hasRFFrontend := s.BoardType != "whalepod"

	fmt.Println("--- Device RF Status ---")
	if s.BoardType != "" {
		fmt.Printf("Board               : %s\n", s.BoardType)
	}
	fmt.Printf("Channels enabled    : %v\n", s.ChannelsEnabled)
	fmt.Printf("Calibration enabled : %v\n", s.CalibrationEnabled)
	fmt.Printf("Frontend atten (dB) : %d\n", s.AttenuationDb)
	if hasRFFrontend {
		fmt.Printf("LO frequency (MHz)  : %d\n", s.LoFrequencyMhz)
		fmt.Printf("RF switch           : %s\n", s.RfSwitch)
		fmt.Printf("Mixer switch        : %s\n", s.MixerSwitch)
		fmt.Printf("IF switch           : %s\n", s.IfSwitch)
	}
	return nil
}

func cmdSetAtt(args []string) error {
	fs := flag.NewFlagSet("set-att", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return errors.New("usage: set-att <dB>  (e.g. set-att 10)")
	}
	db, err := strconv.Atoi(fs.Arg(0))
	if err != nil || db < 0 || db > 255 {
		return fmt.Errorf("invalid attenuation %q (expected integer dB, 0-255)", fs.Arg(0))
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	if err := c.SetAttenuation(int32(db)); err != nil {
		return err
	}
	fmt.Printf("OK (frontend attenuation = %d dB)\n", db)
	return nil
}

func cmdSetCalAtt(args []string) error {
	fs := flag.NewFlagSet("set-cal-att", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return errors.New("usage: set-cal-att <dB>")
	}
	db, err := strconv.Atoi(fs.Arg(0))
	if err != nil || db < 0 || db > 255 {
		return fmt.Errorf("invalid attenuation %q (expected integer dB, 0-255)", fs.Arg(0))
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	if err := c.SetCalAttenuation(int32(db)); err != nil {
		return err
	}
	fmt.Printf("OK (calibration attenuation = %d dB)\n", db)
	return nil
}

func cmdSetChannels(args []string) error {
	fs := flag.NewFlagSet("set-channels", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return errors.New("usage: set-channels <on|off>")
	}
	on, err := parseOnOff(fs.Arg(0))
	if err != nil {
		return err
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	if err := c.SetChannelsEnabled(on); err != nil {
		return err
	}
	state := "OFF"
	if on {
		state = "ON"
	}
	fmt.Printf("OK (channels = %s)\n", state)
	return nil
}

func cmdSetCal(args []string) error {
	fs := flag.NewFlagSet("set-cal", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return errors.New("usage: set-cal <on|off>")
	}
	on, err := parseOnOff(fs.Arg(0))
	if err != nil {
		return err
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	if err := c.SetCalEnabled(on); err != nil {
		return err
	}
	state := "OFF"
	if on {
		state = "ON"
	}
	fmt.Printf("OK (calibration = %s)\n", state)
	return nil
}

func cmdSetCalSource(args []string) error {
	fs := flag.NewFlagSet("set-cal-source", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return errors.New("usage: set-cal-source <internal|external>  (whalepod CAL_SEL)")
	}
	var internal bool
	switch strings.ToLower(fs.Arg(0)) {
	case "internal", "int", "noise", "on":
		internal = true
	case "external", "ext", "off":
		internal = false
	default:
		return fmt.Errorf("invalid source %q: use internal or external", fs.Arg(0))
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	if err := c.SetCalSource(internal); err != nil {
		return err
	}
	src := "external"
	if internal {
		src = "internal"
	}
	fmt.Printf("OK (cal source = %s)\n", src)
	return nil
}

func cmdApplyJSON(args []string) error {
	fs := flag.NewFlagSet("apply-json", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return errors.New("usage: apply-json <file>")
	}

	raw, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var data struct {
		StaticIP      string `json:"static_ip"`
		StaticGateway string `json:"static_gateway"`
		StaticSubnet  string `json:"static_subnet"`
		Hostname      string `json:"hostname"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return err
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	cur, err := c.GetConfig()
	if err != nil {
		return fmt.Errorf("read current config: %w", err)
	}

	ipBytes, err := parseIPv4(data.StaticIP)
	if err != nil {
		return err
	}
	gwBytes, err := parseIPv4(data.StaticGateway)
	if err != nil {
		return err
	}
	subnet := data.StaticSubnet
	if subnet == "" {
		subnet = "255.255.255.0"
	}
	snBytes, err := parseIPv4(subnet)
	if err != nil {
		return err
	}

	fmt.Printf("Applying config: IP=%s, Host=%s\n", data.StaticIP, data.Hostname)

	err = c.SaveConfig(&pb.SaveConfigRequest{
		StaticIp:      ipBytes,
		StaticGateway: gwBytes,
		StaticSubnet:  snBytes,
		MdnsHostname:  data.Hostname,
		MacAddress:    cur.MacAddress,
		SerialNumber:  cur.SerialNumber,
	})
	if err != nil {
		return err
	}
	fmt.Println("Success! Device is rebooting.")
	return nil
}

// ----- entry point -----------------------------------------------------------

func usage() {
	fmt.Fprint(os.Stderr, `WIZnet Pico RF control tool

Usage:
  rf-control [--usb /dev/ttyACM1 | --ip 172.16.22.30 --port 5000] <command> [args]

Commands:
  list                   Discover USB devices matching the firmware
                         (VID:PID 2E8A:000A) and probe the configured
                         TCP address. Useful for finding the right
                         /dev/tty.usbmodemXXX.
  get                    Print the current device configuration.
  set-ip [flags]         Change one or more of: --address, --gateway,
                         --subnet, --hostname. Preserves MAC + serial.
  apply-json <file>      Apply a JSON config file with fields:
                         static_ip, static_gateway, static_subnet, hostname.
  status                 Print live RF status (channels, attenuation,
                         LO frequency, switch positions).
  set-att <dB>           Set frontend attenuation in dB (e.g. 10).
  set-cal-att <dB>       Set calibration attenuation in dB.
  set-channels <on|off>  Enable or disable the RF channels.
  set-cal <on|off>       Enter/leave calibration mode (CAL_SW). With the internal
                         source selected this also turns on the noise-source amp.
  set-cal-source <internal|external>
                         Select the whalepod calibration source (CAL_SEL).
                         The internal noise-source amp turns on only in cal
                         mode with the internal source selected.

Transport selection (place before the command):
  --usb DEVICE   Use that USB serial device, e.g. /dev/cu.usbmodem101.
  --ip ADDRESS   Use TCP at that IPv4 address.
  (neither)      Auto-discover USB. Probes each candidate serial port;
                 the first one that replies to a GetConfigRequest is used.
                 Never falls back to TCP — pass --ip explicitly for that.
  --port PORT    TCP port (default 5000).

This CLI is a thin wrapper around the client Go package in this repo
(github.com/OcupointInc/rf-control/client) — import it directly if you want
to drive a device from your own Go program. See README.md and
examples/whalepod for details.
`)
}

func main() {
	// Accept transport flags either before or after the subcommand to keep
	// usage forgiving. We do a minimal manual scan to find the subcommand.
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}

	// Boolean flags here don't consume the following token. Keep in sync with
	// addCommonFlags.
	boolFlags := map[string]bool{
		"-v": true, "--v": true, "-verbose": true, "--verbose": true,
	}

	// Find the first non-flag, non-flag-value token: that's the subcommand.
	cmdIdx := -1
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") {
			cmdIdx = i
			break
		}
		// `--flag value` form: skip the value too if not `--flag=value` and
		// not a known boolean flag.
		if !strings.Contains(a, "=") && !boolFlags[a] && i+1 < len(args) {
			i++
		}
	}
	if cmdIdx < 0 {
		usage()
		os.Exit(2)
	}

	// Re-order: subcommand-name first, then all flags from both sides.
	cmd := args[cmdIdx]
	rest := append([]string{}, args[:cmdIdx]...)
	rest = append(rest, args[cmdIdx+1:]...)

	var err error
	switch cmd {
	case "list":
		err = cmdList(rest)
	case "get":
		err = cmdGet(rest)
	case "set-ip":
		err = cmdSetIP(rest)
	case "apply-json":
		err = cmdApplyJSON(rest)
	case "status":
		err = cmdStatus(rest)
	case "set-att":
		err = cmdSetAtt(rest)
	case "set-cal-att":
		err = cmdSetCalAtt(rest)
	case "set-channels":
		err = cmdSetChannels(rest)
	case "set-cal":
		err = cmdSetCal(rest)
	case "set-cal-source":
		err = cmdSetCalSource(rest)
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		if errors.Is(err, io.EOF) {
			os.Exit(3)
		}
		os.Exit(1)
	}
}
