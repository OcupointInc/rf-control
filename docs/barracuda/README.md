# Barracuda first bring-up and customer control

This guide verifies a Barracuda unit over USB-C first, then configures it for
control over Ethernet. Normal customer operation consists of CW and continuous
sweep. All frequencies entered and reported by `rf-control` are the 50–1500 MHz
IF; the software handles the internal frequency conversion automatically.

## What you need

- Barracuda and its 12 V power supply
- A computer with the `rf-control` executable
- USB-C and Ethernet cables
- A 50 ohm RF measurement instrument and SMA cable
- A 10 MHz reference source and cable for the external-clock test

## 1. Connect the hardware

1. Connect the Ethernet cable between Barracuda and the lab network or control
   computer.
2. Connect the RF SMA cable from either RF Port A or RF Port B to the
   measurement instrument.
3. Connect the USB-C cable between Barracuda and the control computer. USB-C is
   the control connection for the initial tests.
4. Connect the 12 V power supply and allow the unit to boot.

Set the measurement instrument for the expected frequency range before
enabling RF. At 0 dB attenuation, nominal output after the unit reaches its
normal operating temperature is approximately -25 dBm.

## 2. Confirm the USB-C connection

Run:

```bash
rf-control list
```

The `USB devices` section should show the Barracuda control port. The examples
below use `/dev/ttyACM1` on Linux. Substitute the path printed by `list` (for
example, `/dev/cu.usbmodem101` on macOS or `COM5` on Windows).

Read the device configuration and record its current IP address:

```bash
rf-control --usb /dev/ttyACM1 get
```

If exactly one compatible USB device is connected, `rf-control` can usually
discover it automatically. Using `--usb DEVICE` during first bring-up makes it
clear which unit is being tested.

## 3. Verify a CW tone over USB-C

Generate a 400 MHz IF CW tone with the internal clock and 0 dB attenuation:

```bash
rf-control --usb /dev/ttyACM1 cw \
  --frequency 400 --attenuation 0 --clock internal
```

Confirm all of the following:

- The command reports `Signal locked: true`.
- The measurement instrument shows a CW tone at 400 MHz IF.
- Once the unit is at its normal operating temperature, measured power is
  approximately -25 dBm at 0 dB attenuation. Some variation with frequency,
  unit calibration, cabling, and measurement uncertainty is expected.

Check the live state if needed:

```bash
rf-control --usb /dev/ttyACM1 status
```

## 4. Verify a sweep over USB-C

Run a continuous 50–1500 MHz IF sweep with a 10-second sweep time:

```bash
rf-control --usb /dev/ttyACM1 sweep \
  --start 50 --stop 1500 --time 10s \
  --attenuation 0 --clock internal
```

Confirm that the command reports `Signal locked: true` and that the measurement
instrument shows the requested sweep. Sweep time must include a unit, such as
`10s`, `20ms`, or `35us`.

## 5. Verify the external 10 MHz reference

1. Connect and enable the external 10 MHz reference source.
2. Configure a CW tone using that reference:

   ```bash
   rf-control --usb /dev/ttyACM1 cw \
     --frequency 400 --attenuation 0 --clock external
   ```

3. Read back the state:

   ```bash
   rf-control --usb /dev/ttyACM1 status
   ```

The software output must show:

```text
Clock               : external
Reference locked    : true
Signal locked       : true
```

The external-clock command will not unmute RF unless the 10 MHz reference is
valid, selected, and locked. If the check fails, correct the reference source
or cabling and rerun the complete command. To return to the onboard clock, run
the CW or sweep command with `--clock internal`.

## 6. View or update the network address

Keep USB-C connected while checking or changing the network settings.

To display the current IP address, gateway, subnet, hostname, and device
identity:

```bash
rf-control --usb /dev/ttyACM1 get
```

To change the unit to `192.168.50.25`:

```bash
rf-control --usb /dev/ttyACM1 set-ip 192.168.50.25
```

The customer `set-ip` command automatically derives gateway `192.168.50.1`
and subnet `255.255.255.0`, while preserving the hostname, MAC address, and
serial number. Choose an unused address from `.2` through `.254` on the desired
`/24` network; `.1` is reserved for the derived gateway. The unit reboots after
applying the change.

After the reboot, make sure the control computer can reach the same network,
then discover the unit:

```bash
rf-control list
```

The new address should appear under `Ethernet devices`. Confirm network control
without relying on USB-C:

```bash
rf-control --ip 192.168.50.25 status
rf-control --ip 192.168.50.25 cw \
  --frequency 400 --attenuation 0 --clock internal
```

The unit is now ready for normal control over Ethernet. Substitute its assigned
address in subsequent `--ip` commands.

## Normal operation

CW frequency and sweep start/stop are always specified as 50–1500 MHz IF:

```bash
# CW using the default internal clock
rf-control --ip 192.168.50.25 cw \
  --frequency 400 --attenuation 6.25

# Continuous sweep
rf-control --ip 192.168.50.25 sweep \
  --start 50 --stop 1500 --time 10s --attenuation 0
```

Attenuation accepts 0–31.75 dB in 0.25 dB steps. Each dB of attenuation lowers
the nominal output by one dB from the approximately -25 dBm baseline:

| Attenuation | Nominal output |
|---:|---:|
| 0 dB | -25 dBm |
| 3 dB | -28 dBm |
| 6.25 dB | -31.25 dBm |
| 10 dB | -35 dBm |
| 20 dB | -45 dBm |
| 31.75 dB | -56.75 dBm |

## If a command fails

- Run `rf-control list` and confirm the selected USB device or IP address.
- Run `status` using the same `--usb` or `--ip` connection.
- For an external-clock error, confirm the source is enabled and supplying a
  10 MHz reference, then rerun the complete CW or sweep command.
- A failed RF setup leaves the output at maximum attenuation for safety.
  Correct the reported issue and run the complete command again.
