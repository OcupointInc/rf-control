"""Configure a Whalepod eval board for normal operation.

On the eval board, calibration is always disabled, channels are always
enabled, and the attenuator sits at 0 dB. This script applies that
state and prints back the resulting status.

The unit advertises itself over mDNS as `ocp_whalepod.local`.
"""

from ocupoint_rf import WhalepodClient


SERVER_IP = "ocp_whalepod.local"


def main() -> None:
    with WhalepodClient(SERVER_IP) as client:
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
