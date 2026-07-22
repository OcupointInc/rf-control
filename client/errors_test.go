package client

import (
	"errors"
	"fmt"
	"testing"

	pb "github.com/OcupointInc/rf-control/controlpb"
)

// hwErr builds a *DeviceError with the hardware-error code and the given detail.
func hwErr(detail string) *DeviceError {
	return &DeviceError{Code: pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR, Detail: detail}
}

func TestIsHardwareError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain error", errors.New("boom"), false},
		{"hardware error, pin detail", hwErr("gpio12 readback mismatch"), true},
		{"hardware error, generic detail", hwErr("spi bus timeout"), true},
		{"hardware error, empty detail", hwErr(""), true},
		{"hardware error, pin-less fallback", hwErr("gpio readback mismatch"), true},
		{"unsupported", &DeviceError{Code: pb.ErrorCode_ERROR_CODE_UNSUPPORTED, Detail: "gpio12 readback mismatch"}, false},
		{"invalid request", &DeviceError{Code: pb.ErrorCode_ERROR_CODE_INVALID_REQUEST}, false},
		{"decode failed", &DeviceError{Code: pb.ErrorCode_ERROR_CODE_DECODE_FAILED}, false},
		{"transport error", &TransportError{Err: errors.New("dial refused")}, false},
		{"wrapped hardware error", fmt.Errorf("set-att: %w", hwErr("gpio7 readback mismatch")), true},
	}
	for _, c := range cases {
		if got := IsHardwareError(c.err); got != c.want {
			t.Errorf("%s: IsHardwareError() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestGPIOFaultPin(t *testing.T) {
	cases := []struct {
		name    string
		err     error
		wantPin int
		wantOK  bool
	}{
		{"pin 12", hwErr("gpio12 readback mismatch"), 12, true},
		{"pin 0", hwErr("gpio0 readback mismatch"), 0, true},
		{"pin 27", hwErr("gpio27 readback mismatch"), 27, true},
		{"wrapped", fmt.Errorf("set-clock: %w", hwErr("gpio9 readback mismatch")), 9, true},
		// Right code, but the detail is not the exact per-pin shape.
		{"pin-less fallback", hwErr("gpio readback mismatch"), 0, false},
		{"other hardware detail", hwErr("spi write failed"), 0, false},
		{"empty detail", hwErr(""), 0, false},
		{"missing space", hwErr("gpio12readback mismatch"), 0, false},
		{"negative pin", hwErr("gpio-1 readback mismatch"), 0, false},
		{"trailing text", hwErr("gpio12 readback mismatch now"), 0, false},
		{"leading text", hwErr("warning: gpio12 readback mismatch"), 0, false},
		// Detail matches the shape, but the code is not HARDWARE_ERROR.
		{"wrong code", &DeviceError{Code: pb.ErrorCode_ERROR_CODE_UNSUPPORTED, Detail: "gpio12 readback mismatch"}, 0, false},
		{"non-device error", errors.New("gpio12 readback mismatch"), 0, false},
		{"nil", nil, 0, false},
	}
	for _, c := range cases {
		pin, ok := GPIOFaultPin(c.err)
		if ok != c.wantOK || pin != c.wantPin {
			t.Errorf("%s: GPIOFaultPin() = (%d, %v), want (%d, %v)", c.name, pin, ok, c.wantPin, c.wantOK)
		}
	}
}

// mockTransport is a Transport that returns a canned reply and records the last
// packet it was asked to send. It lets client methods be exercised without a
// real device.
type mockTransport struct {
	reply *pb.Packet
	err   error
	last  *pb.Packet
}

func (m *mockTransport) Send(p *pb.Packet) (*pb.Packet, error) {
	m.last = p
	return m.reply, m.err
}

func (m *mockTransport) Close() error { return nil }

// errorReply builds a device reply carrying an ErrorResponse, matching what the
// firmware sends when it refuses a request.
func errorReply(code pb.ErrorCode, detail string) *pb.Packet {
	return &pb.Packet{MessageId: &pb.Packet_ErrorResponse{
		ErrorResponse: &pb.ErrorResponse{Code: code, Detail: detail},
	}}
}

// When the transport replies with an error_response, a Set* method must surface
// it as a typed *DeviceError (not a wire-level TransportError or an "unexpected
// response type"), and the GPIO-fault helpers must recognize it.
func TestSetMethodReturnsDeviceError(t *testing.T) {
	tx := &mockTransport{reply: errorReply(pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR, "gpio12 readback mismatch")}
	c := New(tx)

	err := c.SetAttenuation(10)
	if err == nil {
		t.Fatal("SetAttenuation() = nil, want a DeviceError")
	}
	var de *DeviceError
	if !errors.As(err, &de) {
		t.Fatalf("SetAttenuation() error is %T, want *DeviceError", err)
	}
	if de.Code != pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR || de.Detail != "gpio12 readback mismatch" {
		t.Errorf("DeviceError = %+v, want code=HARDWARE_ERROR detail=%q", de, "gpio12 readback mismatch")
	}
	if !IsHardwareError(err) {
		t.Error("IsHardwareError() = false, want true")
	}
	if pin, ok := GPIOFaultPin(err); !ok || pin != 12 {
		t.Errorf("GPIOFaultPin() = (%d, %v), want (12, true)", pin, ok)
	}

	// The CLI relies on this exact rendering (classifyExit prints it verbatim).
	const want = "device error: gpio12 readback mismatch (ERROR_CODE_HARDWARE_ERROR)"
	if got := de.Error(); got != want {
		t.Errorf("DeviceError.Error() = %q, want %q", got, want)
	}
}

// SetClockSource keeps its historical "internal" argument but the wire field is
// SetClockSourceRequest.external (opposite sense); verify the inversion so the
// CLI/library semantics stay unchanged after the proto rename.
func TestSetClockSourceInvertsOntoWire(t *testing.T) {
	cases := []struct {
		internal     bool
		wantExternal bool
	}{
		{internal: true, wantExternal: false},
		{internal: false, wantExternal: true},
	}
	for _, c := range cases {
		tx := &mockTransport{reply: &pb.Packet{MessageId: &pb.Packet_SetClockSourceResponse{
			SetClockSourceResponse: &pb.SetClockSourceResponse{},
		}}}
		client := New(tx)
		if err := client.SetClockSource(c.internal); err != nil {
			t.Fatalf("SetClockSource(%v) unexpected error: %v", c.internal, err)
		}
		req, ok := tx.last.MessageId.(*pb.Packet_SetClockSourceRequest)
		if !ok {
			t.Fatalf("SetClockSource sent %T, want *Packet_SetClockSourceRequest", tx.last.MessageId)
		}
		if got := req.SetClockSourceRequest.GetExternal(); got != c.wantExternal {
			t.Errorf("SetClockSource(internal=%v) sent external=%v, want %v", c.internal, got, c.wantExternal)
		}
	}
}
