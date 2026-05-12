# Whalepod eval board — setup guide

This guide walks through unpacking, powering, and talking to a
Whalepod evaluation board for the first time.

![Whalepod test setup](img/test_setup.jpg)

---

## What's in the box

- Whalepod eval board
- Two Samtec RF cable assemblies — a 2-cable bundle (RF in) and a
  3-cable bundle (RF out)

You'll need to supply your own bench PSU capable of **12 V at 0.5 A**
(steady-state draw is ~0.25 A; 0.5 A gives margin for inrush).

---

## RF connections

Both RF bundles use Samtec edge connectors that mate to the headers
on either side of the board.

- **2-cable bundle → left side (RF in).** Top cable is **CH2**,
  bottom is **CH1**.
- **3-cable bundle → right side (RF out).** Top-to-bottom the cables
  are **UHF2**, **VHF1**, **UHF1**.

The silkscreen on the board (`RF IN 1` / `RF IN 2` on the left,
`UHF1` / `VHF1` / `UHF2` on the right) matches the cable order shown
in the test-setup photo above.

---

## Connecting power

Connect a 12 V supply to the headers labeled on the board (red to
`12V`, black to `GND`). Power on the supply — the board should draw
roughly **0.25 A at 12 V** in steady state.

---

## Connecting to a network

The Whalepod talks over Ethernet on TCP port 5000.

1. Plug a CAT5/CAT6 cable from the board's Ethernet jack to your
   network switch or directly to a host.
2. The board pulls an IP address via static IP.

<!-- ![Ethernet jack and link LEDs](ethernet_jack.jpg) -->


## First control script

Install the library (see the [top-level README](../../README.md#install))
and run the basic example:

```bash
python examples/whalepod/basic_sequence.py
```

This connects to `ocp_whalepod.local`, applies the default operating
state (channels enabled, calibration disabled, 0 dB attenuation), and
prints the resulting status. Expected output:

```
calibration_enabled=False  channels_enabled=True  attenuation_db=0
```

Change the argument to `set_attenuation_db()` in the script (or call
it from your own code) to sweep the attenuators across the signal
path.

---

## Reflashing the firmware

See [docs/firmware/README.md](../firmware/README.md) for the
drag-and-drop reflash procedure — it's the same across all Ocupoint
eval boards.

---

## Troubleshooting

**Connection refused / timeout.**
Make sure nothing else has port 5000 open to the board, and that no
firewall is blocking outbound TCP on your host.

---