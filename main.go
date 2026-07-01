// rf-control is a single-binary CLI for configuring the WIZnet Pico RF
// control board over either TCP or USB (CDC1). It mirrors the Python
// config_tool.py and uses the same protobuf wire format.
package main

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
	"google.golang.org/protobuf/proto"

	pb "github.com/OcupointInc/rf-control/internal/controlpb"
)

// verbose is set by the -v / --verbose flag (see addCommonFlags). When true,
// transports hex-dump bytes sent and received and log timings.
var verbose bool

func vlogf(format string, args ...any) {
	if verbose {
		fmt.Fprintf(os.Stderr, "[v] "+format+"\n", args...)
	}
}

func vlogBytes(label string, b []byte) {
	if verbose {
		fmt.Fprintf(os.Stderr, "[v] %s (%d bytes):\n%s", label, len(b), hex.Dump(b))
	}
}

const (
	usbFrameMagic0 = 0xAA
	usbFrameMagic1 = 0x55
	usbReadTimeout = 2 * time.Second
	tcpDialTimeout = 5 * time.Second
	tcpReadTimeout = 5 * time.Second

	// The device's control port only has two listening sockets, and each
	// takes several of the device's main-loop iterations to cycle back to
	// LISTEN after a connection closes. Back-to-back connections (a burst,
	// or just fast sequential commands) can race that and get "connection
	// refused" or "connection reset by peer" even though the device itself
	// is healthy — a short retry clears it up. See rf-control-tcp-reconnect-race
	// in project notes for the full writeup.
	tcpMaxAttempts = 3
	tcpRetryDelay  = 150 * time.Millisecond

	// USB VID/PID of the firmware (see usb_descriptors.c). Surfaced in
	// `list` output but identification is done by protocol probe — that
	// avoids needing cgo for VID/PID enumeration on macOS.
	deviceVID = "2E8A"
	devicePID = "000A"
)

// transport is the abstraction over USB-serial and TCP.
type transport interface {
	send(p *pb.Packet) (*pb.Packet, error)
	close() error
}

// ----- TCP transport ---------------------------------------------------------

type tcpTransport struct {
	addr string
}

// send dials, sends one request, and reads one response, retrying on
// connection-level errors (the device refusing or resetting a fresh
// connection under a two-socket accept race — see tcpMaxAttempts above).
// SaveConfigRequest is exempt: the device intentionally drops the response
// and reboots after a save, and the caller already treats that as success,
// so retrying it would just risk re-triggering the flash write.
func (t *tcpTransport) send(p *pb.Packet) (*pb.Packet, error) {
	attempts := tcpMaxAttempts
	if _, isSaveConfig := p.MessageId.(*pb.Packet_SaveConfigRequest); isSaveConfig {
		attempts = 1
	}

	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		resp, err := t.sendOnce(p)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if attempt < attempts {
			// Jitter spreads out retries from concurrent callers that all hit
			// the race at once — without it they retry in lockstep and can
			// keep colliding on the device's 2 listening sockets.
			delay := tcpRetryDelay + time.Duration(rand.Int63n(int64(tcpRetryDelay)))
			vlogf("TCP request failed (attempt %d/%d): %v — retrying in %v", attempt, attempts, err, delay)
			time.Sleep(delay)
		}
	}
	if attempts > 1 {
		return nil, fmt.Errorf("after %d attempts: %w", attempts, lastErr)
	}
	return nil, lastErr
}

func (t *tcpTransport) sendOnce(p *pb.Packet) (*pb.Packet, error) {
	conn, err := net.DialTimeout("tcp", t.addr, tcpDialTimeout)
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", t.addr, err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(tcpReadTimeout))

	payload, err := proto.Marshal(p)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(payload); err != nil {
		return nil, err
	}

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}
	resp := &pb.Packet{}
	if err := proto.Unmarshal(buf[:n], resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (t *tcpTransport) close() error { return nil }

// ----- USB (CDC1) transport --------------------------------------------------

type usbTransport struct {
	port    serial.Port
	timeout time.Duration
}

func openUSB(devPath string) (*usbTransport, error) {
	mode := &serial.Mode{BaudRate: 115200}
	port, err := serial.Open(devPath, mode)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", devPath, err)
	}
	// The firmware's USB poll gates on tud_cdc_n_connected(), which is true
	// only when the host has asserted DTR. Some platforms / library versions
	// don't assert DTR by default on open, so force it on.
	if err := port.SetDTR(true); err != nil {
		vlogf("SetDTR(true) failed: %v (continuing anyway)", err)
	}
	if err := port.SetRTS(true); err != nil {
		vlogf("SetRTS(true) failed: %v (continuing anyway)", err)
	}
	if err := port.SetReadTimeout(100 * time.Millisecond); err != nil {
		_ = port.Close()
		return nil, err
	}
	vlogf("opened %s, DTR/RTS asserted", devPath)
	return &usbTransport{port: port, timeout: usbReadTimeout}, nil
}

