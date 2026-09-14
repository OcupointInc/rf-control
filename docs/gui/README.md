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

Barracuda status reports the ADF4159 and LMX2595 locks separately. Apply polls
both indicators for up to two seconds, so normal synthesizer acquisition delay
does not immediately fail the operation. During this prototype phase the DSA
is held at 0 dB attenuation until both locks are confirmed; on failure it stays
at 0 dB rather than automatically attenuating the output.

To preview the Barracuda controls without hardware, run the unified GUI binary
as a loopback simulator in one terminal:

```bash
./rf-control-gui-linux-amd64 mock-barracuda
```

Open a second copy normally and connect directly to `127.0.0.1` on port `5000`.
The same command is available from the Windows executable and the executable
inside the macOS application bundle.

External-reference acquisition is polled for up to two seconds within a single
Apply, without repeatedly selecting the clock. Failed Apply attempts refresh
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
