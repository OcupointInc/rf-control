// rf-control is a single-binary CLI for configuring the WIZnet Pico RF
// control board over either TCP or USB (CDC1). It's a thin wrapper around
// the client package (see client/client.go) — import that package directly
// if you want to drive a device from your own Go program instead of
// shelling out to this binary.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/OcupointInc/rf-control/client"
	pb "github.com/OcupointInc/rf-control/controlpb"
)

// version is stamped at link time by the release workflow
// (-X main.version=...): a tag name like "v1.2.3" for a tagged release, or
// "latest-<sha>" for the rolling build of main. Local `go build` leaves "dev".
var version = "dev"

// verbose is set by the -v / --verbose flag (see addCommonFlags). When true,
// transports hex-dump bytes sent and received and log timings.
var verbose bool

func vlogf(format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, "[v] "+format+"\n", args...)
	}
}

// ----- exit codes ------------------------------------------------------------
//
// Distinct codes let a script (or a person) tell *why* a command failed without
// scraping the message — in particular, whether the mistake was in the input
// (fix your command) or on the device side (check the hardware/connection).
const (
	exitOK        = 0 // success
	exitRuntime   = 1 // unexpected internal error
	exitUsage     = 2 // invalid command-line input: unknown command, bad flag, or bad argument value
	exitTransport = 3 // could not reach or talk to the device (connection refused, timeout, USB gone)
	exitDevice    = 4 // the device received the request but rejected it (see the ErrorCode printed)
	exitFault     = 5 // a diagnostic ran successfully but found a hardware fault (e.g. gpio-selftest: a stuck pin)
)

// faultError marks an error as "a diagnostic completed but detected a hardware
// fault" (e.g. gpio-selftest found a stuck pin) rather than a usage, device, or
// transport failure. The command has already printed its own result table, so
// classifyExit maps this to exitFault without printing a generic "Error:" line.
type faultError struct{ err error }

func (e *faultError) Error() string { return e.err.Error() }
func (e *faultError) Unwrap() error { return e.err }

// faultf builds a faultError from a printf-style message.
func faultf(format string, a ...any) error { return &faultError{err: fmt.Errorf(format, a...)} }

// usageError marks an error as caused by bad user input, so main() can exit with
// exitUsage (and, for interactive commands, point the user at --help) instead of
// treating it like an internal failure.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// usagef builds a usageError from a printf-style message.
func usagef(format string, a ...any) error { return &usageError{err: fmt.Errorf(format, a...)} }

// ----- transport selection ---------------------------------------------------

type commonFlags struct {
	ip   string
	port int
	usb  string
}

// makeTransport builds the transport selected by the flags. Any failure here is
// a "can't reach the device" problem, so it's wrapped as a client.TransportError
// (exit code exitTransport) rather than a generic error.
func (c *commonFlags) makeTransport() (client.Transport, error) {
	tx, err := c.makeTransportInner()
	if err != nil {
		return nil, &client.TransportError{Err: err}
	}
	return tx, nil
}