func (u *usbTransport) send(p *pb.Packet) (*pb.Packet, error) {
	payload, err := proto.Marshal(p)
	if err != nil {
		return nil, err
	}
	if len(payload) > 0xFFFF {
		return nil, fmt.Errorf("payload too large: %d bytes", len(payload))
	}

	frame := make([]byte, 4+len(payload))
	frame[0] = usbFrameMagic0
	frame[1] = usbFrameMagic1
	binary.LittleEndian.PutUint16(frame[2:4], uint16(len(payload)))
	copy(frame[4:], payload)

	// Drain any stale input from a previous session.
	_ = u.port.ResetInputBuffer()

	vlogBytes("USB tx frame", frame)
	writeStart := time.Now()
	if _, err := u.port.Write(frame); err != nil {
		return nil, err
	}
	vlogf("USB write took %v", time.Since(writeStart))

	readStart := time.Now()
	respBytes, err := u.readFrame()
	vlogf("USB read took %v", time.Since(readStart))
	if err != nil {
		return nil, err
	}
	vlogBytes("USB rx payload", respBytes)
	resp := &pb.Packet{}
	if err := proto.Unmarshal(respBytes, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (u *usbTransport) close() error { return u.port.Close() }

func (u *usbTransport) readFrame() ([]byte, error) {
	deadline := time.Now().Add(u.timeout)
	state := 0
	var length uint16
	var lengthBuf [2]byte
	lengthIdx := 0
	var rawSeen []byte // for verbose mode: everything we saw on the wire

	one := make([]byte, 1)
	for time.Now().Before(deadline) {
		n, err := u.port.Read(one)
		if err != nil {
			if verbose && len(rawSeen) > 0 {
				vlogBytes("USB rx (raw, before error)", rawSeen)
			}
			return nil, err
		}
		if n == 0 {
			continue
		}
		b := one[0]
		if verbose {
			rawSeen = append(rawSeen, b)
		}

		switch state {
		case 0:
			if b == usbFrameMagic0 {
				state = 1
			}
		case 1:
			switch b {
			case usbFrameMagic1:
				state = 2
				lengthIdx = 0
			case usbFrameMagic0:
				// stay in state 1
			default:
				state = 0
			}
		case 2:
			lengthBuf[lengthIdx] = b
			lengthIdx++
			if lengthIdx == 2 {
				length = binary.LittleEndian.Uint16(lengthBuf[:])
				if length == 0 || length > 4096 {
					state = 0
					continue
				}
				state = 3
			}
		case 3:
			payload := make([]byte, length)
			payload[0] = b
			read := 1
			for read < int(length) && time.Now().Before(deadline) {
				m, err := u.port.Read(payload[read:])
				if err != nil {
					return nil, err
				}
				if verbose && m > 0 {
					rawSeen = append(rawSeen, payload[read:read+m]...)
				}
				read += m
			}
			if read != int(length) {
				if verbose && len(rawSeen) > 0 {
					vlogBytes("USB rx (raw, payload truncated)", rawSeen)
				}
				return nil, errors.New("usb: timed out reading payload")
			}
			return payload, nil
		}
	}
	if verbose {
		if len(rawSeen) == 0 {
			vlogf("USB rx: 0 bytes received before deadline (firmware never replied)")
		} else {
			vlogBytes("USB rx (raw, no valid frame)", rawSeen)
		}
	}
	return nil, errors.New("usb: timed out waiting for response frame")
}

// ----- discovery -------------------------------------------------------------

// listCandidatePorts returns host serial ports whose names look like USB CDC
// devices (i.e. could be our firmware). The check is name-pattern based so
// the binary stays pure-Go on all platforms; identification happens via the
// protocol probe below.
//
//   macOS:   /dev/cu.usbmodem* (preferred over /dev/tty.usbmodem* — they're
//            the same physical port; cu is the callout/outgoing alias and
//            never blocks on DCD)
//   Linux:   /dev/ttyACM*
//   Windows: COM* (any — too noisy to filter further without enumeration)
func listCandidatePorts() ([]string, error) {
	all, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}
	// First pass: identify the cu.* names that exist so we can drop their
	// tty.* twins on macOS.
	cuPresent := map[string]bool{}
	for _, p := range all {
		if strings.HasPrefix(p, "/dev/cu.") {
			cuPresent["/dev/tty."+strings.TrimPrefix(p, "/dev/cu.")] = true
		}
	}
	var out []string
	for _, p := range all {
		if cuPresent[p] {
			continue // skip /dev/tty.X when /dev/cu.X exists
		}
		lower := strings.ToLower(p)
		switch {
		case strings.Contains(lower, "usbmodem"):
			out = append(out, p)
		case strings.HasPrefix(lower, "/dev/ttyacm"):
			out = append(out, p)
		case strings.HasPrefix(p, "COM"):
			out = append(out, p)
		}
	}
	return out, nil
}

// isControlPort opens a candidate port and sends a GetConfigRequest with a
// short timeout. Returns true if the device replied with a valid response —
// i.e. this is the binary control channel, not the debug stdio channel.
func isControlPort(portName string) bool {
	tx, err := openUSB(portName)
	if err != nil {
		return false
	}
	defer tx.close()
	tx.timeout = 1500 * time.Millisecond // generous so freshly-enumerated ports succeed
	req := &pb.Packet{MessageId: &pb.Packet_GetConfigRequest{GetConfigRequest: &pb.GetConfigRequest{}}}
	resp, err := tx.send(req)
	if err != nil || resp == nil {
		return false
	}
	_, ok := resp.MessageId.(*pb.Packet_GetConfigResponse)
	return ok
}

// discoverControlPort scans USB-CDC-looking serial ports and returns the
// path of the first one that responds to the control protocol.
func discoverControlPort() (string, error) {
	cands, err := listCandidatePorts()
	if err != nil {
		return "", err
	}
	if len(cands) == 0 {
		return "", errors.New("no candidate USB serial ports found")
	}
	for _, name := range cands {
		if isControlPort(name) {
			return name, nil
		}
	}
	return "", errors.New("found USB serial ports but none responded to a control probe (already open elsewhere, or wrong device)")
}

// ----- transport selection ---------------------------------------------------

type commonFlags struct {
	ip   string
	port int
	usb  string
}

func (c *commonFlags) makeTransport() (transport, error) {
	// 1. Explicit USB path.
	if c.usb != "" {
		return openUSB(c.usb)
	}
	// 2. Explicit TCP. Only used when --ip is set — we never silently fall
	// back to TCP, since timing out on a non-existent network address is a
	// poor experience.
	if c.ip != "" {
		return &tcpTransport{addr: net.JoinHostPort(c.ip, strconv.Itoa(c.port))}, nil
	}
	// 3. Auto-discover USB. Fail clearly if no device responds.
	portName, err := discoverControlPort()
	if err != nil {
		return nil, fmt.Errorf("USB auto-discovery failed: %w (pass --usb /dev/... or --ip ADDRESS to be explicit)", err)
	}
	fmt.Fprintf(os.Stderr, "[auto] using USB %s\n", portName)
	return openUSB(portName)
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

func fetchConfig(tx transport) (*pb.GetConfigResponse, error) {
	req := &pb.Packet{MessageId: &pb.Packet_GetConfigRequest{GetConfigRequest: &pb.GetConfigRequest{}}}
	resp, err := tx.send(req)
	if err != nil {
		return nil, err
	}
	got, ok := resp.MessageId.(*pb.Packet_GetConfigResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	return got.GetConfigResponse, nil
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
	defer tx.close()

	cfg, err := fetchConfig(tx)
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
	defer tx.close()

	cur, err := fetchConfig(tx)
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

	req := &pb.Packet{MessageId: &pb.Packet_SaveConfigRequest{
		SaveConfigRequest: &pb.SaveConfigRequest{
			StaticIp:      ipBytes,
			StaticGateway: gwBytes,
			StaticSubnet:  snBytes,
			MdnsHostname:  host,
			MacAddress:    cur.MacAddress,
			SerialNumber:  cur.SerialNumber,
		},
	}}

	resp, err := tx.send(req)
	if err != nil {
		// The device reboots after saving; losing the response is normal.
		fmt.Println("Save sent. Device is rebooting (no response received, which is expected).")
		return nil
	}
	if _, ok := resp.MessageId.(*pb.Packet_SaveConfigResponse); ok {
		fmt.Println("Success! Device is rebooting to apply changes.")
		return nil
	}
	return fmt.Errorf("unexpected response type: %T", resp.MessageId)
}

// ----- discovery subcommand --------------------------------------------------

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	common := &commonFlags{}
	addCommonFlags(fs, common)
	_ = fs.Parse(args)

	fmt.Println("Candidate USB serial ports (firmware uses VID:PID " + deviceVID + ":" + devicePID + "):")
	cands, err := listCandidatePorts()
	if err != nil {
		fmt.Printf("  (enumeration error: %v)\n", err)
	} else if len(cands) == 0 {
		fmt.Println("  (none found)")
	} else {
		for _, name := range cands {
			role := "debug/stdio or unrelated (no control-protocol response)"
			if isControlPort(name) {
				if tx, err := openUSB(name); err == nil {
					if cfg, err := fetchConfig(tx); err == nil {
						role = fmt.Sprintf("CONTROL  ->  IP=%s, host=%s, serial=%s",
							ipString(cfg.StaticIp), cfg.MdnsHostname, cfg.SerialNumber)
					} else {
						role = "CONTROL (probe ok, summary failed: " + err.Error() + ")"
					}
					tx.close()
				}
			}
			fmt.Printf("  %s\n     %s\n", name, role)
		}
	}

	// TCP probe only when the user explicitly asked for one (--ip set).
	if common.ip != "" {
		addr := net.JoinHostPort(common.ip, strconv.Itoa(common.port))
		fmt.Printf("\nTCP probe at %s\n", addr)
		tcp := &tcpTransport{addr: addr}
		if cfg, err := fetchConfig(tcp); err != nil {
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
	defer tx.close()

	req := &pb.Packet{MessageId: &pb.Packet_GetStatusRequest{GetStatusRequest: &pb.GetStatusRequest{}}}
	resp, err := tx.send(req)
	if err != nil {
		return err
	}
	got, ok := resp.MessageId.(*pb.Packet_GetStatusResponse)
	if !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
	}
	s := got.GetStatusResponse

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
	defer tx.close()

	pkt := &pb.Packet{MessageId: &pb.Packet_SetFrontendAttenuationRequest{
		SetFrontendAttenuationRequest: &pb.SetAttenuationRequest{AttenuationDb: int32(db)},
	}}
	resp, err := tx.send(pkt)
	if err != nil {
		return err
	}
	if _, ok := resp.MessageId.(*pb.Packet_SetFrontendAttenuationResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
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
	defer tx.close()

	pkt := &pb.Packet{MessageId: &pb.Packet_SetCalAttenuationRequest{
		SetCalAttenuationRequest: &pb.SetAttenuationRequest{AttenuationDb: int32(db)},
	}}
	resp, err := tx.send(pkt)
	if err != nil {
		return err
	}
	if _, ok := resp.MessageId.(*pb.Packet_SetCalAttenuationResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
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
	defer tx.close()

	pkt := &pb.Packet{MessageId: &pb.Packet_SetChannelsEnabledRequest{
		SetChannelsEnabledRequest: &pb.SetChannelsEnabledRequest{Enabled: on},
	}}
	resp, err := tx.send(pkt)
	if err != nil {
		return err
	}
	if _, ok := resp.MessageId.(*pb.Packet_SetChannelsEnabledResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
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
	defer tx.close()

	pkt := &pb.Packet{MessageId: &pb.Packet_SetCalEnabledRequest{
		SetCalEnabledRequest: &pb.SetCalibrationEnabledRequest{Enabled: on},
	}}
	resp, err := tx.send(pkt)
	if err != nil {
		return err
	}
	if _, ok := resp.MessageId.(*pb.Packet_SetCalEnabledResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
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
	defer tx.close()

	pkt := &pb.Packet{MessageId: &pb.Packet_SetCalSourceRequest{
		SetCalSourceRequest: &pb.SetCalSourceRequest{Internal: internal},
	}}
	resp, err := tx.send(pkt)
	if err != nil {
		return err
	}
	if _, ok := resp.MessageId.(*pb.Packet_SetCalSourceResponse); !ok {
		return fmt.Errorf("unexpected response type: %T", resp.MessageId)
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
	defer tx.close()

	cur, err := fetchConfig(tx)
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

	req := &pb.Packet{MessageId: &pb.Packet_SaveConfigRequest{
		SaveConfigRequest: &pb.SaveConfigRequest{
			StaticIp:      ipBytes,
			StaticGateway: gwBytes,
			StaticSubnet:  snBytes,
			MdnsHostname:  data.Hostname,
			MacAddress:    cur.MacAddress,
			SerialNumber:  cur.SerialNumber,
		},
	}}

	resp, err := tx.send(req)
	if err != nil {
		fmt.Println("Save sent. Device is rebooting (no response received, which is expected).")
		return nil
	}
	if _, ok := resp.MessageId.(*pb.Packet_SaveConfigResponse); ok {
		fmt.Println("Success! Device is rebooting.")
		return nil
	}
	return fmt.Errorf("unexpected response type: %T", resp.MessageId)
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
