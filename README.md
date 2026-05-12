# ocupoint-rf-control

Unified Python client library for Ocupoint's Ethernet-controlled RF frontends.
Every device speaks the same shape of protocol — a single protobuf `Packet`
per request, sent over TCP, with a `oneof` discriminator for the request
type. This repo collects the per-device `.proto` files in one place and
wraps each device in a Pythonic client class so application code looks the
same regardless of which frontend you're talking to.

Supported devices today:

- **Black Canyon** — 1U dual-system frontend (VHF + UHF), 8 channels each
- **Straps** — banded LO frontend with switched signal path
- **Whalepod** — shares the Black Canyon command surface

Adding a new device follows a small, repeatable recipe (see [Adding a new
device](#adding-a-new-device) below).

---

## Install

```bash
pip install -e .
```

The only runtime dependency is `protobuf` (>= 4.25). Generated `_pb2.py`
modules are checked into the repo so end users don't need `protoc`
installed.

---

## Quick start

### Black Canyon

```python
from ocupoint_rf import BlackCanyonClient

with BlackCanyonClient("192.168.1.28") as bc:
    bc.set_channels_enabled(True)
    bc.set_calibration_enabled(False)
    bc.set_attenuation_db(10)

    status = bc.get_status()
    print(status.attenuation_db, status.channels_enabled)
```

### Straps

```python
from ocupoint_rf import StrapsClient
from ocupoint_rf.straps import RfBand

with StrapsClient("192.168.0.90") as s:
    s.set_channels_enabled(True)
    s.set_rf_band("RF_BAND_900_1800MHZ")   # name or RfBand.RF_BAND_900_1800MHZ
    s.set_frontend_attenuation_db(0)
    s.set_cal_attenuation_db(30)
    s.set_switches(
        rf_switch="RF_SWITCH_OPTION_4GHZ_LPF",
        mixer_switch="MIXER_SWITCH_OPTION_MIXER",
        if_switch="IF_SWITCH_OPTION_1_2GHZ_BANDPASS",
    )
    s.set_pll_frequency_mhz(2250)
    print(s.get_status())
```

### Apply a JSON config

```bash
python examples/straps/run_config.py examples/straps/configs/RF_BAND_900_1800MHZ.json
```

Each config file lists `server_ip`, `server_port`, and a `commands` map that
the runner translates into client calls.

---

## Layout

```
ocupoint-rf-control/
├── proto/                          authoritative .proto files (one per device)
│   ├── black_canyon.proto
│   └── straps.proto
├── src/ocupoint_rf/
│   ├── __init__.py                 re-exports the client classes
│   ├── transport.py                socket + send/recv plumbing
│   ├── _base.py                    BaseClient — connection lifecycle, _send helpers
│   ├── black_canyon.py             BlackCanyonClient
│   ├── straps.py                   StrapsClient + enum re-exports
│   └── _generated/                 protoc output (checked in)
│       ├── black_canyon_pb2.py
│       └── straps_pb2.py
├── examples/
│   ├── black_canyon/basic_sequence.py
│   └── straps/
│       ├── basic_sequence.py
│       ├── run_config.py
│       └── configs/*.json
├── scripts/regen_proto.py          regenerate _pb2.py modules
├── pyproject.toml
└── README.md
```

---

## Protocol & wire format

The wire format is identical for every device:

1. Client opens a TCP connection to port 5000.
2. Client serializes a single `Packet` protobuf (one of the request types
   filled in via the `oneof message_id`).
3. Client `sendall()`s the bytes — no length prefix, no framing.
4. Server replies with exactly one `Packet` whose `oneof` carries the
   matching response type.
5. Repeat. The connection can be held open for many request/response pairs.

Each device's `.proto` file lists its own request/response variants, but
the envelope and the `GetStatusRequest` / `GetStatusResponse` pair are
common.

### Per-device protos and wire compatibility

The `.proto` files in this repo use namespaced packages
(`ocupoint.black_canyon`, `ocupoint.straps`) so the generated Python
modules can coexist in the same process without descriptor collisions.

This is purely a Python-side concern: protobuf wire format depends only on
field numbers and wire types, not on package or message names. Existing
firmware that uses `package control;` is fully compatible — the bytes on
the wire are identical.

### Regenerating the Python bindings

```bash
python scripts/regen_proto.py
```

Requires `protoc` on PATH. Output lands in `src/ocupoint_rf/_generated/`.

---

## Adding a new device

When you build a new Ethernet-controlled frontend, follow this recipe:

1. **Author the proto.** Add `proto/<device>.proto`. Use
   `package ocupoint.<device>;`. Define request/response messages plus a
   top-level `Packet` with a `oneof message_id` listing every request and
   response. Keep field numbers in `Packet` stable forever — that's the
   wire contract with firmware.

2. **Regenerate Python bindings.**
   ```bash
   python scripts/regen_proto.py
   ```

3. **Add a client class.** Create `src/ocupoint_rf/<device>.py`:

   ```python
   from ._base import BaseClient
   from ._generated import <device>_pb2 as _pb

   class MyDeviceClient(BaseClient):
       _pb = _pb

       def do_thing(self, x: int):
           p = self._new_packet()
           p.do_thing_request.x = x
           return self._send(p)

       def get_status(self):
           p = self._new_packet()
           p.get_status_request.SetInParent()
           return self._send(p).get_status_response
   ```

   `BaseClient` handles the socket, the context manager, and request/response
   plumbing — you just describe the command surface.

4. **Re-export from the package.** Add an import line in
   `src/ocupoint_rf/__init__.py`.

5. **Add an example.** Drop `examples/<device>/basic_sequence.py` modeled
   on the existing ones.

That's it — application code can now `from ocupoint_rf import
MyDeviceClient` and use the same connect / call / status pattern as the
other devices.

---

## Conventions to keep

- **Port 5000 / TCP / TCP_NODELAY** on every device.
- **One `Packet` per request, one per response**, no length framing.
- **`GetStatusRequest` / `GetStatusResponse`** in every device proto.
- **`oneof message_id`** wrapping every request and response message.
- **Field numbers in `Packet` are immutable** once shipped — append-only.
- **Enums use the proto3 convention**: zero value is a meaningful default,
  enum names are `UPPER_SNAKE_CASE` with a shared prefix
  (`RF_BAND_*`, `RF_SWITCH_OPTION_*`, etc.).

---

## Migrating from the old per-device repos

If you have scripts written against `BlackCanyonClient/main.py` or
`StrapsClient/main.py`:

| Old                                                                   | New                                                           |
| --------------------------------------------------------------------- | ------------------------------------------------------------- |
| `import control_pb2`                                                  | `from ocupoint_rf._generated import black_canyon_pb2` (or `straps_pb2`) |
| Manual `socket.socket()` + `sendall` + `recv` + `Packet.ParseFromString` | `with BlackCanyonClient(ip) as c: ...`                        |
| `packet.set_attenuation_request.attenuation_db = N`                   | `c.set_attenuation_db(N)`                                     |
| `packet.set_frontend_attenuation_request.attenuation_db = N` (Straps) | `c.set_frontend_attenuation_db(N)`                            |
| `control_pb2.RfBand.Value("RF_BAND_…")`                               | `c.set_rf_band("RF_BAND_…")` or `StrapsClient.RfBand.…`       |

Wire-format-wise nothing changed; firmware on existing units works as-is.

---

## License

MIT — see `LICENSE`.
