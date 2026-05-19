# Reflashing the eval board firmware

All Ocupoint eval boards (Black Canyon, Straps, Whalepod) run on the
same control board: a WIZnet W55RP20-EVB-Pico (RP2040 + W5500
Ethernet). It ships with the RP2040 USB bootloader, so reflashing is
drag-and-drop — no toolchain required on the host.

## Steps

1. **Power off the external 12 V supply.** The board should be
   running on USB power only during the flash. Leaving the 12 V rail
   live while the RP2040 is in BOOTSEL can backfeed the RF section.
2. Connect the board to your host over USB.
3. Hold **BOOTSEL**, press and release **RESET**, then release
   BOOTSEL. The board will re-enumerate as a USB drive named
   `RPI-RP2`.
4. Drag the `.uf2` for your device (see below) onto the `RPI-RP2`
   drive. The board reboots automatically when the copy finishes and
   the drive disappears.
5. Reconnect the 12 V supply. The board should pull a DHCP lease and
   come up on its mDNS hostname within a few seconds.

<!-- ![BOOTSEL and RESET buttons](bootsel_reset.jpg) -->

## Firmware binaries

Download the `.uf2` for your device from the
[latest release](https://github.com/OcupointInc/rf-control/releases/latest).
Each release attaches one `.uf2` per device alongside the host CLI
binaries — pick the file matching your board:

| Device       | File               | mDNS hostname        |
| ------------ | ------------------ | -------------------- |
| Black Canyon | `black_canyon.uf2` | `ocp_bc.local`       |
| Straps       | `straps.uf2`       | `ocp_straps.local`   |
| Whalepod     | `whalepod.uf2`     | `ocp_whalepod.local` |

The firmware source is maintained in a separate repo; only the built
binaries are published as release assets here.
