# Whalepod library example

Demonstrates using the [`client`](../../client) package directly (instead
of the `rf-control` CLI) to drive a Whalepod board through a calibration
measurement:

1. Read and print the device's network config.
2. Select the internal noise source and enter calibration mode — the
   noise-source amp only turns on when both are true.
3. Sweep the calibration attenuator, pausing at each step so you can take a
   reading (a VNA, spectrum analyzer, power meter, etc. — this example just
   prints where you'd do that).
4. Leave calibration mode so the board returns to its normal through path.

## Run it

```bash
go run . --usb /dev/ttyACM1     # Linux; use /dev/cu.usbmodem101 on macOS
go run . --ip 192.168.1.50      # or over the network
go run .                        # or let it auto-discover the USB device
```

See [`main.go`](main.go) for the full source, and the parent
[README](../../README.md#using-rf-control-as-a-go-library) for the
`client` package overview.
