# Ocupoint RF Control for Python

Native Python access to the same firmware protocol as the `rf-control` Go library
and GUI. It talks directly to a device over Ethernet or USB; no GUI, Go binary,
ATP server, or background service is required. Python 3.10 or newer is supported.

## Install

From the `rf-control` repository:

```sh
python -m pip install ./python            # Ethernet and firmware-update support
python -m pip install './python[usb]'     # also USB CDC and USB discovery
```

For a wheel supplied with a release, use `python -m pip install /path/to/ocupoint_rf_control-0.1.0-py3-none-any.whl`.
The package name is `ocupoint-rf-control`; the import is `rf_control`.

## Customer CW and sweep control

Connect over Ethernet using your device's IP address (replace `192.168.1.50`
below with its actual address):

```python
from rf_control import Client

# external_clock=False: internal reference; True: external 10 MHz reference.
with Client.tcp("192.168.1.50") as device:
    device.cw(500, power_dbm=-31.25, external_clock=False)
    input("CW applied. Press Enter to switch to sweep...")

    device.sweep(50, 1500, duration_s=0.01, power_dbm=-28,
                 external_clock=False)
    input("Sweep applied. Press Enter to exit (RF stays on)...")
```

CW frequencies and sweep endpoints are IF MHz, restricted to 50–1500 MHz.
The customer plan uses a 9600 MHz LMX LO and an ADF frequency of LO + IF.
The `sweep()` duration is in **seconds per sweep**: `0.01` means 10 ms,
`10` means 10 seconds. Sweeps repeat until reconfigured. Durations must resolve
to whole microseconds, from 0.000001 to 4294.967295 seconds.

`power_dbm` accepts **−56.75 to −25 dBm**, in 0.25 dB steps; it defaults to
−25 dBm. Power maps to the internal DSA attenuation automatically.
`external_clock=True` selects the external 10 MHz reference; the default is internal. The nominal output estimate is −25 dBm minus
attenuation, using LMX power code 50; it is not a live power measurement.

With `force=False`, the helpers use the same optional lock verification as Go: set **0 dB attenuation before
retuning**, allow an external reference and both synthesizers up to two seconds
each to settle, and apply requested attenuation only after lock and LO/power
readback verification. A lock failure raises `DeviceError` and leaves attenuation
at **0 dB**, without switching to maximum attenuation. There is no repeated tune
or repeated clock-selection command during lock polling. `lock_timeout` overrides
the acquisition interval; an in-flight status request is additionally bounded by
the transport timeout. Neither helper promises that a failed request rolled back
other hardware settings. The returned `BarracudaConfiguration` describes the
applied plan; call `get_status()` for subsequent live readback.

The Python helpers default to **`force=True`**, so the example needs no extra
argument. In this mode,
the library selects the requested reference, programs the LO and CW/sweep, and
applies the requested power **without waiting for or requiring lock**. It skips
external-reference lock polling and synthesizer lock/frequency/power readback
verification. It never automatically applies maximum attenuation because of an
unlocked indication. `signal_locked` is `None` in the result because lock was
not checked; nominal power is still an estimate, not a measurement.

Forced tuning retains input validation, Barracuda identification, reference-source
selection readback, and device/transport error reporting. It cannot override a
firmware refusal or a failed connection. Set `force=False` to restore bounded
lock verification. `external_clock=False` selects internal reference;
`external_clock=True` selects external 10 MHz reference in either mode.

The earlier `configure_barracuda_cw()` and `configure_barracuda_sweep()` methods
remain available with attenuation in dB and sweep duration in microseconds.
These methods also default to `force=True`; pass `force=False` to retain
lock verification in existing scripts.

## Interactive customer example

The [CW and sweep example](examples/barracuda.py) is a single Ethernet script
with the IP address and settings directly in the calls. Edit those values,
then run it from your editor or,
from the `rf-control` repository:

```sh
python python/examples/barracuda.py
```

No command-line settings are needed. The script starts CW, waits for **Enter**
to start a repeating sweep, then waits for **Enter** to exit. Closing the
connection leaves the current RF output running. A configuration error stops
the script and displays the error; it does not automatically mute or retry.

The separate [sweep command-line example](examples/sweep.py) remains available
for automation, but the interactive example is the customer starting point.

Use the low-level methods below to control an engineering plan independently.
They never automatically change attenuation in response to a lock result.

