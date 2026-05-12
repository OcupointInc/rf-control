"""Basic Whalepod control sequence.

Same command surface as Black Canyon: toggle channels and calibration,
sweep attenuation, print status. The unit advertises itself over mDNS
as `ocp_whalepod.local`.
"""

import time

from ocupoint_rf import WhalepodClient


SERVER_IP = "ocp_whalepod.local"


def print_status(client: WhalepodClient) -> None:
    s = client.get_status()
    print(
        f"  calibration_enabled={s.calibration_enabled}  "
        f"attenuation_db={s.attenuation_db}  "
        f"channels_enabled={s.channels_enabled}"
    )


def main() -> None:
    with WhalepodClient(SERVER_IP) as client:
        print("Initial status:")
        print_status(client)

        for enabled in (False, True):
            print(f"\nChannels enabled = {enabled}")
            client.set_channels_enabled(enabled)
            time.sleep(0.5)
            print_status(client)

        for enabled in (True, False):
            print(f"\nCalibration enabled = {enabled}")
            client.set_calibration_enabled(enabled)
            time.sleep(0.5)
            print_status(client)

        for db in (0, 10, 20, 30):
            print(f"\nAttenuation = {db} dB")
            client.set_attenuation_db(db)
            time.sleep(0.5)
            print_status(client)


if __name__ == "__main__":
    main()
