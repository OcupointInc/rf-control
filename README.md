# Ocupoint RF Control

Supported Ocupoint systems are controlled with one Windows executable. No
installer, source code, build tools, or additional command-line program is
required.

## 1. Download the application

Download **[rf-control-windows-amd64.exe](https://github.com/OcupointInc/rf-control/releases/download/latest/rf-control-windows-amd64.exe)**
from the [latest rf-control release](https://github.com/OcupointInc/rf-control/releases/tag/latest).

Save the executable anywhere convenient. Double-click it to open the Ocupoint RF
Control GUI. If Windows SmartScreen appears the first time, select **More info**
and then **Run anyway**.

## 2. Connect the system over USB-C

1. Power on the complete Ocupoint system.
2. Connect its USB-C control port to the Windows PC.
3. Double-click `rf-control-windows-amd64.exe`.
4. Select **Scan for devices**.
5. Find the system's USB-C connection in the device list and select **Connect**.
   The connection is displayed with its device name and Windows COM port, such
   as `USB-C · COM5`.

The application displays the controls available for the connected hardware.

## 3. Control the RF output

On **RF Control**, when available for the connected system:

1. Select **CW** or **Sweep**.
2. Enter the desired frequency or sweep settings.
3. Select the output power.
4. Select **Internal** or **External reference** for the clock source. The
   external reference may be 10–100 MHz and must be locked before RF is enabled.
5. Set the **RF ON/OFF** slider to the desired state.
6. Select **Apply**.

Changing a field or the RF slider does not immediately change the hardware.
The complete configuration is sent only when **Apply** is selected.

Use **Status** to view the live device state and lock indicators.

## 4. Set the Ethernet address

Keep the system connected over USB-C while changing its network settings.

1. Open **Network**.
2. Enter the new device IP address.
3. Review the automatically calculated subnet and gateway.
4. Select **Apply and reboot** and confirm the change.
5. Wait for the system to reboot.
6. Select **Scan for devices** again and connect to its Ethernet entry. You can
   also enter the new IP under **Connect directly**.

The network is configured as a `/24` subnet with the gateway at `.1`. For
example, device address `192.168.50.25` uses gateway `192.168.50.1`.

## 5. Save and reuse JSON configurations

The GUI can save the RF settings currently shown as a JSON profile.

- Select **Export**, choose a filename, and save the `.json` file.
- Select **Load** to put a saved profile back into the GUI.
- Loading a profile does not change the hardware until **Apply** is selected.

The exported profile contains the settings available on **RF Control**, such as
mode, frequency or sweep range/time, attenuation, clock source, and RF ON/OFF
state.

## 6. Apply a JSON configuration without opening the GUI

The same downloaded executable also works as a command-line tool when it is
started with arguments. This allows another program, script, or automation
system to apply a profile exported from the GUI.

Open PowerShell in the folder containing the executable and JSON file. Use the
COM port shown by the GUI scan:

```powershell
.\rf-control-windows-amd64.exe --usb COM5 apply .\rf-profile.json
```

To control the system over Ethernet, use its configured IP address:

```powershell
.\rf-control-windows-amd64.exe --ip 192.168.50.25 apply .\rf-profile.json
```

Each command connects to the device, applies the complete JSON
configuration, prints a JSON result, and exits. External software can run the
same command whenever it needs to change the hardware configuration.

Double-clicking the executable with no arguments always opens the GUI.
