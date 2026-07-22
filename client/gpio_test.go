package client

import (
	"errors"
	"testing"

	pb "github.com/OcupointInc/rf-control/controlpb"
)

// GpioSelfTest must send a gpio_self_test_request and return the parsed
// gpio_self_test_response — AllPassed plus the per-pin fields — when the device
// replies with one. (mockTransport / errorReply live in errors_test.go.)
func TestGpioSelfTestParsesResponse(t *testing.T) {
	reply := &pb.Packet{MessageId: &pb.Packet_GpioSelfTestResponse{
		GpioSelfTestResponse: &pb.GpioSelfTestResponse{
			AllPassed: false,
			Pins: []*pb.GpioPinResult{
				{Pin: 0, Name: "PWR_EN", Passed: true, Stuck: pb.GpioStuckState_GPIO_STUCK_STATE_NONE},
				{Pin: 12, Name: "CAL_SW", Passed: false, Stuck: pb.GpioStuckState_GPIO_STUCK_STATE_LOW},
			},
		},
	}}
	tx := &mockTransport{reply: reply}
	c := New(tx)

	resp, err := c.GpioSelfTest()
	if err != nil {
		t.Fatalf("GpioSelfTest() unexpected error: %v", err)
	}

	// The request that went out must be a gpio_self_test_request.
	if _, ok := tx.last.MessageId.(*pb.Packet_GpioSelfTestRequest); !ok {
		t.Fatalf("GpioSelfTest sent %T, want *Packet_GpioSelfTestRequest", tx.last.MessageId)
	}

	if resp.GetAllPassed() {
		t.Error("AllPassed = true, want false")
	}
	pins := resp.GetPins()
	if len(pins) != 2 {
		t.Fatalf("len(Pins) = %d, want 2", len(pins))
	}
	if p := pins[0]; p.GetPin() != 0 || p.GetName() != "PWR_EN" || !p.GetPassed() {
		t.Errorf("pins[0] = %+v, want pin=0 name=PWR_EN passed=true", p)
	}
	if p := pins[1]; p.GetPin() != 12 || p.GetName() != "CAL_SW" || p.GetPassed() ||
		p.GetStuck() != pb.GpioStuckState_GPIO_STUCK_STATE_LOW {
		t.Errorf("pins[1] = %+v, want pin=12 name=CAL_SW passed=false stuck=LOW", p)
	}
}

// GpioFaultDescription must return the right board-specific, direction-aware
// consequence for a whalepod pin, and "" for a non-whalepod board or an unknown
// pin name.
func TestGpioFaultDescription(t *testing.T) {
	const calMode = "Calibration switch is stuck in CALIBRATION mode — the unit can't return to the normal RF signal path, so live signals never reach the output."
	const csVhfTransparent = "VHF attenuator chip-select is stuck HIGH (permanently transparent) — the VHF attenuator won't hold its setting and can be corrupted by bus traffic. UHF is unaffected."

	cases := []struct {
		name  string
		board string
		pin   string
		stuck pb.GpioStuckState
		want  string
	}{
		{"whalepod_automation CAL_SW low", "whalepod_automation", "CAL_SW", pb.GpioStuckState_GPIO_STUCK_STATE_LOW, calMode},
		{"whalepod CS_VHF high", "whalepod", "CS_VHF", pb.GpioStuckState_GPIO_STUCK_STATE_HIGH, csVhfTransparent},
		{"non-whalepod board", "straps", "CAL_SW", pb.GpioStuckState_GPIO_STUCK_STATE_LOW, ""},
		{"unknown pin", "whalepod_automation", "NOT_A_PIN", pb.GpioStuckState_GPIO_STUCK_STATE_LOW, ""},
	}
	for _, c := range cases {
		if got := GpioFaultDescription(c.board, c.pin, c.stuck); got != c.want {
			t.Errorf("%s: GpioFaultDescription(%q, %q, %v) =\n %q\nwant\n %q", c.name, c.board, c.pin, c.stuck, got, c.want)
		}
	}
}

// A firmware refusal (ErrorResponse) must surface from GpioSelfTest as a typed
// *DeviceError, the same as every other client method — see Client.send.
func TestGpioSelfTestReturnsDeviceError(t *testing.T) {
	tx := &mockTransport{reply: errorReply(pb.ErrorCode_ERROR_CODE_UNSUPPORTED, "")}
	c := New(tx)

	_, err := c.GpioSelfTest()
	var de *DeviceError
	if !errors.As(err, &de) {
		t.Fatalf("GpioSelfTest() error is %T, want *DeviceError", err)
	}
	if de.Code != pb.ErrorCode_ERROR_CODE_UNSUPPORTED {
		t.Errorf("DeviceError.Code = %v, want UNSUPPORTED", de.Code)
	}
}
