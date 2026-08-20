# RF Control desktop GUI

`rf-control-gui` is a local Wails desktop application for Ocupoint RF hardware.
It does not require internet access, use telemetry, load assets from a CDN, or
open an externally accessible web server. The existing `rf-control` CLI remains
the dependency-free fallback.

The first packaged target is Red Hat Enterprise Linux 8.10 on x86-64. The GUI
uses the GTK3 and WebKit2GTK 4.0 libraries supplied by RHEL 8.

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

## GUI workflow

1. Launch the application and scan for devices.
2. Select the Barracuda USB-C endpoint.
3. Follow **First Bring-Up** to verify CW, sweep, and the external 10 MHz
   reference.
4. Use **Network** to view or assign the device IP. The GUI previews the
   automatically derived `/24` subnet and `.1` gateway before applying it.
5. Allow the device to reboot, scan again, and select its Ethernet endpoint.
6. Use **RF Control** for normal CW or sweep operation.

The GUI displays only customer IF frequencies for Barracuda. LO, mixer, and
synthesizer settings remain internal. A failed Barracuda configuration leaves
the output at maximum attenuation. The **Set maximum attenuation** button is
also always available on the RF Control screen.

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