func (c *commonFlags) makeTransportInner() (client.Transport, error) {
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
		return nil, usagef("not an IPv4 address: %q", s)
	}
	out := make([]byte, 4)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || n > 255 {
			return nil, usagef("not an IPv4 address: %q", s)
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

	fmt.Println("\nEthernet devices (UDP discovery):")
	devices, discoveryErr := client.DiscoverEthernet(750 * time.Millisecond)
	if discoveryErr != nil {
		fmt.Printf("  (discovery error: %v)\n", discoveryErr)
	} else if len(devices) == 0 {
		fmt.Println("  (none found)")
	} else {
		for _, device := range devices {
			fmt.Printf("  %-15s  board=%-20s firmware=%-10s serial=%s port=%d\n",
				ipString(device.Ip), device.Board, device.FirmwareVersion, device.Serial, device.ControlPort)
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
	return false, usagef("expected on/off (got %q)", s)
}

func cmdStatus(args []string) error {
	return cmdStatusView(args, false)
}

func cmdEngineeringStatus(args []string) error {
	return cmdStatusView(args, true)
}

func cmdStatusView(args []string, engineering bool) error {
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
	hasRFFrontend := s.BoardType != "whalepod" && s.BoardType != "rf_switch"

	fmt.Println("--- Device RF Status ---")
	if s.BoardType != "" {
		fmt.Printf("Board               : %s\n", s.BoardType)
	}

	// The SP8T switch board is the mirror image of the whalepod: its firmware
	// stubs out the attenuator, PLL, and calibration path entirely, so every
	// other field in GetStatusResponse is an uninitialised default. The
	// selected channel is the only thing this board actually knows.
	if s.BoardType == "rf_switch" {
		if s.RfSwitchChannel == 0 {
			fmt.Printf("RF switch channel   : off (all isolated)\n")
		} else {
			fmt.Printf("RF switch channel   : %d\n", s.RfSwitchChannel)
		}
		return nil
	}
	if s.BoardType == "barracuda" {
		printBarracudaCustomerStatus(s)
		if engineering {
			printBarracudaEngineeringStatus(s)
		}
		return nil
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

// stuckWord renders a GpioStuckState as the word shown in the FAIL annotation.
func stuckWord(s pb.GpioStuckState) string {
	switch s {
	case pb.GpioStuckState_GPIO_STUCK_STATE_LOW:
		return "LOW"
	case pb.GpioStuckState_GPIO_STUCK_STATE_HIGH:
		return "HIGH"
	default:
		return "?"
	}
}

// switchControlPins are the Whalepod control lines the firmware configures at
// 12 mA pad drive (the switch/select bank) rather than the 4 mA default. Needing
// a high drive is therefore *expected* for these, so they only count as marginal
// when they fail outright (min_drive_ma == 0) — which the pass/fail path already
// reports. Keyed by firmware pin `name`. Every other pin defaults to 4 mA, so a
// value above 4 mA there is the early-warning signal. See gpio_verify.c.
var switchControlPins = map[string]bool{
	"CAL_SW": true, "CAL_SEL": true, "RF_SW": true, "MIXER_SW": true,
	"IF_SW": true, "SW_V1": true, "SW_V2": true, "SW_V3": true, "SW_V4": true,
	"SW_LS": true,
}

// driveNote returns a short "needs N mA" note when a PASSING pin required a pad
// drive strength above its board default — flux/leakage the firmware can still
// drive through, but an early sign the board is degrading. It returns "" when
// there is nothing notable: a pin that passed at (or below) its expected default,
// a 12 mA switch-control line, or a pin with no drive data / an outright failure
// (min_drive_ma == 0, already surfaced as FAIL). This is the single place the
// "expected default" policy lives so both self-test views agree.
func driveNote(name string, minDriveMa uint32) string {
	if minDriveMa == 0 {
		return "" // failed, or firmware too old to report drive — not "marginal"
	}
	if switchControlPins[name] {
		return "" // 12 mA by design; only notable if it fails
	}
	if minDriveMa > 4 {
		return fmt.Sprintf("needs %d mA", minDriveMa)
	}
	return ""
}

// marginalAdvisory returns the one-line note appended after the Result line when
// any PASSING pin needed an above-default drive, or "" when none did. Shared by
// both the flat and grouped self-test views so the advisory reads identically.
func marginalAdvisory(pins []*pb.GpioPinResult) string {
	n := 0
	for _, p := range pins {
		if p.GetPassed() && driveNote(p.GetName(), p.GetMinDriveMa()) != "" {
			n++
		}
	}
	if n == 0 {
		return ""
	}
	noun := "pin"
	if n > 1 {
		noun = "pins"
	}
	return fmt.Sprintf("Note: %d %s needed elevated drive (possible flux/leakage — consider cleaning/rework).\n", n, noun)
}

// formatGPIOSelfTest renders a GpioSelfTestResponse as an aligned, human-readable
// table followed by a summary Result line. It's factored out of cmdGpioSelfTest
// so the formatting can be unit-tested without a transport. Columns (GPIO number
// and signal name) are sized to the widest entry so the table stays aligned for
// any pin set.
//
// board is the device's BoardType (from GetStatus); it lets each FAILed pin
// carry a plain-English, board-specific consequence on an indented continuation
// line (see client.GpioFaultDescription). Pass "" — e.g. when the board type
// couldn't be read — to print the bare table with no descriptions.
func formatGPIOSelfTest(board string, resp *pb.GpioSelfTestResponse) string {
	pins := resp.GetPins()

	var b strings.Builder
	fmt.Fprintf(&b, "GPIO self-test — %d pins\n", len(pins))

	pinW, nameW := 1, 0
	for _, p := range pins {
		if w := len(strconv.FormatUint(uint64(p.GetPin()), 10)); w > pinW {
			pinW = w
		}
		if w := len(p.GetName()); w > nameW {
			nameW = w
		}
	}
	// Indent for a fault's continuation line, aligned under the signal-name
	// column: two leading spaces + "GPIO " + the pin field (pinW) + two spaces.
	descIndent := strings.Repeat(" ", 9+pinW)

	stuck := 0
	for _, p := range pins {
		status := "PASS"
		if p.GetPassed() {
			// Raw table always reports the drive a passing pin needed, so a
			// creeping requirement is visible pin-by-pin (0 = firmware too old
			// to report it — omit rather than print "needs 0 mA").
			if md := p.GetMinDriveMa(); md > 0 {
				status = fmt.Sprintf("PASS  (needs %d mA)", md)
			}
		} else {
			stuck++
			status = "FAIL  (stuck " + stuckWord(p.GetStuck()) + ")"
		}
		fmt.Fprintf(&b, "  GPIO %*d  %-*s  %s\n", pinW, p.GetPin(), nameW, p.GetName(), status)
		if !p.GetPassed() {
			if desc := client.GpioFaultDescription(board, p.GetName(), p.GetStuck()); desc != "" {
				fmt.Fprintf(&b, "%s↳ %s\n", descIndent, desc)
			}
		}
	}

	if resp.GetAllPassed() {
		fmt.Fprintf(&b, "Result: PASS — all %d pin(s) ok\n", len(pins))
	} else {
		fmt.Fprintf(&b, "Result: FAIL — %d of %d pin(s) stuck\n", stuck, len(pins))
	}
	b.WriteString(marginalAdvisory(pins))
	return b.String()
}

func isWhalepod(board string) bool {
	return board == "whalepod" || board == "whalepod_automation"
}

// whalepodSelfTestGroups maps the Whalepod control pins into the functional
// subsystems the high-level gpio-selftest view reports. Slice order is the
// display order; a group appears only if at least one of its member pin-names is
// present in the response. Keyed by the firmware pin `name`. (On the automation
// build "Calibration source select" has only CAL_AMP_EN — no CAL_SEL — and
// "Clock enable" is present; base whalepod is the reverse and adds CAL_ATT.)
var whalepodSelfTestGroups = []struct {
	name    string
	members []string
}{
	{"Power enable", []string{"PWR_EN", "FE_EN"}},
	{"Attenuator control (VHF/UHF)", []string{"SCK", "MOSI", "CS_VHF", "CS_UHF"}},
	{"Calibration enable", []string{"CAL_SW"}},
	{"Calibration source select", []string{"CAL_SEL", "CAL_AMP_EN"}},
	{"Clock enable", []string{"CLOCK_EN"}},
	{"Cal-path attenuator", []string{"CAL_ATT"}},
}

// formatGPIOSelfTestGrouped renders the self-test as one line per functional
// subsystem (Power enable / Attenuator control / …) rather than per pin. A
// subsystem is PASS only if every one of its present control pins passed. A
// FAILing subsystem that owns a single pin just shows the plain-English
// consequence; one that owns several (the attenuator bus) spells out which of
// its pins passed and which failed, so it's clear whether the loss is all
// attenuation (SCK/MOSI) or one channel (CS_VHF / CS_UHF). Whalepod-only; other
// boards use the flat per-pin table.
func formatGPIOSelfTestGrouped(board string, resp *pb.GpioSelfTestResponse) string {
	pins := resp.GetPins()
	byName := make(map[string]*pb.GpioPinResult, len(pins))
	for _, p := range pins {
		byName[p.GetName()] = p
	}

	type grp struct {
		name    string
		members []*pb.GpioPinResult // present members, in taxonomy order
	}
	var groups []grp
	claimed := make(map[string]bool)
	for _, g := range whalepodSelfTestGroups {
		var present []*pb.GpioPinResult
		for _, m := range g.members {
			if p, ok := byName[m]; ok {
				present = append(present, p)
				claimed[m] = true
			}
		}
		if len(present) > 0 {
			groups = append(groups, grp{g.name, present})
		}
	}
	// Any pin not claimed by a subsystem still gets reported, so nothing is
	// silently dropped if the firmware pin set ever grows.
	var other []*pb.GpioPinResult
	for _, p := range pins {
		if !claimed[p.GetName()] {
			other = append(other, p)
		}
	}
	if len(other) > 0 {
		groups = append(groups, grp{"Other", other})
	}

	nameW := 0
	for _, g := range groups {
		if len(g.name) > nameW {
			nameW = len(g.name)
		}
	}

	var b strings.Builder
	overall := "PASS"
	if !resp.GetAllPassed() {
		overall = "FAIL"
	}
	fmt.Fprintf(&b, "GPIO self-test — %s: %s\n", board, overall)

	stuck := 0
	for _, g := range groups {
		failed := false
		for _, p := range g.members {
			if !p.GetPassed() {
				failed = true
			}
		}
		if !failed {
			// The subsystem passed, but flag it if any pad needed an
			// above-default drive: headline the worst offender on the subsystem
			// line and name each marginal pin on its own continuation, so a
			// degrading-but-still-working board is visible without a full FAIL.
			var marg []*pb.GpioPinResult
			worst := uint32(0)
			for _, p := range g.members {
				if driveNote(p.GetName(), p.GetMinDriveMa()) != "" {
					marg = append(marg, p)
					if p.GetMinDriveMa() > worst {
						worst = p.GetMinDriveMa()
					}
				}
			}
			if len(marg) == 0 {
				fmt.Fprintf(&b, "  %-*s  PASS\n", nameW, g.name)
				continue
			}
			fmt.Fprintf(&b, "  %-*s  PASS (marginal: needs %d mA)\n", nameW, g.name, worst)
			for _, p := range marg {
				fmt.Fprintf(&b, "      ↳ %s (GPIO %d) passed only at %d mA (default 4 mA)\n",
					p.GetName(), p.GetPin(), p.GetMinDriveMa())
			}
			continue
		}
		fmt.Fprintf(&b, "  %-*s  FAIL\n", nameW, g.name)

		if len(g.members) == 1 {
			// Single-pin subsystem: the subsystem name already identifies it, so
			// just give the consequence (fall back to a bare pin line if there's
			// no board-specific description, e.g. an "Other" pin).
			p := g.members[0]
			stuck++
			if desc := client.GpioFaultDescription(board, p.GetName(), p.GetStuck()); desc != "" {
				fmt.Fprintf(&b, "      ↳ %s\n", desc)
			} else {
				fmt.Fprintf(&b, "      ↳ %s (GPIO %d) stuck %s\n", p.GetName(), p.GetPin(), stuckWord(p.GetStuck()))
			}
			continue
		}

		// Multi-pin subsystem: spell out each pin's result.
		mnameW, labelW := 0, 0
		labels := make([]string, len(g.members))
		for i, p := range g.members {
			if w := len(p.GetName()); w > mnameW {
				mnameW = w
			}
			labels[i] = fmt.Sprintf("(GPIO %d)", p.GetPin())
			if len(labels[i]) > labelW {
				labelW = len(labels[i])
			}
		}
		for i, p := range g.members {
			status := "PASS"
			if !p.GetPassed() {
				stuck++
				status = "FAIL  (stuck " + stuckWord(p.GetStuck()) + ")"
			} else if note := driveNote(p.GetName(), p.GetMinDriveMa()); note != "" {
				// Passing member of a failing subsystem — keep healthy members a
				// bare PASS, but surface one that's marginal (grouped view only
				// annotates drive above the default; the --pins table shows all).
				status = "PASS  (" + note + ")"
			}
			fmt.Fprintf(&b, "      %-*s %-*s  %s\n", mnameW, p.GetName(), labelW, labels[i], status)
			if !p.GetPassed() {
				if desc := client.GpioFaultDescription(board, p.GetName(), p.GetStuck()); desc != "" {
					fmt.Fprintf(&b, "          ↳ %s\n", desc)
				}
			}
		}
	}

	if resp.GetAllPassed() {
		fmt.Fprintf(&b, "Result: PASS\n")
	} else {
		fmt.Fprintf(&b, "Result: FAIL — %d of %d pin(s) stuck\n", stuck, len(pins))
	}
	b.WriteString(marginalAdvisory(pins))
	return b.String()
}

// cmdGpioSelfTest runs the firmware's control-GPIO diagnostic and prints an
// aligned pass/fail table. It exits exitFault (not exitDevice/exitTransport)
// when the test ran but found a stuck pin — that's a hardware fault the device
// reported successfully, not a rejected request or an unreachable device.
func cmdGpioSelfTest(args []string) error {
	fs := flag.NewFlagSet("gpio-selftest", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	rawPins := fs.Bool("pins", false, "show the raw per-pin table instead of the grouped subsystem view")
	_ = fs.Parse(args)

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	// Learn the board type first so each failed pin can carry a board-specific,
	// plain-English consequence (the self-test response itself has no board_type).
	// A status read that fails must NOT abort the diagnostic — fall back to an
	// empty board string, which just suppresses the per-pin descriptions.
	board := ""
	if st, err := c.GetStatus(); err != nil {
		vlogf("gpio-selftest: could not read board type (%v); fault descriptions disabled", err)
	} else {
		board = st.GetBoardType()
	}

	resp, err := c.GpioSelfTest()
	if err != nil {
		return err
	}

	// Default to the high-level subsystem view on Whalepod; --pins (or any board
	// without a subsystem taxonomy) falls back to the raw per-pin table.
	if !*rawPins && isWhalepod(board) {
		fmt.Print(formatGPIOSelfTestGrouped(board, resp))
	} else {
		fmt.Print(formatGPIOSelfTest(board, resp))
	}

	if !resp.GetAllPassed() {
		// The table above is the detail; signal the fault via the exit code.
		return faultf("gpio self-test found a stuck pin")
	}
	return nil
}

func cmdSetAtt(args []string) error {
	fs := flag.NewFlagSet("set-att", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return usagef("usage: set-att <dB>  (e.g. set-att 10)")
	}
	db, err := strconv.Atoi(fs.Arg(0))
	if err != nil || db < 0 || db > 255 {
		return usagef("invalid attenuation %q (expected integer dB, 0-255)", fs.Arg(0))
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
		return usagef("usage: set-cal-att <dB>")
	}
	db, err := strconv.Atoi(fs.Arg(0))
	if err != nil || db < 0 || db > 255 {
		return usagef("invalid attenuation %q (expected integer dB, 0-255)", fs.Arg(0))
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
		return usagef("usage: set-channels <on|off>")
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
		return usagef("usage: set-cal <on|off>")
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
		return usagef("usage: set-cal-source <internal|external>  (whalepod CAL_SEL)")
	}
	var internal bool
	switch strings.ToLower(fs.Arg(0)) {
	case "internal", "int", "noise", "on":
		internal = true
	case "external", "ext", "off":
		internal = false
	default:
		return usagef("invalid source %q: use internal or external", fs.Arg(0))
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

func cmdSetClockSource(args []string) error {
	fs := flag.NewFlagSet("set-clock", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return usagef("usage: set-clock <internal|external>")
	}
	var internal bool
	switch strings.ToLower(fs.Arg(0)) {
	case "internal", "int", "onboard", "on":
		internal = true
	case "external", "ext", "ref", "off":
		internal = false
	default:
		return usagef("invalid clock source %q: use internal or external", fs.Arg(0))
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	if err := c.SetClockSource(internal); err != nil {
		return err
	}
	src := "external"
	if internal {
		src = "internal"
	}
	fmt.Printf("OK (clock source = %s)\n", src)
	return nil
}

func cmdSetLo(args []string) error {
	fs := flag.NewFlagSet("set-lo", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return usagef("usage: set-lo <MHz>  (Barracuda LMX2595 LO; 0 powers it down)")
	}
	mhz, err := strconv.Atoi(fs.Arg(0))
	if err != nil || mhz < 0 || mhz > 20000 {
		return usagef("invalid frequency %q (expected integer MHz, 0-20000)", fs.Arg(0))
	}
	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()
	if err := c.SetLoFrequency(int32(mhz)); err != nil {
		return err
	}
	fmt.Printf("OK (LO = %d MHz)\n", mhz)
	return nil
}

func cmdSetDsa(args []string) error {
	fs := flag.NewFlagSet("set-dsa", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)
	if fs.NArg() < 1 {
		return usagef("usage: set-dsa <dB>  (Barracuda HMC1119, 0-31.75 dB in 0.25 dB steps)")
	}
	db, err := strconv.ParseFloat(fs.Arg(0), 64)
	if err != nil || db < 0 || db > 31.75 {
		return usagef("invalid attenuation %q (expected 0-31.75 dB)", fs.Arg(0))
	}
	quarter := int32(db*4 + 0.5) // 0.25 dB per LSB
	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()
	if err := c.SetDsaAttenuation(quarter); err != nil {
		return err
	}
	fmt.Printf("OK (DSA = %.2f dB)\n", float64(quarter)/4)
	return nil
}

func cmdSetChirp(args []string) error {
	fs := flag.NewFlagSet("set-chirp", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	start := fs.Int("start", 10000, "ramp start frequency in MHz (VCO output)")
	dev := fs.Int("dev", 1000, "chirp deviation / bandwidth in MHz")
	timeUs := fs.Int("time", 35, "ramp time in microseconds")
	mode := fs.String("mode", "sawtooth", "ramp shape: sawtooth|triangle")
	triggered := fs.Bool("triggered", false, "one ramp per TXDATA trigger (else free-running)")
	off := fs.Bool("off", false, "disable the ramp (hold CW at --start)")

	// Extended waveform options. Every one of these defaults to zero/false, so
	// omitting them all reproduces the classic five-parameter chirp exactly.
	parabolic := fs.Bool("parabolic", false, "nonlinear (parabolic) ramp; --dev is the total span")
	dualRamp := fs.Bool("dual-ramp", false, "second ramp rate from bank 1 (--ramp2-deviation/--ramp2-time)")
	ramp2Dev := fs.Int("ramp2-deviation", 0, "dual ramp: bank-1 sweep span in MHz")
	ramp2Time := fs.Int("ramp2-time", 0, "dual ramp: bank-1 ramp time in microseconds")
	fastRamp := fs.Bool("fast-ramp", false, "two-slope triangular ramp; down-slope time from --fast-down-time")
	fastDown := fs.Int("fast-down-time", 0, "fast ramp: down-slope time in microseconds")
	fskKHz := fs.Int("fsk-khz", 0, "FSK deviation superimposed on the ramp, in kHz (0 = off)")
	delayedStart := fs.Bool("delayed-start", false, "delay the ramp start by --delay-us")
	rampDelay := fs.Bool("ramp-delay", false, "dwell for --delay-us between ramps")
	delayUs := fs.Int("delay-us", 0, "delay/dwell duration in microseconds (used by --delayed-start/--ramp-delay/--trigger-delay)")
	triDelay := fs.Bool("triangular-delay", false, "clipped triangular delay (requires --ramp-delay and --mode triangle)")
	triggerDelay := fs.Bool("trigger-delay", false, "apply --delay-us when TXDATA triggers a ramp")
	extStepClock := fs.Bool("external-step-clock", false, "TXDATA advances each ramp step (no internal step clock)")
	txdataInvert := fs.Bool("txdata-invert", false, "take TXDATA events on the falling edge")
	rampCompleteOut := fs.Bool("ramp-complete-out", false, "route the ramp-complete pulse to MUXOUT (costs lock detect on that pin)")
	_ = fs.Parse(args)

	var cm pb.ChirpMode
	triangle := false
	switch strings.ToLower(*mode) {
	case "sawtooth", "saw":
		cm = pb.ChirpMode_CHIRP_MODE_SAWTOOTH_CONTINUOUS
		if *triggered {
			cm = pb.ChirpMode_CHIRP_MODE_SAWTOOTH_TRIGGERED
		}
	case "triangle", "tri":
		triangle = true
		cm = pb.ChirpMode_CHIRP_MODE_TRIANGLE_CONTINUOUS
		if *triggered {
			cm = pb.ChirpMode_CHIRP_MODE_TRIANGLE_TRIGGERED
		}
	default:
		return usagef("invalid --mode %q (expected sawtooth|triangle)", *mode)
	}
	if *start < 0 || *dev < 0 || *timeUs <= 0 {
		return usagef("--start/--dev must be >= 0 and --time > 0")
	}
	// These all cross into unsigned wire fields, so a negative here would wrap
	// into an enormous value instead of being rejected by the device.
	for _, n := range []struct {
		flag string
		val  int
	}{
		{"--ramp2-deviation", *ramp2Dev},
		{"--ramp2-time", *ramp2Time},
		{"--fast-down-time", *fastDown},
		{"--fsk-khz", *fskKHz},
		{"--delay-us", *delayUs},
	} {
		if n.val < 0 {
			return usagef("%s must be >= 0 (got %d)", n.flag, n.val)
		}
	}
	// Pure logical constraints from the wire spec — check them here so the user
	// gets the flag names back instead of a device-side refusal. Anything that
	// depends on the hardware (RF ceiling, delay range at the current f_PFD,
	// integer-N mode) is left to the firmware, which answers with a detail
	// string naming the constraint.
	if *parabolic && (*dualRamp || *fastRamp) {
		return usagef("--parabolic excludes --dual-ramp and --fast-ramp")
	}
	if *triDelay && !*rampDelay {
		return usagef("--triangular-delay requires --ramp-delay")
	}
	if *triDelay && !triangle {
		return usagef("--triangular-delay requires --mode triangle")
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()
	locked, err := c.SetChirpEx(client.ChirpConfig{
		StartFreqMHz:       int32(*start),
		DeviationMHz:       int32(*dev),
		RampTimeUs:         uint32(*timeUs),
		Mode:               cm,
		Enabled:            !*off,
		Parabolic:          *parabolic,
		DualRamp:           *dualRamp,
		Ramp2DeviationMHz:  int32(*ramp2Dev),
		Ramp2TimeUs:        uint32(*ramp2Time),
		FastRamp:           *fastRamp,
		FastRampDownTimeUs: uint32(*fastDown),
		FskOnRampKHz:       uint32(*fskKHz),
		DelayedStart:       *delayedStart,
		RampDelay:          *rampDelay,
		DelayUs:            uint32(*delayUs),
		TriangularDelay:    *triDelay,
		TxdataTriggerDelay: *triggerDelay,
		ExternalStepClock:  *extStepClock,
		TxdataInvert:       *txdataInvert,
		MuxoutRampComplete: *rampCompleteOut,
	})
	if err != nil {
		return err
	}
	if *off {
		fmt.Printf("OK (ramp disabled; CW at %d MHz)\n", *start)
		return nil
	}

	// Only mention the extended options that are actually in play, so the
	// classic chirp keeps printing exactly the line it always did.
	var opts []string
	if *parabolic {
		opts = append(opts, "parabolic")
	}
	if *dualRamp {
		opts = append(opts, fmt.Sprintf("dual ramp %d MHz/%d us", *ramp2Dev, *ramp2Time))
	}
	if *fastRamp {
		opts = append(opts, fmt.Sprintf("fast ramp, down %d us", *fastDown))
	}
	if *fskKHz > 0 {
		opts = append(opts, fmt.Sprintf("FSK %d kHz", *fskKHz))
	}
	if *delayedStart {
		opts = append(opts, fmt.Sprintf("delayed start %d us", *delayUs))
	}
	if *rampDelay {
		opts = append(opts, fmt.Sprintf("ramp delay %d us", *delayUs))
	}
	if *triDelay {
		opts = append(opts, "triangular delay")
	}
	if *triggerDelay {
		opts = append(opts, fmt.Sprintf("trigger delay %d us", *delayUs))
	}
	if *extStepClock {
		opts = append(opts, "external step clock")
	}
	if *txdataInvert {
		opts = append(opts, "TXDATA inverted")
	}
	if *rampCompleteOut {
		opts = append(opts, "ramp-complete on MUXOUT")
	}
	extra := ""
	if len(opts) > 0 {
		extra = " [" + strings.Join(opts, ", ") + "]"
	}
	trig := ""
	if *triggered {
		trig = ", triggered"
	}
	fmt.Printf("OK (chirp %d MHz + %d MHz over %d us, %s%s; locked=%v)%s\n",
		*start, *dev, *timeUs, strings.ToLower(*mode), trig, locked, extra)
	// With ramp-complete on MUXOUT the pin no longer carries digital lock
	// detect, so the value printed above is the lock state from before the
	// reroute. Say so rather than letting it read as live.
	if *rampCompleteOut {
		fmt.Fprintln(os.Stderr, "note: MUXOUT now carries ramp-complete, so locked= is the state sampled before the reroute, not a live indication")
	}
	return nil
}

func cmdSetFsk(args []string) error {
	fs := flag.NewFlagSet("set-fsk", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	center := fs.Int("center", 10000, "FSK center frequency in MHz")
	devKHz := fs.Int("dev-khz", 0, "FSK deviation in kHz (the hop is +/- this around --center)")
	off := fs.Bool("off", false, "disable FSK (hold CW at --center)")
	_ = fs.Parse(args)

	if *center < 0 {
		return usagef("--center must be >= 0 (got %d)", *center)
	}
	if *devKHz < 0 {
		return usagef("--dev-khz must be >= 0 (got %d)", *devKHz)
	}
	if !*off && *devKHz == 0 {
		return usagef("--dev-khz must be > 0 to enable FSK (or pass --off to revert to CW)")
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()
	if err := c.SetFsk(int32(*center), uint32(*devKHz), !*off); err != nil {
		return err
	}
	if *off {
		fmt.Printf("OK (FSK disabled; CW at %d MHz)\n", *center)
	} else {
		fmt.Printf("OK (FSK %d MHz +/- %d kHz; data on TXDATA)\n", *center, *devKHz)
	}
	return nil
}

// cmdSetPhase takes its arguments positionally rather than as flags: the flag
// package stops parsing at the first non-flag token, so a mixed
// `set-phase psk --deg 90` would silently drop the degrees.
func cmdSetPhase(args []string) error {
	fs := flag.NewFlagSet("set-phase", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return usagef("usage: set-phase <off|psk|static> [degrees]  (Barracuda ADF4159 output phase; e.g. set-phase psk 90)")
	}
	var mode pb.PhaseMode
	switch strings.ToLower(fs.Arg(0)) {
	case "off", "none", "disable", "disabled":
		mode = pb.PhaseMode_PHASE_MODE_OFF
	case "psk", "txdata":
		mode = pb.PhaseMode_PHASE_MODE_PSK
	case "static", "adjust":
		mode = pb.PhaseMode_PHASE_MODE_STATIC
	default:
		return usagef("invalid phase mode %q (expected off, psk, or static)", fs.Arg(0))
	}
	var deg float64
	if fs.NArg() > 1 {
		var err error
		deg, err = strconv.ParseFloat(fs.Arg(1), 64)
		if err != nil {
			return usagef("invalid phase %q (expected degrees, e.g. 90 or -45)", fs.Arg(1))
		}
	} else if mode != pb.PhaseMode_PHASE_MODE_OFF {
		return usagef("set-phase %s needs a phase in degrees (e.g. set-phase %s 90)", fs.Arg(0), fs.Arg(0))
	}
	// The firmware normalises into (-180, 180] before quantising, so anything
	// inside a full turn is meaningful. Beyond that it's almost certainly a
	// units mistake (millidegrees typed where degrees were wanted).
	if deg < -360 || deg > 360 {
		return usagef("phase %g is out of range (expected -360..360 degrees; the device normalises into (-180, 180])", deg)
	}
	milli := int32(math.Round(deg * 1000))

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()
	if err := c.SetPhase(mode, milli); err != nil {
		return err
	}
	switch mode {
	case pb.PhaseMode_PHASE_MODE_OFF:
		fmt.Println("OK (phase control off; phase word cleared)")
	case pb.PhaseMode_PHASE_MODE_PSK:
		fmt.Printf("OK (PSK on TXDATA, +/-%g deg)\n", deg)
	case pb.PhaseMode_PHASE_MODE_STATIC:
		fmt.Printf("OK (static phase increment %+g deg)\n", deg)
	}
	return nil
}

func cmdSetAdfRef(args []string) error {
	fs := flag.NewFlagSet("set-adf-ref", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	// FULL-STATE like set-adf-loop / set-adf-power: every call programs all five
	// fields, so the defaults here are the driver's Barracuda baseline (R = 1, no
	// doubler, no /2, 8/9 prescaler, CP code 7) rather than "leave it alone".
	//
	// Each flag has a short alias matching the firmware's own control_tool, so
	// the same command line works against either CLI.
	var rCounter, cpCode int
	var doubler, div2 bool
	var prescaler string
	fs.IntVar(&rCounter, "r-counter", 1, "reference divider R, 1-32")
	fs.IntVar(&rCounter, "r", 1, "alias for --r-counter")
	fs.IntVar(&cpCode, "cp-code", 7, "charge-pump current code, 0-15 (0.31-5 mA at RSET = 5.1k)")
	fs.IntVar(&cpCode, "cp", 7, "alias for --cp-code")
	fs.BoolVar(&doubler, "ref-doubler", false, "enable the reference doubler (needs REFIN <= 50 MHz; limits --cp-code to 0-7)")
	fs.BoolVar(&doubler, "doubler", false, "alias for --ref-doubler")
	fs.BoolVar(&div2, "ref-div2", false, "divide by 2 after the R counter (50% PFD duty, required for CSR)")
	fs.BoolVar(&div2, "div2", false, "alias for --ref-div2")
	fs.StringVar(&prescaler, "prescaler", "8/9", "RF prescaler: 4/5 (RF <= 8 GHz, INT >= 23) or 8/9 (INT >= 75)")
	_ = fs.Parse(args)

	if rCounter < 1 || rCounter > 32 {
		return usagef("invalid --r-counter %d (expected 1-32)", rCounter)
	}
	if cpCode < 0 || cpCode > 15 {
		return usagef("invalid --cp-code %d (expected 0-15)", cpCode)
	}
	var pre89 bool
	switch strings.ToLower(strings.TrimSpace(prescaler)) {
	case "4/5", "45", "4-5":
		pre89 = false
	case "8/9", "89", "8-9":
		pre89 = true
	default:
		return usagef("invalid --prescaler %q (expected 4/5 or 8/9)", prescaler)
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()
	if err := c.SetAdfRefConfig(client.AdfRefConfig{
		RCounter:      uint32(rCounter),
		RefDoubler:    doubler,
		RefDiv2:       div2,
		Prescaler89:   pre89,
		CpCurrentCode: uint32(cpCode),
	}); err != nil {
		return err
	}
	preName := "4/5"
	if pre89 {
		preName = "8/9"
	}
	fmt.Printf("OK (R=%d, doubler=%v, div2=%v, prescaler=%s, CP code=%d)\n",
		rCounter, doubler, div2, preName, cpCode)
	return nil
}

// cmdSetAdfLoop programs the ADF4159 loop-quality block. This request is
// full-state: the device applies all five fields every time, so an omitted flag
// turns that feature OFF rather than leaving it as it was.
func cmdSetAdfLoop(args []string) error {
	fs := flag.NewFlagSet("set-adf-loop", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	var bleed bool
	csr := fs.Bool("csr", false, "cycle slip reduction (needs --cp-code 0 and --ref-div2 on set-adf-ref)")
	fs.BoolVar(&bleed, "negative-bleed", false, "enable negative bleed current")
	fs.BoolVar(&bleed, "bleed", false, "alias for --negative-bleed")
	bleedCode := fs.Int("bleed-code", 0, "negative bleed magnitude, 0-7 (3.73-916 uA)")
	lolDisable := fs.Bool("lol-disable", false, "disable loss-of-lock indication (more robust lock detect)")
	integerN := fs.Bool("integer-n", false, "integer-N mode: sigma-delta off, FRAC forced 0 (blocks chirp/FSK/PSK/phase)")
	_ = fs.Parse(args)

	if *bleedCode < 0 || *bleedCode > 7 {
		return usagef("invalid --bleed-code %d (expected 0-7)", *bleedCode)
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()
	if err := c.SetAdfLoopConfig(client.AdfLoopConfig{
		Csr:               *csr,
		NegativeBleed:     bleed,
		NegativeBleedCode: uint32(*bleedCode),
		LolDisable:        *lolDisable,
		IntegerNMode:      *integerN,
	}); err != nil {
		return err
	}
	fmt.Printf("OK (csr=%v, negative-bleed=%v code=%d, lol-disable=%v, integer-n=%v)\n",
		*csr, bleed, *bleedCode, *lolDisable, *integerN)
	return nil
}

// cmdSetAdfPower drives the ADF4159 power controls. Full-state like
// set-adf-loop: `set-adf-power` with no flags returns the part to normal
// running (powered up, charge pump active, counters out of reset).
func cmdSetAdfPower(args []string) error {
	fs := flag.NewFlagSet("set-adf-power", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	var threeState bool
	powerDown := fs.Bool("power-down", false, "software power-down (registers retained)")
	fs.BoolVar(&threeState, "cp-three-state", false, "three-state the charge pump (parks VTUNE)")
	fs.BoolVar(&threeState, "cp-tristate", false, "alias for --cp-three-state")
	counterReset := fs.Bool("counter-reset", false, "hold the RF counters in reset")
	_ = fs.Parse(args)

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()
	if err := c.SetAdfPower(client.AdfPowerConfig{
		PowerDown:    *powerDown,
		CpThreeState: threeState,
		CounterReset: *counterReset,
	}); err != nil {
		return err
	}
	if !*powerDown && !threeState && !*counterReset {
		fmt.Println("OK (ADF4159 running: powered up, charge pump active, counters released)")
	} else {
		fmt.Printf("OK (power-down=%v, cp-three-state=%v, counter-reset=%v)\n",
			*powerDown, threeState, *counterReset)
	}
	return nil
}

func cmdSetPll(args []string) error {
	fs := flag.NewFlagSet("set-pll", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return usagef("usage: set-pll <MHz>  (STRAPS LMX2595 LO; e.g. set-pll 3500)")
	}
	mhz, err := strconv.Atoi(fs.Arg(0))
	if err != nil || mhz < 0 || mhz > 15000 {
		return usagef("invalid frequency %q (expected integer MHz, 0-15000)", fs.Arg(0))
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	if err := c.SetPllFrequency(int32(mhz)); err != nil {
		return err
	}
	fmt.Printf("OK (PLL LO = %d MHz)\n", mhz)
	return nil
}

// cmdSetSwitchChannel drives the PE42582 SP8T switch board. Deliberately not
// named "set-channel": that reads as a singular of the unrelated `set-channels
// <on|off>`, and the two would silently do very different things to a typo.
func cmdSetSwitchChannel(args []string) error {
	fs := flag.NewFlagSet("set-switch-channel", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return usagef("usage: set-switch-channel <1-8|off>  (SP8T RF-switch board; e.g. set-switch-channel 3)")
	}

	// Channel 0 is the firmware's all-off / all-isolated state. Spell it "off"
	// on the command line so isolating the switch is an explicit word rather
	// than a magic zero.
	arg := fs.Arg(0)
	var ch int
	switch strings.ToLower(arg) {
	case "off", "none", "isolate":
		ch = 0
	default:
		var err error
		ch, err = strconv.Atoi(arg)
		if err != nil || ch < 0 || ch > 8 {
			return usagef("invalid channel %q (expected 1-8, or \"off\")", arg)
		}
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	if err := c.SetRfSwitchChannel(int32(ch)); err != nil {
		return err
	}
	if ch == 0 {
		fmt.Println("OK (all channels off / isolated)")
	} else {
		fmt.Printf("OK (channel %d)\n", ch)
	}
	return nil
}

func cmdSetBand(args []string) error {
	fs := flag.NewFlagSet("set-band", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return usagef("usage: set-band <band>  (STRAPS band preset; accepts %s, a canonical RF_BAND_* name, or an integer 0-4)", strings.Join(rfBandAliasNames(), ", "))
	}
	v, err := resolveEnumArg(fs.Arg(0), pb.RfBand_value, rfBandAliases)
	if err != nil {
		return usagef("invalid band: %w", err)
	}
	if _, ok := pb.RfBand_name[v]; !ok {
		return usagef("invalid band %d: out of range (expected an integer 0-4, a span like %s, or an RF_BAND_* name)", v, strings.Join(rfBandAliasNames(), "/"))
	}
	band := pb.RfBand(v)

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	if err := c.SetRfBand(band); err != nil {
		return err
	}
	fmt.Printf("OK (band = %s)\n", band)
	return nil
}

func cmdApplyJSON(args []string) error {
	fs := flag.NewFlagSet("apply-json", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	if fs.NArg() < 1 {
		return usagef("usage: apply-json <file>")
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
		return usagef("parse %s: %v", fs.Arg(0), err)
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

// ----- batch apply (stdin/stdout JSON) --------------------------------------
//
// The `apply` command is the machine-facing entry point: it reads one JSON
// document (from stdin by default, or a file), opens the transport once,
// applies every field that's present, then closes the transport and writes a
// JSON result to stdout. It's the "call the binary from Python and set several
// things at once" path — no need to parse human text or spawn a process per
// setting.

// batchConfig is the JSON document `apply` accepts. Every field is optional and
// applied only when present, so a caller can set any subset of device state in
// one shot. Pointer types let us tell "field omitted" apart from "field set to
// zero/false" — the whole point, since channels_enabled=false must differ from
// channels_enabled absent.
type batchConfig struct {
	AttenuationDb       *int            `json:"attenuation_db"`
	CalAttenuationDb    *int            `json:"cal_attenuation_db"`
	ChannelsEnabled     *bool           `json:"channels_enabled"`
	CalEnabled          *bool           `json:"cal_enabled"`
	CalSourceInternal   *bool           `json:"cal_source_internal"`
	ClockSourceInternal *bool           `json:"clock_source_internal"`
	RfBand              *enumField      `json:"rf_band"`
	RfSwitch            *enumField      `json:"rf_switch"`
	MixerSwitch         *enumField      `json:"mixer_switch"`
	IfSwitch            *enumField      `json:"if_switch"`
	PllFrequencyMhz     *int            `json:"pll_frequency_mhz"`
	RfSwitchChannel     *int            `json:"rf_switch_channel"`
	Barracuda           *barracudaBatch `json:"barracuda"`
	Network             *networkBatch   `json:"network"`
}

// barracudaBatch mirrors the simplified customer cw/sweep interface for JSON
// automation. It intentionally exposes no engineering-only waveform fields.
type barracudaBatch struct {
	Mode           string  `json:"mode"`
	IFFrequencyMHz *int    `json:"if_frequency_mhz"`
	StartIFMHz     *int    `json:"start_if_mhz"`
	StopIFMHz      *int    `json:"stop_if_mhz"`
	SweepTime      string  `json:"sweep_time"`
	AttenuationDB  float64 `json:"attenuation_db"`
	Clock          string  `json:"clock"`
}

// networkBatch mirrors the SaveConfigRequest network fields. Omitted fields are
// backfilled from the device's current config, so a caller can change just the
// IP without restating the gateway/subnet/hostname.
type networkBatch struct {
	StaticIP      string `json:"static_ip"`
	StaticGateway string `json:"static_gateway"`
	StaticSubnet  string `json:"static_subnet"`
	Hostname      string `json:"hostname"`
}

// enumField holds a raw JSON scalar (a number or a string) for one of the
// switch enums, resolved later against that enum's value map. Keeping it raw
// until resolve() lets us accept the protobuf enum integer, its canonical name
// (e.g. "RF_SWITCH_OPTION_2GHZ_LPF"), or a short friendly alias (e.g. "2ghz").
type enumField struct{ raw json.RawMessage }

func (e *enumField) UnmarshalJSON(b []byte) error {
	e.raw = append([]byte(nil), b...)
	return nil
}

func (e enumField) resolve(canonical map[string]int32, aliases map[string]int32) (int32, error) {
	// Numeric form: the raw protobuf enum value.
	var n int32
	if err := json.Unmarshal(e.raw, &n); err == nil {
		return n, nil
	}
	var s string
	if err := json.Unmarshal(e.raw, &s); err != nil {
		return 0, fmt.Errorf("expected a string or integer, got %s", string(e.raw))
	}
	if v, ok := canonical[strings.ToUpper(s)]; ok {
		return v, nil
	}
	if v, ok := aliases[strings.ToLower(s)]; ok {
		return v, nil
	}
	accepted := make([]string, 0, len(aliases))
	for a := range aliases {
		accepted = append(accepted, a)
	}
	sort.Strings(accepted)
	return 0, fmt.Errorf("unknown value %q (accepted: %s, a canonical enum name, or an integer)", s, strings.Join(accepted, ", "))
}

// resolveEnumArg resolves a command-line enum argument — a canonical name, a
// friendly alias, or a bare integer — to its protobuf value. Unlike quoting the
// argument unconditionally, a bare integer is passed through as a JSON number so
// the numeric form of resolve() accepts it (e.g. `set-band 2`).
func resolveEnumArg(arg string, canonical, aliases map[string]int32) (int32, error) {
	var raw json.RawMessage
	if _, err := strconv.Atoi(arg); err == nil {
		raw = json.RawMessage(arg) // bare integer -> JSON number
	} else {
		raw = json.RawMessage(strconv.Quote(arg)) // name/alias -> JSON string
	}
	return (&enumField{raw: raw}).resolve(canonical, aliases)
}

// Friendly aliases for the switch enums, in addition to the canonical protobuf
// names (RfSwitchOption_value etc.) and raw integers, which resolve() also
// accepts.
var (
	rfSwitchAliases    = map[string]int32{"4ghz_lpf": 0, "4ghz": 0, "2ghz_lpf": 1, "2ghz": 1}
	mixerSwitchAliases = map[string]int32{"mixer": 0, "bypass": 1}
	ifSwitchAliases    = map[string]int32{"900mhz_lpf": 0, "900mhz": 0, "1_2ghz_bandpass": 1, "1_2ghz": 1, "1.2ghz": 1}
	// STRAPS band presets (control.RfBand). Aliases are the band's frequency span
	// in MHz; resolve() also accepts the canonical RF_BAND_* name or the integer.
	rfBandAliases = map[string]int32{
		"10-900": 0, "0-900": 0,
		"900-1800":  1,
		"1800-2700": 2,
		"2700-3600": 3,
		"3600-4500": 4,
	}
)

// rfBandAliasNames returns the friendly band aliases, sorted, for help text.
func rfBandAliasNames() []string {
	names := make([]string, 0, len(rfBandAliases))
	for a := range rfBandAliases {
		names = append(names, a)
	}
	sort.Strings(names)
	return names
}

// applyResult is the JSON written to stdout by `apply`. `applied` lists the
// operations that succeeded, in order; on failure `error`/`failed_at` describe
// the first operation that failed (everything listed in `applied` still took
// effect). `status` is a read-back of device state after applying, omitted when
// a network change rebooted the device.
type applyResult struct {
	OK       bool        `json:"ok"`
	Applied  []string    `json:"applied"`
	Error    string      `json:"error,omitempty"`
	FailedAt string      `json:"failed_at,omitempty"`
	Rebooted bool        `json:"rebooted,omitempty"`
	Status   *statusJSON `json:"status,omitempty"`
}

// statusJSON is the machine-readable form of GetStatusResponse. Switch enums
// are rendered as their canonical names so the output round-trips back through
// `apply` as input.
type statusJSON struct {
	BoardType                string                   `json:"board_type,omitempty"`
	ChannelsEnabled          bool                     `json:"channels_enabled"`
	CalibrationEnabled       bool                     `json:"calibration_enabled"`
	CalSourceInternal        bool                     `json:"cal_source_internal"`
	ClockSourceInternal      bool                     `json:"clock_source_internal"`
	AttenuationDb            int32                    `json:"attenuation_db"`
	CalAttenuationDb         int32                    `json:"cal_attenuation_db"`
	LoFrequencyMhz           int32                    `json:"lo_frequency_mhz"`
	RfSwitch                 string                   `json:"rf_switch"`
	MixerSwitch              string                   `json:"mixer_switch"`
	IfSwitch                 string                   `json:"if_switch"`
	RfSwitchChannel          int32                    `json:"rf_switch_channel"`
	PLLLocked                bool                     `json:"pll_locked,omitempty"`
	RefLocked                bool                     `json:"ref_locked,omitempty"`
	McuTemperatureC          float32                  `json:"mcu_temperature_c,omitempty"`
	McuTemperatureBootSample bool                     `json:"mcu_temperature_is_boot_sample,omitempty"`
	Barracuda                *pb.BarracudaDiagnostics `json:"barracuda,omitempty"`
}

func statusToJSON(s *pb.GetStatusResponse) *statusJSON {
	return &statusJSON{
		BoardType:          s.BoardType,
		ChannelsEnabled:    s.ChannelsEnabled,
		CalibrationEnabled: s.CalibrationEnabled,
		CalSourceInternal:  s.CalSourceInternal,
		// The wire field is clock_source_external (opposite sense); the JSON
		// stays clock_source_internal for backward compatibility, so invert.
		ClockSourceInternal:      !s.ClockSourceExternal,
		AttenuationDb:            s.AttenuationDb,
		CalAttenuationDb:         s.CalAttenuationDb,
		LoFrequencyMhz:           s.LoFrequencyMhz,
		RfSwitch:                 s.RfSwitch.String(),
		MixerSwitch:              s.MixerSwitch.String(),
		IfSwitch:                 s.IfSwitch.String(),
		RfSwitchChannel:          s.RfSwitchChannel,
		PLLLocked:                s.PllLocked,
		RefLocked:                s.RefLocked,
		McuTemperatureC:          s.McuTemperatureC,
		McuTemperatureBootSample: s.McuTemperatureIsBootSample,
		Barracuda:                s.Barracuda,
	}
}

func cmdApply(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	// Input: stdin by default (the Python-from-a-pipe case), or a file path;
	// "-" also means stdin.
	var raw []byte
	var err error
	if fs.NArg() == 0 || fs.Arg(0) == "-" {
		raw, err = io.ReadAll(os.Stdin)
	} else {
		raw, err = os.ReadFile(fs.Arg(0))
	}
	if err != nil {
		return err
	}

	var cfg batchConfig
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields() // a typo'd key must fail loudly, not silently no-op on hardware
	if err := dec.Decode(&cfg); err != nil {
		return usagef("parse JSON config: %v", err)
	}
	// Reject bad values before opening the transport, so a typo'd config fails
	// with a clear input error (exit 2) even when no device is connected.
	if err := cfg.validate(); err != nil {
		return err
	}

	tx, err := common.makeTransport()
	if err != nil {
		return err
	}
	c := client.New(tx)
	defer c.Close()

	result, applyErr := applyBatch(c, &cfg)

	// Always emit the JSON result on stdout, success or failure, so a caller can
	// read one machine-parseable object regardless of exit code. Diagnostics go
	// to stderr (see makeTransport / verbose), keeping stdout clean JSON.
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)
	return applyErr
}

// validate checks every statically-checkable field — enum values, numeric
// ranges, IP formats — so a bad document is rejected up front, before the
// transport is even opened, with a usageError (exit code exitUsage). This means
// a typo'd value fails the same way whether or not a device is connected, and
// never leaves the hardware half-configured. Fields that need live device state
// (the switch backfill) are still handled in applyBatch.
func (cfg *batchConfig) validate() error {
	if cfg.Barracuda != nil {
		if cfg.AttenuationDb != nil || cfg.CalAttenuationDb != nil || cfg.ChannelsEnabled != nil ||
			cfg.CalEnabled != nil || cfg.CalSourceInternal != nil || cfg.ClockSourceInternal != nil ||
			cfg.RfBand != nil || cfg.RfSwitch != nil || cfg.MixerSwitch != nil || cfg.IfSwitch != nil ||
			cfg.PllFrequencyMhz != nil || cfg.RfSwitchChannel != nil {
			return usagef("barracuda cannot be combined with legacy RF fields; network may be combined with it")
		}
		if _, _, err := cfg.Barracuda.customerConfigs(); err != nil {
			return usagef("barracuda: %v", err)
		}
	}
	resolves := []struct {
		field   string
		f       *enumField
		canon   map[string]int32
		aliases map[string]int32
		names   map[int32]string // valid resolved values, for a range check
	}{
		{"rf_band", cfg.RfBand, pb.RfBand_value, rfBandAliases, pb.RfBand_name},
		{"rf_switch", cfg.RfSwitch, pb.RfSwitchOption_value, rfSwitchAliases, pb.RfSwitchOption_name},
		{"mixer_switch", cfg.MixerSwitch, pb.MixerSwitchOption_value, mixerSwitchAliases, pb.MixerSwitchOption_name},
		{"if_switch", cfg.IfSwitch, pb.IfSwitchOption_value, ifSwitchAliases, pb.IfSwitchOption_name},
	}
	for _, r := range resolves {
		if r.f == nil {
			continue
		}
		v, err := r.f.resolve(r.canon, r.aliases)
		if err != nil {
			return usagef("%s: %v", r.field, err)
		}
		// A bare integer resolves without error even when out of range; reject
		// values that aren't a defined enum member.
		if _, ok := r.names[v]; !ok {
			return usagef("%s: %d is not a valid value", r.field, v)
		}
	}

	ranges := []struct {
		field  string
		v      *int
		lo, hi int
		unit   string
	}{
		{"attenuation_db", cfg.AttenuationDb, 0, 255, ""},
		{"cal_attenuation_db", cfg.CalAttenuationDb, 0, 255, ""},
		{"pll_frequency_mhz", cfg.PllFrequencyMhz, 0, 15000, " MHz"},
		{"rf_switch_channel", cfg.RfSwitchChannel, 0, 8, ""},
	}
	for _, r := range ranges {
		if r.v == nil {
			continue
		}
		if *r.v < r.lo || *r.v > r.hi {
			return usagef("%s: out of range %d (expected %d-%d%s)", r.field, *r.v, r.lo, r.hi, r.unit)
		}
	}

	if cfg.Network != nil {
		ips := []struct{ field, val string }{
			{"static_ip", cfg.Network.StaticIP},
			{"static_gateway", cfg.Network.StaticGateway},
			{"static_subnet", cfg.Network.StaticSubnet},
		}
		for _, ip := range ips {
			if ip.val == "" {
				continue
			}
			if _, err := parseIPv4(ip.val); err != nil {
				return usagef("network.%s: %v", ip.field, err)
			}
		}
	}
	return nil
}

// applyBatch applies each present field of cfg through c, in an order chosen so
// dependent settings land correctly (cal source before cal enable; network last
// because it reboots). It returns the result to serialize plus an error for the
// process exit code — both describe the same outcome.
func applyBatch(c *client.Client, cfg *batchConfig) (*applyResult, error) {
	res := &applyResult{Applied: []string{}}
	fail := func(field string, err error) (*applyResult, error) {
		res.OK = false
		res.Error = err.Error()
		res.FailedAt = field
		return res, fmt.Errorf("%s: %w", field, err)
	}
	// failInput is fail for bad-input problems (unknown enum value, out-of-range
	// number), so the CLI exits with exitUsage instead of a generic error while
	// the JSON result still reports the same field + message.
	failInput := func(field string, err error) (*applyResult, error) {
		return fail(field, &usageError{err: err})
	}

	if cfg.Barracuda != nil {
		cw, sweep, err := cfg.Barracuda.customerConfigs()
		if err != nil {
			return failInput("barracuda", err)
		}
		if cw != nil {
			if _, err := c.ConfigureBarracudaCW(*cw); err != nil {
				return fail("barracuda", err)
			}
			res.Applied = append(res.Applied, fmt.Sprintf("barracuda.cw=%dMHz-IF", cw.IFFrequencyMHz))
		} else {
			if _, err := c.ConfigureBarracudaSweep(*sweep); err != nil {
				return fail("barracuda", err)
			}
			res.Applied = append(res.Applied,
				fmt.Sprintf("barracuda.sweep=%d-%dMHz-IF/%s", sweep.StartIFMHz, sweep.StopIFMHz, sweep.SweepTime))
		}
	}

	// Clock source before anything PLL-related: it selects the LMX2595 reference,
	// so the LO must be (re)tuned after it, not before.
	if cfg.ClockSourceInternal != nil {
		if err := c.SetClockSource(*cfg.ClockSourceInternal); err != nil {
			return fail("clock_source_internal", err)
		}
		res.Applied = append(res.Applied, fmt.Sprintf("clock_source_internal=%v", *cfg.ClockSourceInternal))
	}

	// Band preset first: on STRAPS this sets all three switch banks AND tunes the
	// LO in one firmware call, so applying it before the individual switch / PLL
	// fields lets those override the preset when a caller specifies both.
	if cfg.RfBand != nil {
		v, err := cfg.RfBand.resolve(pb.RfBand_value, rfBandAliases)
		if err != nil {
			return failInput("rf_band", err)
		}
		band := pb.RfBand(v)
		if err := c.SetRfBand(band); err != nil {
			return fail("rf_band", err)
		}
		res.Applied = append(res.Applied, "rf_band="+band.String())
	}

	// Switches (rf/mixer/if) go out as one firmware call that sets all three, so
	// backfill any omitted ones from current status.
	if cfg.RfSwitch != nil || cfg.MixerSwitch != nil || cfg.IfSwitch != nil {
		st, err := c.GetStatus()
		if err != nil {
			return fail("switches", fmt.Errorf("read current switch state: %w", err))
		}
		rf, mixer, ifSw := st.RfSwitch, st.MixerSwitch, st.IfSwitch
		if cfg.RfSwitch != nil {
			v, err := cfg.RfSwitch.resolve(pb.RfSwitchOption_value, rfSwitchAliases)
			if err != nil {
				return failInput("rf_switch", err)
			}
			rf = pb.RfSwitchOption(v)
		}
		if cfg.MixerSwitch != nil {
			v, err := cfg.MixerSwitch.resolve(pb.MixerSwitchOption_value, mixerSwitchAliases)
			if err != nil {
				return failInput("mixer_switch", err)
			}
			mixer = pb.MixerSwitchOption(v)
		}
		if cfg.IfSwitch != nil {
			v, err := cfg.IfSwitch.resolve(pb.IfSwitchOption_value, ifSwitchAliases)
			if err != nil {
				return failInput("if_switch", err)
			}
			ifSw = pb.IfSwitchOption(v)
		}
		if err := c.SetSwitches(rf, mixer, ifSw); err != nil {
			return fail("switches", err)
		}
		res.Applied = append(res.Applied,
			"rf_switch="+rf.String(), "mixer_switch="+mixer.String(), "if_switch="+ifSw.String())
	}

	// PLL after switches/band so an explicit LO wins over a band preset's LO.
	if cfg.PllFrequencyMhz != nil {
		mhz := *cfg.PllFrequencyMhz
		if mhz < 0 || mhz > 15000 {
			return failInput("pll_frequency_mhz", fmt.Errorf("out of range %d (expected 0-15000 MHz)", mhz))
		}
		if err := c.SetPllFrequency(int32(mhz)); err != nil {
			return fail("pll_frequency_mhz", err)
		}
		res.Applied = append(res.Applied, fmt.Sprintf("pll_frequency_mhz=%d", mhz))
	}

	if cfg.RfSwitchChannel != nil {
		ch := *cfg.RfSwitchChannel
		if ch < 0 || ch > 8 {
			return failInput("rf_switch_channel", fmt.Errorf("out of range %d (expected 0-8, 0 = all off)", ch))
		}
		if err := c.SetRfSwitchChannel(int32(ch)); err != nil {
			return fail("rf_switch_channel", err)
		}
		res.Applied = append(res.Applied, fmt.Sprintf("rf_switch_channel=%d", ch))
	}

	if cfg.AttenuationDb != nil {
		db := *cfg.AttenuationDb
		if db < 0 || db > 255 {
			return failInput("attenuation_db", fmt.Errorf("out of range %d (expected 0-255)", db))
		}
		if err := c.SetAttenuation(int32(db)); err != nil {
			return fail("attenuation_db", err)
		}
		res.Applied = append(res.Applied, fmt.Sprintf("attenuation_db=%d", db))
	}

	if cfg.CalAttenuationDb != nil {
		db := *cfg.CalAttenuationDb
		if db < 0 || db > 255 {
			return failInput("cal_attenuation_db", fmt.Errorf("out of range %d (expected 0-255)", db))
		}
		if err := c.SetCalAttenuation(int32(db)); err != nil {
			return fail("cal_attenuation_db", err)
		}
		res.Applied = append(res.Applied, fmt.Sprintf("cal_attenuation_db=%d", db))
	}

	if cfg.ChannelsEnabled != nil {
		if err := c.SetChannelsEnabled(*cfg.ChannelsEnabled); err != nil {
			return fail("channels_enabled", err)
		}
		res.Applied = append(res.Applied, fmt.Sprintf("channels_enabled=%v", *cfg.ChannelsEnabled))
	}

	// Cal source before cal enable: on Whalepod the noise-source amp only turns
	// on when cal mode is active AND the internal source is selected, so choose
	// the source first, then flip the mode.
	if cfg.CalSourceInternal != nil {
		if err := c.SetCalSource(*cfg.CalSourceInternal); err != nil {
			return fail("cal_source_internal", err)
		}
		res.Applied = append(res.Applied, fmt.Sprintf("cal_source_internal=%v", *cfg.CalSourceInternal))
	}
	if cfg.CalEnabled != nil {
		if err := c.SetCalEnabled(*cfg.CalEnabled); err != nil {
			return fail("cal_enabled", err)
		}
		res.Applied = append(res.Applied, fmt.Sprintf("cal_enabled=%v", *cfg.CalEnabled))
	}

	// Network last: SaveConfig reboots the device, so anything after it would
	// race the reboot.
	if cfg.Network != nil {
		cur, err := c.GetConfig()
		if err != nil {
			return fail("network", fmt.Errorf("read current config: %w", err))
		}
		pick := func(override string, fallback []byte) ([]byte, error) {
			if override == "" {
				return fallback, nil
			}
			return parseIPv4(override)
		}
		ipB, err := pick(cfg.Network.StaticIP, cur.StaticIp)
		if err != nil {
			return fail("network.static_ip", err)
		}
		gwB, err := pick(cfg.Network.StaticGateway, cur.StaticGateway)
		if err != nil {
			return fail("network.static_gateway", err)
		}
		snB, err := pick(cfg.Network.StaticSubnet, cur.StaticSubnet)
		if err != nil {
			return fail("network.static_subnet", err)
		}
		host := cur.MdnsHostname
		if cfg.Network.Hostname != "" {
			host = cfg.Network.Hostname
		}
		if err := c.SaveConfig(&pb.SaveConfigRequest{
			StaticIp:      ipB,
			StaticGateway: gwB,
			StaticSubnet:  snB,
			MdnsHostname:  host,
			MacAddress:    cur.MacAddress,
			SerialNumber:  cur.SerialNumber,
		}); err != nil {
			return fail("network", err)
		}
		res.Applied = append(res.Applied,
			"network.static_ip="+ipString(ipB), "network.hostname="+host)
		res.Rebooted = true
	}

	res.OK = true
	// Read back the resulting device state so the caller gets a confirmation of
	// what's now set — unless a network change just rebooted the device, in
	// which case it won't answer.
	if !res.Rebooted {
		if st, err := c.GetStatus(); err == nil {
			res.Status = statusToJSON(st)
		}
	}
	return res, nil
}

// ----- entry point -----------------------------------------------------------

func usage() {
	fmt.Fprint(os.Stderr, `Ocupoint Barracuda RF control

Usage:
  rf-control [--ip ADDRESS | --usb DEVICE] <command> [options]

Customer commands:
  cw --frequency MHz [--attenuation dB] [--clock internal|external]
                         Generate a CW signal at the requested IF.
  sweep --start MHz --stop MHz --time DURATION
        [--attenuation dB] [--clock internal|external]
                         Generate a continuous IF sweep. Example duration:
                         10s, 20ms, or 35us.
  status                 Show current IF, clock, attenuation, lock, and temperature.
  list                   Discover Ethernet and USB devices.
  get                    Show device identity and network configuration.
  set-ip [flags]         Change network configuration.
  version                Show the rf-control version.
  help                   Show this help.

Defaults and limits:
  Clock                  internal
  Attenuation            0 dB (nominal output -25 dBm)
  IF frequency range     50..1500 MHz
  Attenuation range      0..31.75 dB in 0.25 dB steps

Run 'rf-control cw --help' or 'rf-control sweep --help' for examples.
Run 'rf-control engineering-help' for service, diagnostics, firmware update,
and lower-level board commands.
`)
}

func engineeringUsage() {
	fmt.Fprint(os.Stderr, `WIZnet Pico RF control tool

Usage:
  rf-control [--usb /dev/ttyACM1 | --ip 172.16.22.30 --port 5000] <command> [args]

Commands:
  cw --frequency MHz [--attenuation dB] [--clock internal|external]
                         Customer CW operation at 50..1500 MHz IF.
                         0 dB attenuation is nominally -25 dBm.
  sweep --start MHz --stop MHz --time DURATION
        [--attenuation dB] [--clock internal|external]
                         Customer continuous sawtooth sweep. DURATION includes
                         units, for example 10s, 20ms, or 35us. Start/stop are
                         50..1500 MHz IF; 0 dB attenuation is nominally -25 dBm.
  list                   Discover Ethernet and USB devices matching firmware.
  get                    Print the current device configuration.
  set-ip [flags]         Change one or more of: --address, --gateway,
                         --subnet, --hostname. Preserves MAC + serial.
  apply-json <file>      Apply a JSON config file with fields:
                         static_ip, static_gateway, static_subnet, hostname.
  apply [file]           Read one JSON config (from stdin, or a file / "-")
                         describing any subset of device state, open the
                         transport once, apply it all, then print a JSON
                         result to stdout. This is the command to call from
                         Python/scripts. Fields (all optional):
                           barracuda{} customer plan (mode cw/sweep, frequencies,
                           sweep_time, attenuation_db, clock),
                           attenuation_db, cal_attenuation_db,
                           channels_enabled, cal_enabled, cal_source_internal,
                           clock_source_internal, rf_band, rf_switch,
                           mixer_switch, if_switch, pll_frequency_mhz,
                           rf_switch_channel, and a network{} block (static_ip,
                           static_gateway, static_subnet, hostname).
  status                 Print live RF status (channels, attenuation,
                         LO frequency, switch positions). On the SP8T
                         RF-switch board, prints the selected channel.
  engineering-status     Print status plus Barracuda LO/synthesizer details.
  gpio-selftest          Run the firmware's control-GPIO diagnostic (alias:
                         selftest). Drives every control pin low then high and
                         reads the pad back, printing a per-pin pass/fail table.
                         DISRUPTIVE (toggles power-enable/select lines); exits 5
                         if any pin is stuck.
  set-att <dB>           Set frontend attenuation in dB (e.g. 10).
  set-cal-att <dB>       Set calibration attenuation in dB.
  set-channels <on|off>  Enable or disable the RF channels.
  set-cal <on|off>       Enter/leave calibration mode (CAL_SW). With the internal
                         source selected this also turns on the noise-source amp.
  set-cal-source <internal|external>
                         Select the whalepod calibration source (CAL_SEL).
                         The internal noise-source amp turns on only in cal
                         mode with the internal source selected.
  set-clock <internal|external>
                         Select the board's internal or external reference.
  set-pll <MHz>          Tune the STRAPS LMX2595 LO (e.g. 3500). On Barracuda
                         this tunes the ADF4159 VCO CW tone.
  set-band <band>        Apply a STRAPS band preset (switches + LO in one shot).
                         Accepts a span like 1800-2700, an RF_BAND_* name, or 0-4.

  Barracuda RF module:
  set-lo <MHz>           Tune the LMX2595 LO (0 powers it down).
  set-dsa <dB>           HMC1119 attenuation, 0-31.75 dB in 0.25 dB steps.
  set-chirp [flags]      Program/arm the ADF4159 FMCW ramp. Flags:
                           --start MHz (10000)  --dev MHz (1000)  --time us (35)
                           --mode sawtooth|triangle  --triggered  --off
                         Extended waveform options (all default off — omit them
                         all for the classic chirp):
                           --parabolic  --dual-ramp --ramp2-deviation MHz
                           --ramp2-time us  --fast-ramp --fast-down-time us
                           --fsk-khz kHz  --delayed-start  --ramp-delay
                           --delay-us us  --triangular-delay  --trigger-delay
                           --external-step-clock  --txdata-invert
                           --ramp-complete-out
                         Reports the ADF4159 lock state after programming (with
                         --ramp-complete-out that's the state from before MUXOUT
                         was rerouted, not a live reading).
  set-fsk [flags]        ADF4159 FSK: --center MHz, --dev-khz kHz (the hop is
                         +/- that around center), --off to revert to CW. The
                         data stream itself is driven on the TXDATA pin.
  set-phase <off|psk|static> [degrees]
                         ADF4159 output phase, e.g. "set-phase psk 90". psk =
                         +/-degrees toggled from TXDATA; static = a one-shot
                         increment relative to the current phase; off clears the
                         phase word. Hardware resolution is 360/4096 (~0.088)
                         degrees.
  set-adf-ref [flags]    ADF4159 reference path:
                         f_PFD = REFIN x (1+doubler) / (R x (1+div2)).
                           --r-counter 1-32 (1)   --cp-code 0-15 (7)
                           --ref-doubler  --ref-div2
                           --prescaler 4/5|8/9 (8/9)
                         FULL STATE: the defaults above are the Barracuda
                         baseline, applied to anything you leave out. The device
                         revalidates the armed waveform at the new f_PFD and
                         refuses a change that would overrun the RF ceiling, so
                         program a frequency first.
  set-adf-loop [flags]   ADF4159 loop quality: --csr, --negative-bleed,
                         --bleed-code 0-7, --lol-disable, --integer-n.
                         FULL STATE: every call applies all five, so a flag you
                         omit is turned OFF, not left alone.
  set-adf-power [flags]  ADF4159 power controls: --power-down, --cp-three-state,
                         --counter-reset. FULL STATE like set-adf-loop — with no
                         flags it returns the part to normal running.
                         (The ADF4159 flags above also accept the firmware
                         control_tool's shorter spellings: --r, --cp, --doubler,
                         --div2, --bleed, --cp-tristate.)
  image-info <image.bin> Inspect a Barracuda OTA image without a device.
  flash [flags] <image.bin>
                         Flash over Ethernet port 5002 or framed USB, verify the
                         image and CRC, reboot, and confirm firmware version.
  set-switch-channel <1-8|off>
                         Route the SP8T RF-switch board's common port to one of
                         the eight channels, or "off" to isolate all of them.
  version                Print the binary's version and exit.

Transport selection (place before the command):
  --usb DEVICE   Use that USB serial device, e.g. /dev/cu.usbmodem101.
  --ip ADDRESS   Use TCP at that IPv4 address.
  (neither)      Auto-discover USB. Probes each candidate serial port;
                 the first one that replies to a GetConfigRequest is used.
                 Never falls back to TCP — pass --ip explicitly for that.
  --port PORT    TCP port (default 5000).

Exit codes:
  0  success
  1  unexpected internal error
  2  invalid input (unknown command, bad flag, or bad argument value)
  3  could not reach the device (connection refused, timeout, USB gone)
  4  the device received the request but rejected it (see the ErrorCode printed)
  5  a diagnostic ran but found a hardware fault (gpio-selftest: a stuck pin)

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
		os.Exit(exitUsage)
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
		// `version` and `help` also spell as flags, and a flag spelling can
		// never be picked as the subcommand below. Catch them here, before the
		// `--flag value` rule mistakes one for a transport flag. The loop stops
		// at the subcommand, so a later `--help` still reaches its own flagset.
		switch a {
		case "-V", "--version":
			fmt.Println(version)
			return
		case "-h", "--help":
			usage()
			return
		}
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
		os.Exit(exitUsage)
	}

	// Re-order: subcommand-name first, then all flags from both sides.
	cmd := args[cmdIdx]
	rest := append([]string{}, args[:cmdIdx]...)
	rest = append(rest, args[cmdIdx+1:]...)

	var err error
	switch cmd {
	case "list":
		err = cmdList(rest)
	case "cw":
		err = cmdCustomerCW(rest)
	case "sweep":
		err = cmdCustomerSweep(rest)
	case "get":
		err = cmdGet(rest)
	case "set-ip":
		err = cmdSetIP(rest)
	case "apply-json":
		err = cmdApplyJSON(rest)
	case "apply":
		err = cmdApply(rest)
	case "status":
		err = cmdStatus(rest)
	case "engineering-status":
		err = cmdEngineeringStatus(rest)
	case "gpio-selftest", "selftest":
		err = cmdGpioSelfTest(rest)
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
	case "set-clock":
		err = cmdSetClockSource(rest)
	case "set-pll":
		err = cmdSetPll(rest)
	case "set-lo":
		err = cmdSetLo(rest)
	case "set-dsa":
		err = cmdSetDsa(rest)
	case "set-chirp":
		err = cmdSetChirp(rest)
	case "set-fsk":
		err = cmdSetFsk(rest)
	case "set-phase":
		err = cmdSetPhase(rest)
	case "set-adf-ref":
		err = cmdSetAdfRef(rest)
	case "set-adf-loop":
		err = cmdSetAdfLoop(rest)
	case "set-adf-power":
		err = cmdSetAdfPower(rest)
	case "set-band":
		err = cmdSetBand(rest)
	case "set-switch-channel":
		err = cmdSetSwitchChannel(rest)
	case "image-info":
		err = cmdImageInfo(rest)
	case "flash":
		err = cmdFlash(rest)
	case "version":
		fmt.Println(version)
		return
	case "engineering-help":
		engineeringUsage()
		return
	case "help", "-h", "--help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
		os.Exit(exitUsage)
	}
	if err != nil {
		os.Exit(classifyExit(cmd, err))
	}
}

// classifyExit prints err to stderr and maps it to the process exit code that
// best describes it (see the exit* constants). It lets a caller tell an input
// mistake (fix your command) apart from the device rejecting the request or
// being unreachable — without parsing the message text.
func classifyExit(cmd string, err error) int {
	var ue *usageError
	if errors.As(err, &ue) {
		fmt.Fprintln(os.Stderr, "Error:", err)
		fmt.Fprintf(os.Stderr, "Run 'rf-control %s --help' for usage.\n", cmd)
		return exitUsage
	}
	// A diagnostic that ran fine but found a hardware fault (e.g. gpio-selftest
	// with a stuck pin). The command already printed its result table, so don't
	// print a redundant "Error:" line — just map it to the fault exit code.
	var fe *faultError
	if errors.As(err, &fe) {
		return exitFault
	}
	var de *client.DeviceError
	if errors.As(err, &de) {
		fmt.Fprintln(os.Stderr, "Error: the device rejected the request:", err)
		return exitDevice
	}
	var te *client.TransportError
	if errors.As(err, &te) || errors.Is(err, io.EOF) {
		fmt.Fprintln(os.Stderr, "Error: could not reach the device:", err)
		return exitTransport
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	return exitRuntime
}
