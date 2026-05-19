# rf-control

Single-binary CLI for configuring Ocupoint Ethernet-controlled RF
frontends (Black Canyon, Straps, Whalepod). Talks to the device over
either TCP (default port 5000) or the USB control channel on the
second CDC interface — useful when the network side isn't reachable
yet (fresh board, wrong static IP, no DHCP).

No runtime dependencies. Download the binary for your platform and
run it.

---

## Install

Grab the binary for your platform from the
[latest release](https://github.com/OcupointInc/rf-control/releases/latest):

| Platform               | File                              |
| ---------------------- | --------------------------------- |
| Linux x86_64           | `control_tool-linux-amd64`        |
| Linux ARM64 (e.g. Pi)  | `control_tool-linux-arm64`        |
| macOS Intel            | `control_tool-darwin-amd64`       |
| macOS Apple Silicon    | `control_tool-darwin-arm64`       |
| Windows x86_64         | `control_tool-windows-amd64.exe`  |
| Windows ARM64          | `control_tool-windows-arm64.exe`  |

On macOS and Linux, mark the file executable after download:

```bash
chmod +x control_tool-*
mv control_tool-* /usr/local/bin/control_tool   # optional
```

On Windows, rename to `control_tool.exe` and run it from PowerShell or
cmd.

---

## Quick start

### Find the device

```bash
control_tool list
```

This enumerates USB-CDC serial ports that look like the firmware
(VID:PID `2E8A:000A`) and probes each one for a control-protocol
response. The path it prints is what you'd pass to `--usb`. If `--ip`
is supplied, the TCP address is also probed.

### Read current configuration

```bash
control_tool --usb /dev/ttyACM1 get          # Linux
control_tool --usb /dev/cu.usbmodem101 get   # macOS
control_tool --usb COM5 get                  # Windows
control_tool --ip 192.168.1.50 get            # over the network
```

With no `--usb` or `--ip`, USB is auto-discovered.

### Change just the IP (MAC, hostname, serial preserved)

```bash
control_tool --usb /dev/ttyACM1 set-ip --address 192.168.1.50
```

### Change multiple network fields at once

```bash
control_tool --usb /dev/ttyACM1 set-ip \
    --address 192.168.1.50 --gateway 192.168.1.1 \
    --subnet 255.255.255.0 --hostname my-rf-box
```

### Apply a JSON config file

```bash
control_tool --usb /dev/ttyACM1 apply-json example_config.json
```

The schema:

```json
{
  "static_ip":            "192.168.1.50",
  "static_gateway":       "192.168.1.1",
  "static_subnet":        "255.255.255.0",
  "hostname":             "my-rf-box",
  "enable_gateway_check": true
}
```

### RF control

```bash
control_tool status                 # live RF status (channels, atten, LO, switches)
control_tool set-channels on        # enable/disable all RF channels
control_tool set-att 10             # frontend attenuation in dB
control_tool set-cal-att 30         # calibration-path attenuation in dB
```

---

## All commands

```
list                   Discover USB devices and probe TCP if --ip is set
get                    Print the current device configuration
set-ip [flags]         Change --address, --gateway, --subnet, --hostname
apply-json <file>      Apply a JSON config file
status                 Print live RF status
set-att <dB>           Set frontend attenuation
set-cal-att <dB>       Set calibration attenuation
set-channels <on|off>  Enable or disable the RF channels
```

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

## Hardware setup guides

- [Whalepod eval board](docs/whalepod/README.md)
- [Reflashing the eval board firmware](docs/firmware/README.md) —
  shared procedure for all devices; binaries in [`firmware/`](firmware/)

---

## Build from source

You don't need to — releases are prebuilt — but if you want to:

```bash
go build -o control_tool .
```

Cross-compile (pure Go, no cgo required):

```bash
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o control_tool.exe .
GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build -o control_tool-mac .
GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build -o control_tool-pi .
```

Releases are produced by `.github/workflows/release.yml`, which fires
on `v*` tag pushes and uploads a binary per platform to the GitHub
Release page.

---

## License

MIT — see [LICENSE](LICENSE).