## USB and discovery

```python
import ipaddress
from rf_control import Client, discover_ethernet, discover_usb_ports

for found in discover_ethernet(timeout=1.0):
    print(str(ipaddress.IPv4Address(found.ip)), found.serial, found.control_port)

ports = discover_usb_ports()  # explicitly sends read-only GetConfig probes
for port in ports:
    with Client.usb(port) as device:
        print(port, device.get_config().serial_number)

# An explicitly selected binary control interface also works:
# with Client.usb("COM5") as device: ...
# with Client.usb("/dev/ttyACM1") as device: ...
```

USB requires the second CDC interface (binary control), not the debug console.
DTR/RTS are asserted when opened. Discovery filters by VID/PID when available,
then probes the protocol. `list_candidate_ports()` enumerates without probing;
`is_control_port(path)` probes one path. Ethernet discovery accepts a
`broadcast="192.168.1.255"` override for a subnet-directed broadcast.

## Complete control API

All low-level operations are explicit, keyword-only methods on `Client` and
return the corresponding generated protobuf response. `pb` exports every
message, enum, status field, capability flag, and diagnostic from
[`control.proto`](../control.proto). `help(Client.set_chirp)` and Python IDEs
show parameter names/defaults. Advanced options use the protocol's units and
full-state semantics: omitted booleans are false, numeric values zero, repeated
fields empty. Supply every state you want retained when a command replaces a
complete register configuration. Firmware rejects unsupported boards or invalid
combinations with `DeviceError`.

| Controls | Methods |
| --- | --- |
| Identity and live diagnostics | `get_config()`, `get_status()`, `gpio_self_test()`, `lmx2595_register_read(addresses=(0, 44, 110))` |
| Frontend/calibration attenuation | `set_attenuation(db)`, `set_frontend_attenuation(attenuation_db=10)`, `set_cal_attenuation(attenuation_db=10)` |
| Channel/calibration enables | `set_channels_enabled(enabled=True)`, `set_cal_enabled(enabled=True)`, `set_cal_source(internal=True)` |
| STRAPS RF routing | `set_switches(rf_switch=..., mixer_switch=..., if_switch=...)`, `set_rf_band(band=...)` |
| SP8T routing | `set_rf_switch_channel(channel=1)`; channel 0 isolates all ports |
| PLL and LO | `set_pll_frequency(frequency_mhz=10100)`, `set_lo_frequency(frequency_mhz=9600)`; LO 0 powers down |
| Barracuda attenuation | `set_dsa_attenuation(quarter_db=25)`, `set_dsa_attenuation_db(6.25)` |
| Clock source | `set_clock_source(external=False)`; **True means external**, unlike the Go `SetClockSource(internal)` argument |
| Clock outputs | `set_lmk_clock_outputs(lmx_enabled=True, adf_enabled=True, sma_a_enabled=False, sma_b_enabled=False)` |
| Reference rate / LO drive | `set_lmk_reference_frequency(requested_frequency_hz=20_000_000)`, `set_lmx_output_power(power_code=50)` |
| Chirp and extended waveform | `set_chirp(start_freq_mhz=..., deviation_mhz=..., ramp_time_us=..., mode=..., enabled=True, ...)` |
| FSK / phase | `set_fsk(center_freq_mhz=..., deviation_khz=..., enabled=True)`, `set_phase(mode=..., phase_millidegrees=...)` |
| ADF reference path | `set_adf_ref_config(r_counter=..., ref_doubler=..., ref_div2=..., prescaler_8_9=..., cp_current_code=...)` |
| ADF loop | `set_adf_loop_config(csr=..., negative_bleed=..., negative_bleed_code=..., lol_disable=..., integer_n_mode=...)` |
| ADF power | `set_adf_power(power_down=..., cp_three_state=..., counter_reset=...)` |
| Equalized sweep | `run_equalized_sweep(start_frequency_khz=..., stop_frequency_khz=..., sweep_time_us=..., points=..., lo_frequency_mhz=..., restore_entry_state=...)` |
| EEPROM | `eeprom_read(address=0, length=32)`, `eeprom_write(address=0, data=b"...")`, `get_serial()`, `set_serial("SN2")` |
| Network persistence | `configure_network(ip=..., gateway=..., subnet=..., hostname=...)`, or `save_config(...)` |
| Firmware update | `update_firmware(image, progress=...)`; raw `fw_update_begin`, `fw_update_data`, `fw_update_commit`, `fw_update_abort` are also exposed |

