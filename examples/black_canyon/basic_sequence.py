"""Configure a Black Canyon unit for normal operation.

Applies the default operating state — channels enabled, calibration
disabled, 0 dB attenuation — and prints back the resulting status.

The unit advertises itself over mDNS as `ocp_bc.local`.
"""

from ocupoint_rf import BlackCanyonClient


SERVER_IP = "ocp_bc.local"


def main() -> None:
    with BlackCanyonClient(SERVER_IP) as client:
        client.set_calibration_enabled(False)
        client.set_channels_enabled(True)
        client.set_attenuation_db(0)

        s = client.get_status()
        print(
            f"calibration_enabled={s.calibration_enabled}  "
            f"channels_enabled={s.channels_enabled}  "
            f"attenuation_db={s.attenuation_db}"
        )


if __name__ == "__main__":
    main()
