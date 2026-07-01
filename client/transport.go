package client

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"
	"google.golang.org/protobuf/proto"

	pb "github.com/OcupointInc/rf-control/controlpb"
)

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
	// is healthy — a short retry clears it up.
	tcpMaxAttempts = 3
	tcpRetryDelay  = 150 * time.Millisecond

	// USB VID/PID of the firmware (see usb_descriptors.c in the firmware
	// repo). Identification of the control port is done by protocol probe
	// (IsControlPort), not by VID/PID alone — this is only used to narrow
	// candidate ports and for display purposes.
	DeviceVID = "2E8A"
	DevicePID = "000A"
)

// Transport is the wire-level abstraction over USB-serial and TCP. Send
// marshals p, writes it, and unmarshals exactly one response packet.
// Implementations are not safe for concurrent use by multiple goroutines.
type Transport interface {
	Send(p *pb.Packet) (*pb.Packet, error)
	Close() error
}

// ----- TCP transport ---------------------------------------------------------

// TCPTransport talks to the device's control port (default 5000) over TCP.
// Each Send dials a fresh connection, writes the request, reads one
// response, and closes the connection — the device's control port has no
// notion of a session, so there is nothing to gain by keeping a socket open
// across requests (and the firmware itself only tracks one framed
// request/response per accepted connection).
type TCPTransport struct {
	addr string

	// Verbose, if set, is called with log lines describing retries. Leave
	// nil to disable.
	Verbose func(format string, args ...any)
}

// NewTCPTransport returns a Transport that connects to host:port over TCP.
func NewTCPTransport(host string, port int) *TCPTransport {
	return &TCPTransport{addr: net.JoinHostPort(host, strconv.Itoa(port))}
}

func (t *TCPTransport) logf(format string, args ...any) {
	if t.Verbose != nil {
		t.Verbose(format, args...)
	}
}

// Send dials, sends one request, and reads one response, retrying on
// connection-level errors (the device refusing or resetting a fresh
// connection under the two-socket accept race described above).
// SaveConfigRequest is exempt from retries: the device intentionally drops
// the response and reboots after a save, so a "failure" there is normal,
// and retrying would just risk re-triggering the flash write.
func (t *TCPTransport) Send(p *pb.Packet) (*pb.Packet, error) {
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
			t.logf("TCP request failed (attempt %d/%d): %v — retrying in %v", attempt, attempts, err, delay)
			time.Sleep(delay)
		}
	}
	if attempts > 1 {
		return nil, fmt.Errorf("after %d attempts: %w", attempts, lastErr)
	}
	return nil, lastErr
}

func (t *TCPTransport) sendOnce(p *pb.Packet) (*pb.Packet, error) {
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

func (t *TCPTransport) Close() error { return nil }

// ----- USB (CDC1) transport --------------------------------------------------

// USBTransport talks to the device's binary control channel on its second
// USB-CDC interface. Frames are `0xAA 0x55 <len_lo> <len_hi> <protobuf>`,
// little-endian length, in both directions.
type USBTransport struct {
	port    serial.Port
	timeout time.Duration

	// Verbose, if set, is called with hex dumps and timing info for every
	// frame sent/received. Leave nil to disable.
	Verbose func(format string, args ...any)
}

// NewUSBTransport opens devPath (e.g. "/dev/ttyACM1", "/dev/cu.usbmodem101",
// "COM5") as the device's binary control channel.
func NewUSBTransport(devPath string) (*USBTransport, error) {
	mode := &serial.Mode{BaudRate: 115200}
	port, err := serial.Open(devPath, mode)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", devPath, err)
	}
	u := &USBTransport{port: port, timeout: usbReadTimeout}
	// The firmware's USB poll gates on tud_cdc_n_connected(), which is true
	// only when the host has asserted DTR. Some platforms / library versions
	// don't assert DTR by default on open, so force it on.
	if err := port.SetDTR(true); err != nil {
		u.logf("SetDTR(true) failed: %v (continuing anyway)", err)
	}
	if err := port.SetRTS(true); err != nil {
		u.logf("SetRTS(true) failed: %v (continuing anyway)", err)
	}
	if err := port.SetReadTimeout(100 * time.Millisecond); err != nil {
		_ = port.Close()
		return nil, err
	}
	u.logf("opened %s, DTR/RTS asserted", devPath)
	return u, nil
}

