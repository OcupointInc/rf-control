"""Configure a Straps unit for normal operation.

Applies the default operating state — channels enabled, calibration
disabled, 0 dB frontend attenuation — and prints back the resulting
status. Pick an RF band with `set_rf_band.py`.

The unit advertises itself over mDNS as `ocp_straps.local`.
"""

from ocupoint_rf import StrapsClient


SERVER_IP = "ocp_straps.local"


def main() -> None:
    with StrapsClient(SERVER_IP) as client:
        client.set_channels_enabled(True)
        client.set_calibration_enabled(False)
        client.set_frontend_attenuation_db(0)

        s = client.get_status()
        print(
            f"calibration_enabled={s.calibration_enabled}  "
            f"channels_enabled={s.channels_enabled}  "
            f"frontend_attenuation_db={s.attenuation_db}  "
            f"lo_mhz={s.lo_frequency_mhz}"
        )


if __name__ == "__main__":
    main()
