package client

import (
	pb "github.com/OcupointInc/rf-control/controlpb"
)

// GpioFaultDescription returns a plain-English, customer-readable consequence of
// a stuck control pin, or "" when there is no board-specific description to give.
//
// It is deliberately gated to the Whalepod family (board == "whalepod" or
// "whalepod_automation"): the pin names and their functional meaning are
// board-specific, so on any other board — or for an unknown pin name, or a stuck
// value that names no determinate direction — it returns "" and the caller
// simply prints the pin's pass/fail line without a consequence.
//
// stuck is the direction the pin failed toward, mirroring the firmware's enum:
// GPIO_STUCK_STATE_LOW  means the firmware could NOT drive the pin high (it is
// held low), and GPIO_STUCK_STATE_HIGH means it could NOT drive the pin low (it
// is held high). The returned text is written in those terms.
//
// This is a presentation helper for gpio-selftest output; it adds no new wire
// data — only the pin name and stuck direction the firmware already reported (see
// GpioSelfTestResponse / GpioPinResult) drive it.
func GpioFaultDescription(board, pinName string, stuck pb.GpioStuckState) string {
	if board != "whalepod" && board != "whalepod_automation" {
		return ""
	}

	var low bool
	switch stuck {
	case pb.GpioStuckState_GPIO_STUCK_STATE_LOW:
		low = true
	case pb.GpioStuckState_GPIO_STUCK_STATE_HIGH:
		low = false
	default:
		return "" // no determinate direction to describe
	}

	// word is the stuck direction as it appears in the SCK/MOSI sentences.
	word := "HIGH"
	if low {
		word = "LOW"
	}

	switch pinName {
	case "PWR_EN", "FE_EN":
		if low {
			return "Board/front-end power-enable is stuck OFF — the RF front-end can't be powered up, so no signal passes and the channels can't be turned on."
		}
		return "Board/front-end power-enable is stuck ON — the front-end can't be powered down; the channels-off command has no effect."
	case "SCK":
		return "Attenuator serial-clock (SCK) line is stuck " + word + " — no attenuator can be reprogrammed, so all attenuation control is lost (both VHF and UHF front-end attenuators are frozen at their last setting)."
	case "MOSI":
		return "Attenuator serial-data (MOSI) line is stuck " + word + " — attenuator programming can't be sent, so all attenuation control is lost (VHF and UHF frozen)."
	case "CS_VHF":
		if low {
			return "VHF attenuator chip-select is stuck LOW (permanently latched) — the VHF front-end attenuator can't accept new values, so VHF attenuation is frozen. UHF is unaffected."
		}
		return "VHF attenuator chip-select is stuck HIGH (permanently transparent) — the VHF attenuator won't hold its setting and can be corrupted by bus traffic. UHF is unaffected."
	case "CS_UHF":
		if low {
			return "UHF attenuator chip-select is stuck LOW (permanently latched) — the UHF front-end attenuator can't accept new values, so UHF attenuation is frozen. VHF is unaffected."
		}
		return "UHF attenuator chip-select is stuck HIGH (permanently transparent) — the UHF attenuator won't hold its setting and can be corrupted by bus traffic. VHF is unaffected."
	case "CAL_SW":
		if low {
			return "Calibration switch is stuck in CALIBRATION mode — the unit can't return to the normal RF signal path, so live signals never reach the output."
		}
		return "Calibration switch is stuck in the RF THROUGH path — the unit can't enter calibration mode, so it can't be calibrated."
	case "CAL_AMP_EN":
		if low {
			return "Internal calibration noise-source amplifier is stuck OFF — internal-source calibration produces no reference signal (a cal run will read as no signal)."
		}
		return "Internal calibration noise-source amplifier is stuck ON — the noise source is always powered and may inject noise into the signal path during normal operation."
	case "CLOCK_EN":
		if low {
			return "Clock-enable is stuck OFF — the board's clock can't be turned on; anything downstream that needs it won't work."
		}
		return "Clock-enable is stuck ON — the clock can't be gated off; the enable line isn't controllable (usually otherwise harmless)."
	case "CAL_SEL":
		if low {
			return "Calibration source-select is stuck on EXTERNAL — the internal noise source can't be selected for calibration."
		}
		return "Calibration source-select is stuck on INTERNAL — an external calibration input can't be selected."
	case "CAL_ATT":
		if low {
			return "Calibration-path attenuator chip-select is stuck LOW (permanently latched) — the cal-path attenuator can't be updated, so cal-path attenuation is frozen."
		}
		return "Calibration-path attenuator chip-select is stuck HIGH (permanently transparent) — the cal-path attenuator won't hold its setting."
	}
	return ""
}