func (u *USBTransport) logf(format string, args ...any) {
	if u.Verbose != nil {
		u.Verbose(format, args...)
	}
}

func (u *USBTransport) logBytes(label string, b []byte) {
	if u.Verbose != nil {
		u.Verbose("%s (%d bytes):\n%s", label, len(b), hex.Dump(b))
	}
}

// SetTimeout overrides the default 2s read timeout — e.g. IsControlPort uses
// a shorter, more generous timeout while probing freshly-enumerated ports.
func (u *USBTransport) SetTimeout(d time.Duration) { u.timeout = d }

func (u *USBTransport) Send(p *pb.Packet) (*pb.Packet, error) {
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

	u.logBytes("USB tx frame", frame)
	writeStart := time.Now()
	if _, err := u.port.Write(frame); err != nil {
		return nil, err
	}
	u.logf("USB write took %v", time.Since(writeStart))

	readStart := time.Now()
	respBytes, err := u.readFrame()
	u.logf("USB read took %v", time.Since(readStart))
	if err != nil {
		return nil, err
	}
	u.logBytes("USB rx payload", respBytes)
	resp := &pb.Packet{}
	if err := proto.Unmarshal(respBytes, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (u *USBTransport) Close() error { return u.port.Close() }

func (u *USBTransport) readFrame() ([]byte, error) {
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
			if len(rawSeen) > 0 {
				u.logBytes("USB rx (raw, before error)", rawSeen)
			}
			return nil, err
		}
		if n == 0 {
			continue
		}
		b := one[0]
		rawSeen = append(rawSeen, b)

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
				if m > 0 {
					rawSeen = append(rawSeen, payload[read:read+m]...)
				}
				read += m
			}
			if read != int(length) {
				if len(rawSeen) > 0 {
					u.logBytes("USB rx (raw, payload truncated)", rawSeen)
				}
				return nil, errors.New("usb: timed out reading payload")
			}
			return payload, nil
		}
	}
	if len(rawSeen) == 0 {
		u.logf("USB rx: 0 bytes received before deadline (firmware never replied)")
	} else {
		u.logBytes("USB rx (raw, no valid frame)", rawSeen)
	}
	return nil, errors.New("usb: timed out waiting for response frame")
}

// ----- discovery -------------------------------------------------------------

// ListCandidatePorts returns host serial ports whose names look like USB CDC
// devices (i.e. could be our firmware). The check is name-pattern based so
// callers stay pure-Go on all platforms; identification happens via
// IsControlPort.
//
//	macOS:   /dev/cu.usbmodem* (preferred over /dev/tty.usbmodem* — they're
//	         the same physical port; cu is the callout/outgoing alias and
//	         never blocks on DCD)
//	Linux:   /dev/ttyACM*
//	Windows: COM* (any — too noisy to filter further without enumeration)
func ListCandidatePorts() ([]string, error) {
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

// IsControlPort opens a candidate port and sends a GetConfigRequest with a
// short timeout. Returns true if the device replied with a valid response —
// i.e. this is the binary control channel, not the debug stdio channel.
func IsControlPort(portName string) bool {
	tx, err := NewUSBTransport(portName)
	if err != nil {
		return false
	}
	defer tx.Close()
	tx.SetTimeout(1500 * time.Millisecond) // generous so freshly-enumerated ports succeed
	_, err = New(tx).GetConfig()
	return err == nil
}

// DiscoverUSBPort scans USB-CDC-looking serial ports and returns the path of
// the first one that responds to the control protocol.
func DiscoverUSBPort() (string, error) {
	cands, err := ListCandidatePorts()
	if err != nil {
		return "", err
	}
	if len(cands) == 0 {
		return "", errors.New("no candidate USB serial ports found")
	}
	for _, name := range cands {
		if IsControlPort(name) {
			return name, nil
		}
	}
	return "", errors.New("found USB serial ports but none responded to a control probe (already open elsewhere, or wrong device)")
}
