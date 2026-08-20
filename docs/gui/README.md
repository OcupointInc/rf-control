# RF Control desktop GUI

`rf-control` is a Wails desktop application for Ocupoint RF hardware.
It does not require internet access, use telemetry, load assets from a CDN, or
open an externally accessible web server. On Windows, the GUI and CLI are two
modes of the same executable.

Packaged targets are Windows on x86-64 and Red Hat Enterprise Linux 8.10 on
x86-64. The RHEL GUI uses the GTK3 and WebKit2GTK 4.0 libraries supplied by
RHEL 8.

## Run on Windows

Download `rf-control-windows-amd64.exe` from the GitHub release. It is the only
Windows binary: double-click it with no arguments to launch the GUI, or invoke
that same file with arguments from PowerShell to use the CLI. No installer is
required. The Barracuda control interface must use the Windows USB serial
driver and appear as a COM port in Device Manager.

For example, a tuning profile saved by the GUI can be applied later with:

```powershell
.\rf-control-windows-amd64.exe --usb COM4 apply barracuda-cw-400mhz.json
```

## Install on RHEL 8.10

Download both the GUI RPM and `rf-control-linux-amd64` CLI from the same GitHub
release.

Install the GUI RPM:

```bash
sudo dnf install ./rf-control-gui-0.1.0-1.el8.x86_64.rpm
```

The RPM declares its `gtk3` and `webkit2gtk3` runtime dependencies, so `dnf`
installs them from the configured RHEL repositories if needed.

Launch it from the application menu as **Ocupoint RF Control**, or run:

```bash
rf-control-gui
```

The raw `rf-control-gui-linux-amd64` executable is also published. Before using
the raw executable, install its runtime libraries explicitly:

```bash
sudo dnf install gtk3 webkit2gtk3
chmod +x rf-control-gui-linux-amd64
./rf-control-gui-linux-amd64
```

For the CLI fallback:

```bash
chmod +x rf-control-linux-amd64
./rf-control-linux-amd64 help
```

## USB-C access

The application must be able to open the Barracuda USB CDC control port,
normally `/dev/ttyACM1`. If the current lab account cannot open that device,
add it to the system's serial-device access group according to the lab's RHEL
policy, then sign out and back in. Ethernet control does not require serial
device permissions.

On Windows, the control interface must appear under **Ports (COM & LPT)** as a
USB serial/COM device. If Device Manager shows it using the **WinUSB** driver,
the serial transport cannot open it; restore the Windows USB serial (`usbser`)
driver for that interface.

## GUI workflow

1. Launch the application. It opens idle and does not scan automatically.
2. Enter the known address under **Connect directly** (`COM5` or an IPv4
   address), or explicitly scan for USB and Ethernet devices. A scan is bounded
   by a five-second timeout in both the GUI and native backend.
   On Windows, scanning filters USB serial ports by the Ocupoint VID/PID and
   probes the control protocol so the board name and identity appear immediately.
3. Select the Barracuda endpoint; the application opens directly on **RF
   Control**.
4. Configure CW or sweep, desired output power, and the clock source, then
   press **Apply**. Output power is entered from −25 to −56 dBm; the GUI maps
   that range to the internal 0–31 dB attenuation setting.
   The **RF ON/OFF** slider is a pending form setting and does not touch the
   hardware until **Apply** is pressed. Applying RF OFF powers the Barracuda
   LMX2595 down with a 0 MHz request; applying RF ON restores the fixed
   customer-plan LO plus calibrated LMX output power.
5. Use **Network** to view or assign the device IP. The GUI previews the
   automatically derived `/24` subnet and `.1` gateway before applying it.
6. Use **Registers** to read selected LMX2595 addresses or the complete
   R0–R112 map without changing the hardware. Results can be copied in the
   CLI's `R0=0x0000` text format or as JSON; the page also provides a copyable
   `rf-control read-lmx` command for the current USB or Ethernet connection.
7. Use **Export** on RF Control to save every setting currently shown in the
   form as a CLI-compatible tuning profile. **Load** restores it into the form
   without touching the hardware; press **Apply** when ready. Profiles contain
   mode, frequency or sweep range/time, attenuation, clock source, and RF
   ON/OFF state.
8. The application version is shown at the bottom of the sidebar. It matches
   the value printed by `rf-control version`: a release tag for stable builds
   or `latest-<commit>` for rolling builds.
9. Allow the device to reboot and reconnect at its Ethernet address.

The GUI displays only customer RF frequencies for Barracuda. LO, mixer, and
synthesizer settings remain internal. A failed Barracuda configuration leaves
the output safely attenuated.

Other Ocupoint firmware variants appear in discovery and provide read-only
identity/status plus network configuration. Their product-specific controls
remain available through the CLI until a customer workflow is defined for each
product.

## Build for RHEL 8

The production artifact is built inside a RHEL 8-compatible environment. Build
dependencies are:

```bash
sudo dnf install gcc gcc-c++ pkgconf-pkg-config gtk3-devel \
  webkit2gtk3-devel npm
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
```

Then build with the WebKit2GTK 4.0 ABI:

```bash
cd cmd/rf-control-gui
wails build -clean -trimpath -tags webkit2_40 \
  -o rf-control-gui-linux-amd64
```

The frontend uses pinned dependencies from `frontend/package-lock.json`.
Validate them and the TypeScript build with:

```bash
cd frontend
npm ci
npm audit --audit-level=high
npm run check
npm run build
```

## Build for Windows

Install Go, Node.js, and the pinned Wails CLI, then build from the GUI module:

```powershell
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
cd cmd/rf-control-gui
wails build -clean -windowsconsole -trimpath -m -o rf-control-windows-amd64.exe
```

The Windows build keeps console support for CLI mode. With no arguments it
detaches the console immediately and opens the desktop GUI.
