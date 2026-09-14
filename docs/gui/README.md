# Ocupoint RF Control guide

The customer instructions are now maintained in the repository's main
[README](../../README.md). It covers downloading the single Windows binary,
USB-C discovery, GUI control, network configuration, and exported JSON profiles
for external command-line control.

Airshark devices (firmware board type `straps`) also expose an **Advanced**
tab. It controls the RF and IF filter banks, mixer/bypass routing, LO frequency,
and both 0–31 dB digital attenuators. The tab runs the firmware GPIO self-test
and flashes validated OTA `.bin` images over USB or Ethernet. STRAPS firmware
1.1.1 adds attenuation SCK/MOSI to the diagnostic; the attenuators themselves
are write-only, so their displayed status is commanded state rather than chip
readback. Factory `.uf2` images remain BOOTSEL recovery images and are not
accepted by the GUI flasher.

Barracuda status reports the ADF4159 and LMX2595 locks separately. Apply uses
force mode by default: it sets 0 dB attenuation before retuning, programs the
reference and synthesizers, and applies requested attenuation without requiring
lock or frequency/power readback verification. Live lock indications do not
block Apply. Input validation and device/transport errors remain enforced; no
GUI force-mode toggle is exposed.

To preview the Barracuda controls without hardware, run the unified GUI binary
as a loopback simulator in one terminal:

```bash
./rf-control-gui-linux-amd64 mock-barracuda
```

Open a second copy normally and connect directly to `127.0.0.1` on port `5000`.
The same command is available from the Windows executable and the executable
inside the macOS application bundle.

Failed Apply attempts refresh
live status and discard the previous successful configuration's display overrides.
Form edits survive navigation, and notice expiry does not redraw the form.

## Build locally on Ubuntu 26.04

Install the native dependencies, then build with the WebKit2GTK 4.1 ABI:

```bash
sudo apt install build-essential pkg-config libgtk-3-dev libwebkit2gtk-4.1-dev
# From the rf-control repository:
cd cmd/rf-control-gui/frontend
npm ci
npm run build
cd ..
go build -trimpath -tags 'production,webkit2_41' \
  -o ../../dist/rf-control-gui-linux-amd64 .
```

This binary uses the Ubuntu system libraries. Use the RHEL build above for
customer deployments on RHEL 8.

Go callers can opt into bounded lock verification by setting the CW/sweep
configuration's `Force` pointer to a false boolean. A nil pointer (the default)
or true enables force mode. `BarracudaConfiguration.LockVerified` is false
when checks were skipped; `SignalLocked` is meaningful only when it is true.
The CLI reports unverified lock as "not checked" and uses force mode by default.
