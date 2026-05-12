"""Apply a JSON command sequence to a Straps frontend.

Usage:
    python run_config.py configs/RF_BAND_900_1800MHZ.json [--ip 192.168.0.90]

JSON schema:
    {
      "server_ip": "192.168.0.90",
      "server_port": 5000,
      "commands": {
        "set_channels_enabled": true,
        "set_cal_enabled": false,
        "set_frontend_attenuation": 0,
        "set_cal_attenuation": 30,
        "set_rf_band": "RF_BAND_900_1800MHZ",
        "set_pll_frequency": 3000,
        "set_switches": {
          "rf_switch": "RF_SWITCH_OPTION_4GHZ_LPF",
          "mixer_switch": "MIXER_SWITCH_OPTION_MIXER",
          "if_switch": "IF_SWITCH_OPTION_1_2GHZ_BANDPASS"
        },
        "get_status": true
      }
    }

`set_frontend_attenuation` is applied last so it lands after band/switch
changes that might disturb the attenuator state.
"""

import argparse
import json
import logging
from pathlib import Path

from ocupoint_rf import StrapsClient


logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s", datefmt="%H:%M:%S")
log = logging.getLogger("straps.run_config")


DISPATCH = {
    "set_channels_enabled": lambda c, v: c.set_channels_enabled(bool(v)),
    "set_cal_enabled": lambda c, v: c.set_calibration_enabled(bool(v)),
    "set_cal_attenuation": lambda c, v: c.set_cal_attenuation_db(int(v)),
    "set_pll_frequency": lambda c, v: c.set_pll_frequency_mhz(int(v)),
    "set_rf_band": lambda c, v: c.set_rf_band(v),
    "set_switches": lambda c, v: c.set_switches(**v),
    "get_status": lambda c, v: c.get_status(),
    # `set_frontend_attenuation` handled separately (applied last).
}


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("config", type=Path)
    parser.add_argument("--ip", help="Override server_ip from the config file")
    parser.add_argument("--port", type=int, help="Override server_port from the config file")
    args = parser.parse_args()

    cfg = json.loads(args.config.read_text())
    ip = args.ip or cfg.get("server_ip")
    port = args.port or cfg.get("server_port", 5000)
    commands = cfg.get("commands") or {}
    if not ip:
        raise SystemExit("config is missing server_ip and --ip not supplied")

    # Defer frontend attenuation to the end so it isn't perturbed by later changes.
    frontend_atten = commands.pop("set_frontend_attenuation", None)

    log.info("Connecting to %s:%d", ip, port)
    with StrapsClient(ip, port) as client:
        for key, value in commands.items():
            handler = DISPATCH.get(key)
            if handler is None:
                log.warning("Unknown command %r, skipping", key)
                continue
            log.info("-> %s = %s", key, value)
            handler(client, value)

        if frontend_atten is not None:
            log.info("-> set_frontend_attenuation = %s", frontend_atten)
            client.set_frontend_attenuation_db(int(frontend_atten))

        status = client.get_status()
        log.info("Final status: atten=%ddB lo=%dMHz", status.attenuation_db, status.lo_frequency_mhz)


if __name__ == "__main__":
    main()
