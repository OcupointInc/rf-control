# Whalepod eval board — setup guide

This guide walks through unpacking, powering, and talking to a
Whalepod evaluation board for the first time.

<!-- ![Whalepod eval board](board_overview.jpg) -->

---

## What's in the box

- Whalepod eval board
- [TODO: power supply — voltage, connector type]
- [TODO: any cables, antennas, or accessories shipped with it]

---

## Connecting power

[TODO: describe the power input — voltage, polarity, connector. Note
any power-up LEDs or indicators the customer should look for.]

<!-- ![Power connector](power_connector.jpg) -->

---

## Connecting to a network

The Whalepod talks over Ethernet on TCP port 5000.

1. Plug a CAT5/CAT6 cable from the board's Ethernet jack to your
   network switch or directly to a host.
2. The board pulls an IP address via DHCP. Confirm it's online by
   checking your router/DHCP server's lease table.
3. Once on the network, the board advertises itself over mDNS as
   `ocp_whalepod.local`.

<!-- ![Ethernet jack and link LEDs](ethernet_jack.jpg) -->

### Verifying discovery

From a host with mDNS resolution (macOS, most Linux distros with
Avahi, Windows with Bonjour installed):

```bash
ping ocp_whalepod.local
```

If the ping resolves, you're ready to talk to the board.

---

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

From here you can drop the same three calls into your own script and
start building.

---

## RF connections

[TODO: describe SMA / RF port layout — which connectors are the 2
inputs, which 3 are the outputs (2 UHF + 1 combined VHF), what the
expected signal levels are, any DC blocks or attenuators recommended
on the bench.]

<!-- ![RF port layout](rf_ports.jpg) -->

---

## Troubleshooting

**`ocp_whalepod.local` doesn't resolve.**
Confirm the board has pulled a DHCP lease. Try the raw IP directly:
`python examples/whalepod/basic_sequence.py` after editing
`SERVER_IP` at the top of the script. On Windows, install Apple's
Bonjour Print Services to get mDNS resolution.

**Connection refused / timeout.**
Make sure nothing else has port 5000 open to the board, and that no
firewall is blocking outbound TCP on your host.

**[TODO: any device-specific gotchas — boot time, LED states,
factory-reset procedure, expected behavior on first power-up]**

---

## Specifications

[TODO: pull from the datasheet — power consumption, RF input/output
ranges, gain, noise figure, dimensions, environmental ratings.]
