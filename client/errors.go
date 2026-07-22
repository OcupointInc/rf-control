package client

import (
	"errors"
	"regexp"
	"strconv"

	pb "github.com/OcupointInc/rf-control/controlpb"
)

// Readback-verified control-GPIO writes (firmware >= 1.1.0)
// -------------------------------------------------------------------------
//
// Older firmware acked a Set* command as soon as it updated the RP2040 SIO
// output latch — an ack meant "I ran gpio_put", not "the pad actually moved".
// A stuck pad (an ESD-damaged output driver, a shorted trace, a glitch that
// flips the pin direction) would silently swallow the write and the host had no
// way to know the command hadn't physically taken effect.
//
// Firmware >= 1.1.0 verifies every control-GPIO write: it drives the pin, waits
// for it to settle, then reads the pad INPUT buffer back three times and
// requires all three reads to equal the commanded level. On mismatch it re-inits
// the pin and retries (up to three attempts total). If the pad still never
// reaches the commanded level, the Set* command replies with an ErrorResponse of
// code ERROR_CODE_HARDWARE_ERROR and detail "gpio<N> readback mismatch" naming
// the offending pin, instead of the normal empty ack.
//
// The practical guarantee: on firmware >= 1.1.0 a clean ack from a Set* command
// now MEANS the MCU pad physically reached the commanded level. See
// IsHardwareError and GPIOFaultPin to detect and diagnose the failure, and the
// caveat on GPIOFaultPin about what a passing ack does and does not prove.

// gpioFaultRe matches the firmware's per-pin readback-mismatch detail string,
// produced by send_hardware_error() as snprintf(..., "gpio%d readback mismatch",
// pin). It deliberately does NOT match the pin-less fallback ("gpio readback
// mismatch") the firmware emits when no specific pin was recorded, so
// GPIOFaultPin only reports a pin it can actually name. The whole string must
// match (anchored) so an unrelated HARDWARE_ERROR detail can't be misparsed.
var gpioFaultRe = regexp.MustCompile(`^gpio([0-9]+) readback mismatch$`)

// IsHardwareError reports whether err is (or wraps, via errors.As) a
// *DeviceError with Code ERROR_CODE_HARDWARE_ERROR — the firmware's signal that
// the underlying hardware operation failed. The dominant cause on firmware
// >= 1.1.0 is a control-GPIO write that failed readback verification (a pad that
// never reached the commanded level after retries); see the package note above
// and GPIOFaultPin to recover which pin.
//
// It returns false for transport failures, other DeviceError codes
// (UNSUPPORTED, DECODE_FAILED, INVALID_REQUEST), and nil.
func IsHardwareError(err error) bool {
	var de *DeviceError
	if !errors.As(err, &de) {
		return false
	}
	return de.Code == pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR
}

// GPIOFaultPin extracts the offending GPIO pin number from a readback-mismatch
// hardware fault. It returns (pin, true) only when err is (or wraps) a
// *DeviceError with Code ERROR_CODE_HARDWARE_ERROR whose Detail matches the
// firmware's exact "gpio<N> readback mismatch" shape (see the package note
// above); otherwise it returns (0, false).
//
// The Detail is a human-readable string, so the match is intentionally strict:
// a HARDWARE_ERROR with any other detail — including the firmware's pin-less
// "gpio readback mismatch" fallback — yields (0, false), as does any non-
// hardware error. Use it to attribute a fault to a specific pad, e.g. to map the
// pin to the RF stage it drives:
//
//	if err := c.SetRfSwitchChannel(3); err != nil {
//		if pin, ok := client.GPIOFaultPin(err); ok {
//			log.Printf("control pin gpio%d is stuck", pin)
//		}
//		return err
//	}
//
// Caveat: readback proves the MCU pad physically reached the commanded level —
// nothing downstream of the pin. A clean ack (no error) with still-dead RF means
// the fault lies past the pad (the switch/attenuator IC, its supply, or the RF
// path), not in the GPIO drive the firmware can see.
func GPIOFaultPin(err error) (pin int, ok bool) {
	var de *DeviceError
	if !errors.As(err, &de) {
		return 0, false
	}
	if de.Code != pb.ErrorCode_ERROR_CODE_HARDWARE_ERROR {
		return 0, false
	}
	m := gpioFaultRe.FindStringSubmatch(de.Detail)
	if m == nil {
		return 0, false
	}
	n, convErr := strconv.Atoi(m[1])
	if convErr != nil {
		return 0, false // unreachable: regexp guarantees a decimal run
	}
	return n, true
}
