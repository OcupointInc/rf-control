# rf-control

Single-binary CLI for configuring Ocupoint Ethernet-controlled RF
frontends (Black Canyon, Straps, Whalepod). Talks to the device over
either TCP (default port 5000) or the USB control channel on the
second CDC interface — useful when the network side isn't reachable
yet (fresh board, wrong static IP, no DHCP).

No runtime dependencies. Download the binary for your platform from
the GitHub Releases page and run it. Prefer to drive a device from your
own Go program instead? See
[Using rf-control as a Go library](#using-rf-control-as-a-go-library).

---

## Download

1. Open the
   **[latest release](https://github.com/OcupointInc/rf-control/releases/latest)**
   page.
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

This enumerates USB-CDC serial ports that look like the firmware
(VID:PID `2E8A:000A`) and probes each one for a control-protocol
response. The path it prints is what you'd pass to `--usb`. If `--ip`
is supplied, the TCP address is also probed.

### Read current configuration

```bash
rf-control --usb /dev/ttyACM1 get          # Linux
rf-control --usb /dev/cu.usbmodem101 get   # macOS
rf-control --usb COM5 get                  # Windows
rf-control --ip 192.168.1.50 get            # over the network
```

With no `--usb` or `--ip`, USB is auto-discovered.

### Change just the IP (MAC, hostname, serial preserved)

```bash
rf-control --usb /dev/ttyACM1 set-ip --address 192.168.1.50
```

### Change multiple network fields at once

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

### RF control

```bash
rf-control status                 # live RF status (channels, atten, LO, switches)
rf-control set-channels on        # enable/disable all RF channels
rf-control set-att 10             # frontend attenuation in dB
rf-control set-cal-att 30         # calibration-path attenuation in dB
rf-control set-cal on             # enter/leave calibration mode (CAL_SW)
rf-control set-cal-source internal  # whalepod cal source (CAL_SEL): internal|external
rf-control set-pll 3500            # tune the STRAPS LMX2595 LO, in MHz
rf-control set-band 1800-2700     # STRAPS band preset: switches + LO in one shot
```

On the Whalepod the internal noise-source amplifier only turns on when
calibration mode is active *and* the internal source is selected, i.e.
`set-cal on` together with `set-cal-source internal`.

`set-pll` and `set-band` drive the STRAPS frontend's LMX2595 PLL: `set-pll`
tunes the LO directly, while `set-band` applies a band preset that sets all
three switch banks *and* tunes the LO to that band in one firmware call.
`set-band` accepts a frequency span (`10-900`, `900-1800`, `1800-2700`,
`2700-3600`, `3600-4500`), a canonical `RF_BAND_*` name, or an integer 0-4.
Boards without a PLL accept both requests but perform no tuning.

---

## All commands

```
list                   Discover USB devices and probe TCP if --ip is set
get                    Print the current device configuration
set-ip [flags]         Change --address, --gateway, --subnet, --hostname
apply-json <file>      Apply a JSON network config file
apply [file]           Apply a full-device JSON config from stdin (or a file)
                       and print a JSON result — the command to call from Python
status                 Print live RF status
set-att <dB>           Set frontend attenuation
set-cal-att <dB>       Set calibration attenuation
set-channels <on|off>  Enable or disable the RF channels
set-cal <on|off>       Enter/leave calibration mode (CAL_SW)
set-cal-source <internal|external>
                       Select the Whalepod calibration source (CAL_SEL)
set-pll <MHz>          Tune the STRAPS LMX2595 LO
set-band <band>        Apply a STRAPS band preset (switches + LO)
```

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
```

Code 4 carries the firmware's machine-readable reason. When importing the Go
`client` package directly, that reason is a typed `*client.DeviceError` (with a
`Code` field) and an unreachable device is a `*client.TransportError` — use
`errors.As` to branch on them.

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
| `rf_band`             | string or int | STRAPS band preset (sets switches + LO): a span like `"1800-2700"`, an `RF_BAND_*` name, or int 0–4 |
| `rf_switch`           | string or int | `"4ghz"`/`"2ghz"`, a canonical enum name, or the raw int       |
| `mixer_switch`        | string or int | `"mixer"`/`"bypass"`                                            |
| `if_switch`           | string or int | `"900mhz"`/`"1_2ghz"`                                           |
| `pll_frequency_mhz`   | int 0–15000   | STRAPS LMX2595 LO frequency in MHz                             |
| `rf_switch_channel`   | int 0–8       | SP8T RF-switch board; 0 = all off                              |
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

if err := c.SetAttenuation(10); err != nil {
    log.Fatal(err)
}
```

`Client` has one method per request the firmware supports today —
`GetConfig`, `SaveConfig`, `GetStatus`, `SetAttenuation`, `SetCalAttenuation`,
`SetChannelsEnabled`, `SetCalEnabled`, `SetCalSource`, `SetSwitches`,
`SetPllFrequency`, `SetRfBand`, `SetRfSwitchChannel` — each returning the
typed protobuf response (or nothing but an error, for the setters) from
`github.com/OcupointInc/rf-control/controlpb`. USB discovery helpers
(`client.ListCandidatePorts`, `client.IsControlPort`,
`client.DiscoverUSBPort`) are exported too, so you can replicate the CLI's
`list`/auto-discovery behavior in your own code.

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

See [`client/client.go`](client/client.go) and
[`client/whalepod.go`](client/whalepod.go) for the full API (`go doc
github.com/OcupointInc/rf-control/client` once fetched) and
[`examples/whalepod`](examples/whalepod) for a complete, runnable program
that walks through a calibration measurement on a Whalepod board.

---

## Hardware setup guides

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

Releases are produced by `.github/workflows/release.yml`, which fires
on `v*` tag pushes and uploads a binary per platform to the GitHub
Release page.

---

## License

MIT — see [LICENSE](LICENSE).
