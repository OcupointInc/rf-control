# ocupoint-rf-control

Python client library for Ocupoint's Ethernet-controlled RF frontends.
Each device is wrapped in a Pythonic client class that handles
connection, commands, and status — your application code looks the
same regardless of which frontend you're talking to.

Supported devices:

- **Black Canyon** — 1U dual-system frontend (VHF + UHF), 8 channels each
- **Straps** — banded LO frontend with switched signal path
- **Whalepod** — 2-channel input, 3-channel output (2 UHF + 1 combined VHF)

---

## Install

This project is managed with [uv](https://docs.astral.sh/uv/):

```bash
uv sync
```

That creates `.venv/` and installs the package in editable mode along
with its dependencies pinned by `uv.lock`. Run anything inside the env
with `uv run`, e.g.
`uv run python examples/black_canyon/basic_sequence.py`.

To use the library from another project, add it as a dependency
(`uv add ocupoint-rf-control` once published, or
`uv add path/to/ocupoint-rf-control` for a local checkout). Plain
`pip install -e .` also works. The only runtime dependency is
`protobuf` (>= 4.25).

---

## Quick start

Each device advertises itself over mDNS, so you can connect by
hostname instead of IP: `ocp_bc.local`, `ocp_straps.local`,
`ocp_whalepod.local`. A raw IP works too.

### Black Canyon

```python
from ocupoint_rf import BlackCanyonClient

with BlackCanyonClient("ocp_bc.local") as bc:
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

with StrapsClient("ocp_straps.local") as s:
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

### Whalepod

```python
from ocupoint_rf import WhalepodClient

with WhalepodClient("ocp_whalepod.local") as w:
    w.set_channels_enabled(True)
    w.set_attenuation_db(10)
    print(w.get_status())
```

### Configure a Straps band

```bash
uv run python examples/straps/set_rf_band.py RF_BAND_900_1800MHZ
```

Edit `BAND`/`SERVER_IP` at the top of the script, or pass the band
name as the first argument.

---

## Examples

Runnable scripts live under [`examples/`](examples/):

- [`examples/black_canyon/basic_sequence.py`](examples/black_canyon/basic_sequence.py)
- [`examples/straps/basic_sequence.py`](examples/straps/basic_sequence.py)
- [`examples/straps/set_rf_band.py`](examples/straps/set_rf_band.py)
- [`examples/whalepod/basic_sequence.py`](examples/whalepod/basic_sequence.py)

---

## License

MIT — see [LICENSE](LICENSE).
