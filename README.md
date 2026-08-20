# rf-control

Single-binary CLI for configuring Ocupoint Ethernet-controlled RF
frontends (Barracuda, Black Canyon, Straps, Whalepod). Talks to the device over
either TCP (default port 5000) or the USB control channel on the
second CDC interface — useful when the network side isn't reachable
yet (fresh board, wrong static IP, no DHCP).

No runtime dependencies. Download the binary for your platform from
the GitHub Releases page and run it. Prefer to drive a device from your
own Go program instead? See
[Using rf-control as a Go library](#using-rf-control-as-a-go-library).

---

## Download

1. Open the **[current tested build](https://github.com/OcupointInc/rf-control/releases/tag/latest)**
   page. This rolling prerelease is rebuilt from `main`; customer shipments
   should use a numbered release made from the same tested commit.
2. Under **Assets**, click the file matching your platform:

   | Platform               | File                            |
   | ---------------------- | ------------------------------- |
   | Linux x86_64           | `rf-control-linux-amd64`        |
   | Linux ARM64 (e.g. Pi)  | `rf-control-linux-arm64`        |
   | macOS Intel            | `rf-control-darwin-amd64`       |
   | macOS Apple Silicon    | `rf-control-darwin-arm64`       |
   | Windows x86_64         | `rf-control-windows-amd64.exe`  |
   | Windows ARM64          | `rf-control-windows-arm64.exe`  |

   The `.uf2` firmware images for each eval board are tracked in
   [`firmware/`](firmware/) — see
   [docs/firmware/README.md](docs/firmware/README.md) for the reflash
   procedure.

3. Make it runnable.

   **Linux / macOS** — mark it executable and (optionally) drop it on
   your `PATH`:

   ```bash
   chmod +x rf-control-*
   sudo mv rf-control-* /usr/local/bin/rf-control
   ```

   **macOS Gatekeeper** may block an unsigned binary the first time
   you run it. Right-click the file in Finder → **Open** → confirm,
   or run `xattr -d com.apple.quarantine rf-control-*` from a
   terminal.

   **Windows** — rename the download to `rf-control.exe` and run it
   from PowerShell or `cmd`. SmartScreen may warn the first time;
   click **More info → Run anyway**.

4. Sanity check:

   ```bash
   rf-control help
   ```

---

## Quick start

### Find the device

```bash
rf-control list
```

This discovers Barracuda units over Ethernet and enumerates USB-CDC serial
ports that look like the firmware (VID:PID `2E8A:000A`). The printed address or
device path is what you pass to `--ip` or `--usb`.

### Read current configuration

```bash
rf-control --usb /dev/ttyACM1 get          # Linux
rf-control --usb /dev/cu.usbmodem101 get   # macOS
rf-control --usb COM5 get                  # Windows
rf-control --ip 192.168.1.50 get            # over the network
```

With no `--usb` or `--ip`, USB is auto-discovered.

### Set the customer network address

```bash
rf-control --usb /dev/ttyACM1 set-ip 192.168.50.25
```

The customer shorthand automatically uses gateway `192.168.50.1` and subnet
`255.255.255.0`. In general, `set-ip A.B.C.D` derives gateway `A.B.C.1` and a
`/24` subnet. MAC, hostname, and serial number are preserved.

### Engineering: use a nonstandard network

```bash
rf-control --usb /dev/ttyACM1 set-ip \
    --address 192.168.1.50 --gateway 192.168.1.1 \
    --subnet 255.255.255.0 --hostname my-rf-box
```

### Apply a JSON config file

```bash
rf-control --usb /dev/ttyACM1 apply-json example_config.json
```

The schema:

```json
{
  "static_ip":      "192.168.1.50",
  "static_gateway": "192.168.1.1",
  "static_subnet":  "255.255.255.0",
  "hostname":       "my-rf-box"
}
```

### Barracuda customer control

For normal use there are only two RF commands. Customer frequencies are always
entered and reported as 50–1500 MHz IF. The software handles conversion and
synthesizer settings internally, safely mutes the output while reconfiguring,
and then applies the requested attenuation.

CW at 400 MHz IF using the internal clock:

```bash
rf-control --ip 192.168.1.253 cw --frequency 400
```

Continuous 50–1500 MHz IF sweep over 10 seconds using an external 10 MHz
reference:

```bash
rf-control --ip 192.168.1.253 sweep \
  --start 50 --stop 1500 --time 10s --clock external
```

Set attenuation with `--attenuation`; values are 0–31.75 dB in 0.25 dB steps:

```bash
rf-control --ip 192.168.1.253 cw \
  --frequency 900 --attenuation 6.25 --clock internal
```

The calibrated nominal output is −25 dBm at 0 dB attenuation. Each customer
command restores the calibrated power baseline before enabling RF.
Each dB of attenuation then lowers the nominal level by one dB, so 6.25 dB
requests approximately −31.25 dBm. The command prints the requested nominal
level.

`--clock internal` is the default. With `--clock external`, RF remains muted and
the command fails if the external reference is not valid, selected, and locked.
Use `rf-control --ip 192.168.1.253 status` to inspect the current clock,
IF/waveform mode, attenuation, power estimate, lock state, and controller
temperature. Internal conversion details are available only through
`rf-control engineering-status`.

The lower-level `set-*`, diagnostic, and firmware-update commands remain
available for engineering and service work through
`rf-control engineering-help`; customers do not need them for CW or sweep
operation. The normal `rf-control help` output shows only the customer surface.
See the one-page [Barracuda customer guide](docs/barracuda/README.md) for the
handoff instructions.

---

## All commands

```
cw --frequency <MHz> [--attenuation <dB>] [--clock internal|external]
                       Customer CW at 50-1500 MHz IF
sweep --start <MHz> --stop <MHz> --time <duration>
      [--attenuation <dB>] [--clock internal|external]
                       Customer continuous sweep at 50-1500 MHz IF
list                   Discover Ethernet and USB devices
get                    Print the current device configuration
set-ip ADDRESS         Set IP; derive a /24 subnet and gateway x.x.x.1
set-ip [flags]         Engineering network overrides
apply-json <file>      Apply a JSON network config file
apply [file]           Apply a full-device JSON config from stdin (or a file)
                       and print a JSON result — the command to call from Python
status                 Print live RF status
engineering-status     Include Barracuda conversion/synthesizer status
set-att <dB>           Set frontend attenuation
set-cal-att <dB>       Set calibration attenuation
set-channels <on|off>  Enable or disable the RF channels
set-cal <on|off>       Enter/leave calibration mode (CAL_SW)
set-cal-source <internal|external>
                       Select the Whalepod calibration source (CAL_SEL)
set-clock <internal|external>
                       Select the board's internal or external reference
set-pll <MHz>          Tune the STRAPS LMX2595 LO
set-band <band>        Apply a STRAPS band preset (switches + LO)
set-switch-channel <1-8|off>
                       Route the SP8T RF-switch board's common port
gpio-selftest          Run the control-GPIO diagnostic (alias: selftest);
                       exits 5 if any pin is stuck

Barracuda RF module (other boards answer "unsupported", exit code 4):
set-lo <MHz>           Tune the LMX2595 LO (0 powers it down)
set-dsa <dB>           HMC1119 attenuation, 0-31.75 dB in 0.25 dB steps
set-chirp [flags]      Program/arm the ADF4159 FMCW ramp
set-fsk [flags]        ADF4159 FSK around a center frequency
set-phase <off|psk|static> [degrees]
                       ADF4159 output phase / PSK
set-adf-ref [flags]    ADF4159 reference path (R counter, doubler, CP current)
set-adf-loop [flags]   ADF4159 loop quality (CSR, bleed, integer-N) — full state
set-adf-power [flags]  ADF4159 power-down / CP three-state / counter reset — full state
image-info <image.bin> Inspect a Barracuda OTA application image
flash [flags] <image.bin>
                       Flash via Ethernet or USB and verify after reboot
```

### Engineering: Barracuda ADF4159 commands

```bash
rf-control set-chirp --start 10000 --dev 1000 --time 35 --mode sawtooth
rf-control set-chirp --off                    # hold CW at --start
rf-control set-fsk --center 10000 --dev-khz 500
rf-control set-phase psk 90                   # +/-90 deg toggled from TXDATA
rf-control set-adf-ref --r-counter 1 --cp-code 7 --prescaler 8/9
rf-control set-adf-loop --negative-bleed --bleed-code 4
rf-control set-adf-power                      # no flags = back to normal running
```

`set-chirp` also takes the extended ADF4159 waveform options, all defaulting to
off so omitting them reproduces the classic chirp exactly: `--parabolic`,
`--dual-ramp` (with `--ramp2-deviation` / `--ramp2-time`), `--fast-ramp` (with
`--fast-down-time`), `--fsk-khz`, `--delayed-start`, `--ramp-delay`,
`--delay-us`, `--triangular-delay`, `--trigger-delay`, `--external-step-clock`,
`--txdata-invert`, and `--ramp-complete-out`. With `--ramp-complete-out` the
MUXOUT pin carries the ramp-complete pulse instead of digital lock detect, so
the `locked=` value it prints is the state sampled *before* the reroute, not a
live reading.

`set-adf-ref`, `set-adf-loop`, and `set-adf-power` are **full-state**: every
call programs all of their fields, so a flag you leave out is applied as its
default rather than left as it was. Build the whole command each time instead of
issuing incremental tweaks. For `set-adf-loop` and `set-adf-power` those
defaults are all-off, which makes a bare `set-adf-power` the way back to normal
running; for `set-adf-ref` they are the Barracuda baseline (`--r-counter 1`,
`--cp-code 7`, `--prescaler 8/9`, no doubler, no ÷2).

Changing the reference path rescales everything derived from f_PFD, so program a
frequency first: the firmware revalidates the armed waveform at the new f_PFD
and refuses a change that would push the output past the RF ceiling.

These flags also accept the shorter spellings the firmware's own `control_tool`
uses — `--r`, `--cp`, `--doubler`, `--div2`, `--bleed`, `--cp-tristate` — so the
same command line works against either CLI.

The device is the real validator for all of these. When it refuses, it answers
with an `INVALID_REQUEST` error whose detail names the constraint (for example
`parabolic excludes dual/fast ramp`), which the CLI prints before exiting 4.

## Exit codes

Every command exits with a code describing *why* it failed, so a script can tell
an input mistake apart from a device or connection problem without scraping the
message text. Bad input is also rejected up front — for `apply`, before the
transport is even opened — so a typo never half-configures the hardware.

```
0  success
1  unexpected internal error
2  invalid input (unknown command, bad flag, or bad argument value)
3  could not reach the device (connection refused, timeout, USB gone)
4  the device received the request but rejected it (firmware ErrorCode printed)
5  a diagnostic ran but found a hardware fault (gpio-selftest: a stuck pin)
```

Code 5 is distinct on purpose: the command reached the device and it answered
successfully — the *result* of the diagnostic is a fault, not a rejected request
(code 4) or an unreachable device (code 3). Only `gpio-selftest` uses it today.

Code 4 carries the firmware's machine-readable reason. When importing the Go
`client` package directly, that reason is a typed `*client.DeviceError` (with a
`Code` field) and an unreachable device is a `*client.TransportError` — use
`errors.As` to branch on them.

## Hardware faults: readback-verified GPIO writes

Firmware **≥ 1.1.0** verifies every control-GPIO write. Older firmware acked a
`set-*` command the instant it updated the MCU's output latch — an ack meant "I
ran the write", not "the pad actually moved". Now the firmware drives the pin,
lets it settle, reads the pad back three times, and requires all three reads to
match the commanded level; on a mismatch it re-inits the pin and retries (three
attempts total). If a pad still never reaches the commanded level — a stuck
output driver, a shorted trace, a glitch that flipped the pin direction — the
command is answered with `ERROR_CODE_HARDWARE_ERROR` naming the offending pin
instead of the normal ack.

So on firmware ≥ 1.1.0, **a clean ack now means the MCU pad physically reached
the commanded level.**

At the CLI a stuck pin fails the command, prints the device's reason, and exits
`4`:

```console
$ rf-control --ip 192.168.1.50 set-rf-switch 3
Error: the device rejected the request: device error: gpio12 readback mismatch (ERROR_CODE_HARDWARE_ERROR)
$ echo $?
4
```

At the library level the same fault is a typed `*client.DeviceError`. Two
helpers make it first-class — `client.IsHardwareError` and
`client.GPIOFaultPin`:

```go
if err := c.SetRfSwitchChannel(3); err != nil {
    if pin, ok := client.GPIOFaultPin(err); ok {
        // A control pad is stuck. pin is the GPIO number the firmware named.
        log.Fatalf("control pin gpio%d failed readback — the write did not take effect", pin)
    }
    if client.IsHardwareError(err) {
        // Hardware fault without a specific pin (e.g. an SPI/I2C failure).
        log.Fatalf("device hardware error: %v", err)
    }
    log.Fatal(err) // transport error, or the device refused the request for another reason
}
```

`GPIOFaultPin` only matches the firmware's exact `gpio<N> readback mismatch`
detail, so it returns `(0, false)` for any other hardware error;
`IsHardwareError` covers `ERROR_CODE_HARDWARE_ERROR` in general.

**Caveat:** readback proves the *MCU pad* moved — nothing downstream of it. A
passing ack while the RF is still dead means the fault lies past the pin (the
switch/attenuator IC, its supply, or the RF path itself), not in the GPIO drive
the firmware can see.

## GPIO self-test (`gpio-selftest`)

Where the readback check above catches a stuck pad *when you happen to write to
it*, `gpio-selftest` sweeps the whole control-GPIO bank on demand. The firmware
drives every board control pin **low, then high**, reading the pad input buffer
back each way, and reports per-pin pass/fail with the stuck direction. It also
**sweeps the pad drive strength** (2 → 4 → 8 → 12 mA) and reports the *lowest*
drive at which each pin toggled cleanly — an early-warning signal, described
below. Firmware **≥ 1.1.0**. Alias: `selftest`.

```console
$ rf-control --ip 192.168.1.50 gpio-selftest --pins
GPIO self-test — 8 pins
  GPIO  0  PWR_EN      PASS  (needs 4 mA)
  GPIO  2  SCK         PASS  (needs 4 mA)
  GPIO  3  MOSI        PASS  (needs 4 mA)
  GPIO  7  CS_VHF      PASS  (needs 4 mA)
  GPIO  8  CS_UHF      PASS  (needs 4 mA)
  GPIO 12  CAL_SW      FAIL  (stuck LOW)
           ↳ Calibration switch is stuck in CALIBRATION mode — the unit can't return to the normal RF signal path, so live signals never reach the output.
  GPIO 15  CAL_AMP_EN  PASS  (needs 4 mA)
  GPIO  9  CLOCK_EN    PASS  (needs 4 mA)
Result: FAIL — 1 of 8 pin(s) stuck
$ echo $?
5
```

(The pins above are the whalepod_automation control set; other boards sweep
their own.) A passing pin prints `PASS`; a stuck one prints `FAIL  (stuck LOW)`
or `FAIL  (stuck HIGH)` — `LOW` means the firmware could not drive it high (held
low: shorted to ground / a dead driver), `HIGH` means it could not drive it low
(held high: shorted to a rail). When every pin passes the last line is
`Result: PASS — all N pin(s) ok` and the command exits `0`; any stuck pin exits
`5` (see [Exit codes](#exit-codes)).

### Drive-strength reporting (early-warning)

Each `PASS` also carries the **lowest pad drive** the pin needed to toggle
cleanly — `PASS  (needs 4 mA)` above. Most pins are configured at a 4 mA
default; the switch/select lines (`CAL_SW`, `CAL_SEL`, `RF_SW`, `MIXER_SW`,
`IF_SW`, `SW_V1`–`SW_V4`, `SW_LS`) run at 12 mA. A pin that only passes **above
its default** is *marginal*: the firmware can still drive it at runtime, but the
extra drive it took usually means flux residue, a hairline short, or leakage on
that net — a board that's starting to degrade. `min_drive_ma = 0` is not a drive
value; it means the pin failed even at 12 mA (a hard fault, shown as `FAIL`).

In the raw `--pins` table every `PASS` shows its drive. The default grouped
subsystem view stays clean when the board is healthy and only calls out drive
when it's above default: the subsystem is flagged `PASS (marginal: needs N mA)`,
the offending pin is named on a continuation line, and a one-line advisory
follows the `Result` line.

```console
$ rf-control --ip 192.168.1.50 gpio-selftest
GPIO self-test — whalepod_automation: PASS
  Power enable                  PASS
  Attenuator control (VHF/UHF)  PASS (marginal: needs 8 mA)
      ↳ CS_VHF (GPIO 7) passed only at 8 mA (default 4 mA)
  Calibration enable            PASS
  Calibration source select     PASS
  Clock enable                  PASS
Result: PASS
Note: 1 pin needed elevated drive (possible flux/leakage — consider cleaning/rework).
```

A marginal pin still **passes** — the command exits `0`. Treat the note as a cue
to inspect/clean/rework that net before it becomes a hard fault, not as a failure.

On Whalepod-family boards each stuck pin also gets an indented `↳` line
translating the fault into its plain-English consequence — what the operator
actually loses (as with `CAL_SW` above). These consequence messages are
**whalepod-specific**; on other boards the table prints the pass/fail rows
without them. The same text is available programmatically from
`client.GpioFaultDescription(board, pinName, stuck)`.

**This is disruptive.** It momentarily toggles power-enable and select lines
(CAL_SW, chip-selects, `PWR_EN`, …), so don't run it mid-measurement. The
firmware snapshots and restores each pin as it goes and re-asserts the latched
attenuator state at the end, so the RF is left at its boot defaults rather than
whatever you had configured — re-apply your settings afterward.

**Caveat (same as above):** the self-test proves each *MCU pad* can move in both
directions. It says nothing about anything downstream — a broken trace, a failed
switch/attenuator IC, or a dead RF path will still read `PASS` here because the
pad itself toggles fine. Use it to rule the MCU GPIO in or out, not to bless the
whole signal chain.

From Go, the same data is available structurally — `Client.GpioSelfTest()`
returns a `*controlpb.GpioSelfTestResponse` (`AllPassed` plus a `Pins` slice of
`{Pin, Name, Passed, Stuck, MinDriveMa}`, where `MinDriveMa` is the lowest drive
in mA the pin passed at, or `0` if it failed); `AllPassed == false` is a normal
result to inspect, not an error.

## Transport selection

Place these before or after the command — both orderings work.

```
--usb DEVICE   Use that USB serial device (e.g. /dev/cu.usbmodem101)
--ip ADDRESS   Use TCP at that IPv4 address
--port PORT    TCP port (default 5000)
(neither)      Auto-discover USB. Never silently falls back to TCP —
               pass --ip explicitly for that.
-v, --verbose  Hex-dump frames sent and received, log timings
```

---

## Wire format

USB frames on CDC1 use a 4-byte header followed by the protobuf
payload, in both directions:

```
| 0xAA | 0x55 | len_lo | len_hi | <protobuf bytes> |
```

`len` is little-endian. TCP transports use the raw protobuf with no
frame header (the W5500 TCP socket boundary is the message boundary).

---

## Driving it from Python (JSON over stdin/stdout)

If you just want the binary and don't want to write Go, the `apply` command is
the machine interface. Feed it **one JSON document** describing any subset of
device state; it opens the transport once, applies every field present (in a
safe order), closes the transport, and prints a JSON result to stdout. Only
`stdout` is JSON — diagnostics (auto-discovery notes, `-v` hex dumps) go to
stderr, so parsing stdout is always clean.

The Barracuda customer interface is available as one nested object. CW:

```json
{
  "barracuda": {
    "mode": "cw",
    "if_frequency_mhz": 400,
    "attenuation_db": 0,
    "clock": "internal"
  }
}
```

Continuous sweep:

```json
{
  "barracuda": {
    "mode": "sweep",
    "start_if_mhz": 50,
    "stop_if_mhz": 1500,
    "sweep_time": "10s",
    "attenuation_db": 0,
    "clock": "external"
  }
}
```

Pass either document to `rf-control --ip ADDRESS apply`. The same IF range,
quarter-dB, clock-lock, internal-conversion, and safe-muting rules as `cw` and
`sweep` apply. A `network` block may accompany `barracuda`; legacy RF fields
may not.

The older shared-board fields remain available for Whalepod, STRAPS, and RF
switch automation:

```bash
echo '{
  "attenuation_db": 10,
  "channels_enabled": true,
  "cal_enabled": true,
  "cal_source_internal": true
}' | rf-control --usb /dev/ttyACM1 apply
```

```jsonc
// stdout:
{
  "ok": true,
  "applied": ["channels_enabled=true", "attenuation_db=10", "cal_source_internal=true", "cal_enabled=true"],
  "status": { "board_type": "whalepod", "channels_enabled": true, "attenuation_db": 10, ... }
}
```

From Python — build the config, hand the process a transport flag, read one
object back:

```python
import json, subprocess

def apply(config, *, usb=None, ip=None):
    transport = ["--usb", usb] if usb else ["--ip", ip]
    p = subprocess.run(
        ["rf-control", *transport, "apply"],
        input=json.dumps(config), capture_output=True, text=True,
    )
    result = json.loads(p.stdout)      # {"ok": ..., "applied": [...], "status": {...}}
    if not result["ok"]:
        raise RuntimeError(f'{result["failed_at"]}: {result["error"]}')
    return result

apply({"attenuation_db": 10, "channels_enabled": True,
       "cal_enabled": True, "cal_source_internal": True}, usb="/dev/ttyACM1")
```

`p.returncode` is non-zero on failure too, so you can branch on either that or
`result["ok"]`.

### Config fields

All fields are optional; only the ones you include are touched.

| Field                 | Type          | Notes                                                          |
| --------------------- | ------------- | -------------------------------------------------------------- |
| `attenuation_db`      | int 0–255     | Frontend attenuator                                            |
| `cal_attenuation_db`  | int 0–255     | Calibration-path attenuator                                    |
| `channels_enabled`    | bool          | Enable/disable all RF channels                                 |
| `cal_enabled`         | bool          | Enter/leave calibration mode (CAL_SW)                          |
| `cal_source_internal` | bool          | Whalepod CAL_SEL: `true` = internal noise source, `false` = ext |
| `clock_source_internal` | bool        | Legacy switchable clock: `true` = internal, `false` = external |
| `rf_band`             | string or int | STRAPS band preset (sets switches + LO): a span like `"1800-2700"`, an `RF_BAND_*` name, or int 0–4 |
| `rf_switch`           | string or int | `"4ghz"`/`"2ghz"`, a canonical enum name, or the raw int       |
| `mixer_switch`        | string or int | `"mixer"`/`"bypass"`                                            |
| `if_switch`           | string or int | `"900mhz"`/`"1_2ghz"`                                           |
| `pll_frequency_mhz`   | int 0–15000   | STRAPS LMX2595 LO frequency in MHz                             |
| `rf_switch_channel`   | int 0–8       | SP8T RF-switch board; 0 = all off                              |
| `barracuda`           | object        | Simplified `cw` or `sweep` customer plan described above       |
| `network`             | object        | `static_ip`, `static_gateway`, `static_subnet`, `hostname`     |

Notes:

- Fields are applied in a fixed, dependency-safe order regardless of key order
  in the JSON: band preset → switches → PLL → attenuators → channels → cal
  source → cal enable → network. `rf_band` runs first (it presets switches and
  the LO together) so an explicit `rf_switch`/`if_switch`/`pll_frequency_mhz` in
  the same document overrides just that part of the preset.
  `cal_source_internal` is set before `cal_enabled` so the Whalepod
  noise-source amp gating is correct.
- A `network` block reboots the device (it's a flash write), so it's applied
  last and the result reports `"rebooted": true` with no `status` read-back.
  Omitted `network` sub-fields are preserved from the device's current config.
- An unknown JSON key is an error, not a silent no-op — a typo like
  `attenuaton_db` fails loudly rather than leaving the hardware half-configured.
- On failure the result is `{"ok": false, "error": "...", "failed_at": "<field>",
  "applied": [...]}`; everything listed in `applied` already took effect.

## Using rf-control as a Go library

This CLI is a thin wrapper around a `client` package that you can import
directly from your own Go program — a test harness or automated test bench,
for example — instead of shelling out to the `rf-control` binary.

```bash
go get github.com/OcupointInc/rf-control/client
```

```go
import "github.com/OcupointInc/rf-control/client"

tx := client.NewTCPTransport("192.168.1.50", 5000) // or client.NewUSBTransport("/dev/ttyACM1")
c := client.New(tx)
defer c.Close()

cfg, err := c.GetConfig()
if err != nil {
    log.Fatal(err)
}
fmt.Println(cfg.SerialNumber)

result, err := c.ConfigureBarracudaCW(client.BarracudaCWConfig{
    IFFrequencyMHz: 400,
    AttenuationDB: 0,
    ExternalClock: false,
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("nominal output: %.2f dBm\n", result.NominalOutputDBm)
```

`ConfigureBarracudaCW` and `ConfigureBarracudaSweep` are the supported
customer-facing Go API. They enforce the 50–1500 MHz IF operating range, derive
the conversion plan internally, mute during reconfiguration, validate the
selected clock, and apply the output attenuation. Lower-level methods remain
available for engineering tools.

`Client` also has one method per request the firmware supports today —
`GetConfig`, `SaveConfig`, `GetStatus`, `SetAttenuation`, `SetCalAttenuation`,
`SetChannelsEnabled`, `SetCalEnabled`, `SetCalSource`, `SetClockSource`,
`SetSwitches`, `SetPllFrequency`, `SetRfBand`, `SetRfSwitchChannel`, and for
the Barracuda module `SetLoFrequency`, `SetDsaAttenuation`, `SetChirp` /
`SetChirpEx`, `SetFsk`, `SetPhase`, `SetAdfRefConfig`, `SetAdfLoopConfig`,
`SetAdfPower`, `SetLMXOutputPower`, `SetLMKClockOutputs`,
`SetLMKReferenceFrequency`, `ReadLMX2595Registers`, and `RunEqualizedSweep` — each
returning the typed protobuf response (or nothing but an error, for the
setters) from
`github.com/OcupointInc/rf-control/controlpb`. USB discovery helpers
(`client.ListCandidatePorts`, `client.IsControlPort`,
`client.DiscoverUSBPort`) and `client.DiscoverEthernet` are exported too, so
you can replicate the CLI's discovery behavior in your own code.

`SetChirp` keeps the classic five-argument signature (start, deviation, ramp
time, mode, enabled) and leaves every extended ADF4159 waveform option at zero.
Use `SetChirpEx(client.ChirpConfig{...})` when you need the parabolic, dual/fast
ramp, delay, or TXDATA options. The ADF4159 reference, loop, and power requests
take config structs (`client.AdfRefConfig`, `client.AdfLoopConfig`,
`client.AdfPowerConfig`); the last two are full-state, so a zero-valued field
turns that feature off on the device rather than leaving it untouched.

For the "configure several things and apply" workflow there's a
`client.Whalepod` settings object: set the fields, then `Write()` them all at
once. Call `Read()` first to load the device's current state so you only
change what you mean to.

```go
wp := client.NewWhalepod(tx)
defer wp.Close()

wp.Read()                    // load current channels/attenuation/cal state
wp.CalSourceInternal = true
wp.CalEnabled = true
wp.CalAttenuation = 10
if err := wp.Write(); err != nil {
    log.Fatal(err)
}
```

`TCPTransport` retries connection-level errors internally (see its doc
comment — the device's control port only has two listening sockets, so
back-to-back fresh connections can occasionally race the accept path). You
don't need to add your own retry loop on top.

The package also exposes `LoadFirmwareImage`, `UpdateFirmwareTCP`, and
`UpdateFirmwareUSB` for validated Barracuda OTA updates.

See [`client/client.go`](client/client.go),
[`client/barracuda_customer.go`](client/barracuda_customer.go), and
[`client/whalepod.go`](client/whalepod.go) for the full API (`go doc
github.com/OcupointInc/rf-control/client` once fetched) and
[`examples/whalepod`](examples/whalepod) for a complete, runnable program
that walks through a calibration measurement on a Whalepod board.

---

## Hardware setup guides

- [Barracuda customer control](docs/barracuda/README.md)
- [Whalepod eval board](docs/whalepod/README.md)
- [Reflashing the eval board firmware](docs/firmware/README.md) —
  shared procedure across all devices; `.uf2` files live in
  [`firmware/`](firmware/).

---

## Build from source

You don't need to — releases are prebuilt — but if you want to:

```bash
go build -o rf-control .
```

Cross-compile (pure Go, no cgo required):

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o rf-control.exe .
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o rf-control-mac .
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -o rf-control-pi .
```

Releases are produced by `.github/workflows/release.yml`. Every push to `main`
refreshes the rolling `latest` prerelease after tests pass; `v*` tags publish a
numbered release with binaries for every supported platform.

---

## License

MIT — see [LICENSE](LICENSE).
