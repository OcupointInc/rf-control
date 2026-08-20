# Ocupoint RF Control

Ocupoint RF Control discovers, configures, and monitors supported Ocupoint
hardware over USB-C or Ethernet. Download a ready-to-run file from GitHub; no
source code or build tools are required.

## 1. Download the application

Open the **[latest rf-control release](https://github.com/OcupointInc/rf-control/releases/tag/latest)**
and download the file for your computer:

| Operating system | Download | Interface |
| --- | --- | --- |
| Windows x86-64 | [`rf-control-windows-amd64.exe`](https://github.com/OcupointInc/rf-control/releases/download/latest/rf-control-windows-amd64.exe) | Desktop GUI and CLI |
| RHEL 8 x86-64 | [`rf-control-gui-0.1.0-1.el8.x86_64.rpm`](https://github.com/OcupointInc/rf-control/releases/download/latest/rf-control-gui-0.1.0-1.el8.x86_64.rpm) | Desktop GUI |
| Linux x86-64 | [`rf-control-linux-amd64`](https://github.com/OcupointInc/rf-control/releases/download/latest/rf-control-linux-amd64) | CLI |
| Linux ARM64 | [`rf-control-linux-arm64`](https://github.com/OcupointInc/rf-control/releases/download/latest/rf-control-linux-arm64) | CLI |
| macOS Intel | [`rf-control-darwin-amd64`](https://github.com/OcupointInc/rf-control/releases/download/latest/rf-control-darwin-amd64) | CLI |
| macOS Apple silicon | [`rf-control-darwin-arm64`](https://github.com/OcupointInc/rf-control/releases/download/latest/rf-control-darwin-arm64) | CLI |

The desktop GUI is currently published for Windows and RHEL 8. The macOS and
general Linux downloads provide the same command-line control used for external
automation.

On Linux or macOS, make a downloaded command-line file executable once before
running it, for example: `chmod +x rf-control-linux-amd64`.

To open the GUI on Windows, double-click
`rf-control-windows-amd64.exe`. If Windows SmartScreen appears the first time,
select **More info**, then **Run anyway**.

On RHEL 8, open **Ocupoint RF Control** after installing the downloaded RPM.

## 2. Connect over USB-C or Ethernet

1. Power on the complete Ocupoint system.
2. Open the Ocupoint RF Control GUI.
3. Connect using either method:
   - **USB-C:** Connect the system's USB-C control port to the computer, select
     **Scan for devices**, then select **Connect** beside its USB connection.
   - **Ethernet:** If the device IP address is already known, enter it under
     **Connect directly**. You can also select **Scan for devices** and connect
     to its Ethernet entry.
4. Confirm that the device shows as connected.

A USB connection includes its serial port in the description, such as
`USB-C · COM5` on Windows or `/dev/ttyACM1` on Linux.

## 3. Control the connected system

The GUI displays the tabs and controls supported by the connected hardware.

- Use the primary control tab to configure the system. Change the desired
  settings, then select **Apply** to send the complete configuration.
- Use **Status** to view the current device state, measurements, and applicable
  lock indicators.
- Use **Network** to view or change the Ethernet configuration.
- Use **Disconnect** before selecting a different device.

Controls can include modes, frequencies, output levels, clock sources,
switches, or other settings appropriate to the connected system. Where a
setting is pending, changing it in the GUI does not affect the hardware until
**Apply** is selected.

## 4. Set the Ethernet address

USB-C is recommended while changing network settings so the connection is not
lost during the update.

1. Open **Network**.
2. Enter the new device IP address.
3. Review the automatically calculated subnet and gateway.
4. Select **Apply and reboot** and confirm the change.
5. Wait for the system to reboot.
6. Scan again and connect to its Ethernet entry, or enter the new IP under
   **Connect directly**.

The network is configured as a `/24` subnet with the gateway at `.1`. For
example, device address `192.168.50.25` uses gateway `192.168.50.1`.

## 5. Save and reuse JSON configurations

When **Export** is available, it saves the settings currently shown in the GUI
as a JSON profile.

- Select **Export**, choose a filename, and save the `.json` file.
- Select **Load** to put a saved profile back into the GUI.
- Review the loaded settings, then select **Apply** to send them to the
  hardware.

Loading a profile does not change the hardware until **Apply** is selected.

## 6. Apply a JSON configuration without opening the GUI

The downloaded files also work as command-line tools. This allows another
program, script, or automation system to apply a JSON profile exported from the
GUI. Each command connects to the device, applies the profile, prints a JSON
result, and exits.

Use the device's known Ethernet address on any operating system:

**Windows**

```powershell
.\rf-control-windows-amd64.exe --ip 192.168.50.25 apply .\rf-profile.json
```

**Linux x86-64**

```bash
./rf-control-linux-amd64 --ip 192.168.50.25 apply ./rf-profile.json
```

**Linux ARM64**

```bash
./rf-control-linux-arm64 --ip 192.168.50.25 apply ./rf-profile.json
```

**macOS Intel**

```bash
./rf-control-darwin-amd64 --ip 192.168.50.25 apply ./rf-profile.json
```

**macOS Apple silicon**

```bash
./rf-control-darwin-arm64 --ip 192.168.50.25 apply ./rf-profile.json
```

USB works the same way by replacing `--ip ADDRESS` with the USB serial port:

```text
Windows:  --usb COM5
Linux:    --usb /dev/ttyACM1
macOS:    --usb /dev/cu.usbmodem101
```

External software can run the appropriate command whenever it needs to change
the hardware configuration. On Windows, launching the executable with no
arguments opens the GUI; passing arguments runs it as a CLI.
