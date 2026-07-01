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
```

On the Whalepod the internal noise-source amplifier only turns on when
calibration mode is active *and* the internal source is selected, i.e.
`set-cal on` together with `set-cal-source internal`.

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
set-cal <on|off>       Enter/leave calibration mode (CAL_SW)
set-cal-source <internal|external>
                       Select the Whalepod calibration source (CAL_SEL)
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
`SetChannelsEnabled`, `SetCalEnabled`, `SetCalSource` — each returning the
typed protobuf response (or nothing but an error, for the setters) from
`github.com/OcupointInc/rf-control/controlpb`. USB discovery helpers
(`client.ListCandidatePorts`, `client.IsControlPort`,
`client.DiscoverUSBPort`) are exported too, so you can replicate the CLI's
`list`/auto-discovery behavior in your own code.

`TCPTransport` retries connection-level errors internally (see its doc
comment — the device's control port only has two listening sockets, so
back-to-back fresh connections can occasionally race the accept path). You
don't need to add your own retry loop on top.

See [`client/client.go`](client/client.go) for the full API (`go doc
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
