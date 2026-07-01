# Whalepod library example

Demonstrates using the [`client`](../../client) package directly (instead
of the `rf-control` CLI) to drive a Whalepod board. It centers on the
[`client.Whalepod`](../../client/whalepod.go) settings object — the
"change the values, then Write" pattern:

```go
wp := client.NewWhalepod(tx)
wp.Read()                    // load current channels/attenuation/cal state
wp.CalSourceInternal = true
wp.CalEnabled = true
wp.CalAttenuation = 0
wp.Write()                   // push it all to the device
```

`Write()` sends every field, so `Read()` first (or set every field) to avoid
clobbering settings you meant to leave alone. `Whalepod` embeds `*Client`, so
`GetConfig`, `GetStatus`, and `Close` are available on it too.

The program:

1. Reads and prints the device serial / firmware.
2. `Read()`s the current state, then enters calibration mode with the
   internal noise source (the noise-source amp only turns on when cal mode
   and the internal source are both selected).
3. Sweeps the calibration attenuator, changing the field and `Write()`-ing
   each step — pause here to take a reading (VNA, spectrum analyzer, power
   meter, etc.; the example just prints where you'd do that).
4. Leaves calibration mode so the board returns to its normal through path.

## Run it

```bash
go run . --usb /dev/ttyACM1     # Linux; use /dev/cu.usbmodem101 on macOS
go run . --ip 192.168.1.50      # or over the network
go run .                        # or let it auto-discover the USB device
```

See [`main.go`](main.go) for the full source, and the parent
[README](../../README.md#using-rf-control-as-a-go-library) for the
`client` package overview.