Every protocol operation is also available through
`device.request("set_phase", mode=pb.PHASE_MODE_STATIC, phase_millidegrees=90000)`.
This validates the response type and exposes firmware error codes exactly like
the named methods. `discovery_request` is normally used via UDP discovery;
the raw firmware-update methods require an update transport (Ethernet port
5002), so use `update_firmware()` for automatic transport selection.

Extended chirps expose `parabolic`, `dual_ramp`, `ramp2_deviation_mhz`,
`ramp2_time_us`, `fast_ramp`, `fast_ramp_down_time_us`, `fsk_on_ramp_khz`,
`delayed_start`, `ramp_delay`, `delay_us`, `triangular_delay`,
`txdata_trigger_delay`, `external_step_clock`, `txdata_invert`, and
`muxout_ramp_complete`. When MUXOUT carries ramp completion, `locked` and
`pll_locked` are the last lock sample taken before rerouting, not live lock.
GPIO self-test momentarily toggles control pins and can disrupt an active signal.

```python
from rf_control import Client, pb

with Client.tcp("192.168.1.50") as device:
    response = device.set_chirp(
        start_freq_mhz=10_000, deviation_mhz=100, ramp_time_us=5000,
        mode=pb.CHIRP_MODE_TRIANGLE_TRIGGERED, enabled=True,
        fast_ramp=True, fast_ramp_down_time_us=1000)
    print(response.locked)
    print(device.get_status().barracuda.capabilities)
```

## Firmware and persistent network configuration

```python
from rf_control import Client, FirmwareImage

image = FirmwareImage.load("barracuda-app.bin")  # OTA .bin, not factory UF2
with Client.tcp("192.168.1.50") as device:
    device.update_firmware(image, progress=lambda done, total: print(done, total))
```

The updater checks the embedded image identity, size and CRC, uses the dedicated
framed Ethernet update service (port 5002) or the existing USB connection,
verifies transfer acknowledgements and commit CRC, and attempts abort if an
update fails. A successful commit reboots the device. `port=` overrides the
Ethernet update port. The firmware remains responsible for rejecting a board
identity mismatch before erasing.

`configure_network()` reads current identity first and preserves serial, MAC,
hostname (unless overridden), and gateway-check state. It saves the supplied
IPv4 address/gateway/subnet and reboots. If the reply is dropped during reboot,
`TransportError` reports an **unknown outcome**; reconnect to the new address
and verify with `get_config()`. Do not treat that exception as proof of failure
or blindly repeat a flash write.

## Errors, timing and concurrency

- `DeviceError.code` / `.detail`: firmware received but rejected the request,
  including unsupported commands, hardware faults and customer lock failures.
- `TransportError`: connection, timeout or I/O failure. A command already sent
  may have applied. No sent command is automatically replayed (particularly
  static phase increments, equalized sweeps and flash writes).
- `ProtocolError`: malformed or unexpected reply, invalid EEPROM record or
  inconsistent firmware update acknowledgement.
- `ValueError`: invalid local arguments, before the command is sent.

TCP retries connection establishment up to three times for the device's
small listener pool. TCP responses are assembled using the protobuf oneof
message length, so fragmented replies are handled. Control TCP uses a fresh
connection per request; USB uses `AA 55` + little-endian 16-bit length frames.
Both use bounded I/O deadlines. `Client.tcp(..., timeout=5, connect_attempts=3)`
and `Client.usb(..., timeout=2)` configure these limits.

A client is synchronous and should be used by one thread at a time. Use a lock
around shared access, including the entire CW/sweep/update call. Async programs
can invoke methods through `asyncio.to_thread` while serializing access.

## Development and verification

```sh
python -m pip install './python[dev]'
python -m unittest discover -s python/tests -v
python -m build ./python
python python/tools/generate_protocol.py
```

The bindings are checked in; users do not need `protoc`. Regeneration uses
`grpcio-tools==1.62.3` against the repository's `control.proto`; the generator
uses a private descriptor pool so this package can coexist with older ATP
gateway bindings in the same process. Test coverage includes every protocol
method, fragmented loopback TCP, USB framing with a fake serial port, discovery,
lock acquisition/failure, clock-reference settling, EEPROM/network behavior,
and firmware transfer/error recovery. No test contacts RF equipment.
