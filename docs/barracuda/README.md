# Barracuda customer control

`rf-control` provides two normal operations: CW and continuous sweep. Customer
frequencies are always entered and reported as 50–1500 MHz IF. The software
safely mutes the output while changing settings.

## 1. Find the unit

Connect Barracuda to the same network as the control computer, then run:

```bash
rf-control list
```

Use the Barracuda IP address printed under `Ethernet devices` in the commands
below. USB can be used instead with `--usb DEVICE`.

To change the unit to `192.168.50.25` over USB:

```bash
rf-control --usb /dev/ttyACM1 set-ip 192.168.50.25
```

This customer command automatically sets gateway `192.168.50.1` and subnet
`255.255.255.0`. The unit reboots after applying the network change.

## 2. Generate a CW tone

```bash
rf-control --ip 192.168.1.253 cw --frequency 400
```

The frequency is IF in MHz and must be 50–1500 MHz.

## 3. Generate a continuous sweep

```bash
rf-control --ip 192.168.1.253 sweep \
  --start 50 --stop 1500 --time 10s
```

Start and stop are IF in MHz. Sweep time must include a unit, such as `10s`,
`20ms`, or `35us`. Conversion and synthesizer settings are handled internally.

## Output level

At 0 dB attenuation the calibrated nominal output is −25 dBm. Each command
restores that calibrated power baseline automatically. Add
`--attenuation` to either command to reduce it in 0.25 dB steps:

```bash
rf-control --ip 192.168.1.253 cw \
  --frequency 400 --attenuation 6.25
```

| Attenuation | Nominal output |
|---:|---:|
| 0 dB | −25 dBm |
| 3 dB | −28 dBm |
| 6.25 dB | −31.25 dBm |
| 10 dB | −35 dBm |
| 20 dB | −45 dBm |
| 31.75 dB | −56.75 dBm |

These are nominal levels based on the calibrated −25 dBm baseline; actual
level can vary with frequency and unit calibration.

## Clock source

The internal clock is used by default. To use the external 10 MHz reference:

```bash
rf-control --ip 192.168.1.253 cw \
  --frequency 400 --clock external
```

The command will not unmute RF unless the external reference is valid,
selected, and locked. Use `--clock internal` to return to the onboard clock.

## Check status

```bash
rf-control --ip 192.168.1.253 status
```

Status shows the selected clock, CW/sweep mode, IF frequency, attenuation,
nominal output estimate, lock state, and controller temperature.

## If a command fails

- Confirm the IP with `rf-control list`.
- For external-clock errors, connect and enable the 10 MHz reference or retry
  with `--clock internal`.
- A failed setup leaves the output at maximum attenuation for safety. Correct
  the reported issue and run the complete command again.
