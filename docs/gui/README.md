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
