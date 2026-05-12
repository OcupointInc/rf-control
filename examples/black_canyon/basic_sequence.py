"""Basic Black Canyon control sequence.

Connects, prints status, toggles channels and calibration, sweeps attenuation.
Set SERVER_IP to your device's address (find it in your router's DHCP table
or use the static IP printed on the unit).
"""

import time

from ocupoint_rf import BlackCanyonClient


SERVER_IP = "192.168.1.28"


def print_status(client: BlackCanyonClient) -> None:
    s = client.get_status()
    print(
        f"  calibration_enabled={s.calibration_enabled}  "
        f"attenuation_db={s.attenuation_db}  "
        f"channels_enabled={s.channels_enabled}"
    )


def main() -> None:
    with BlackCanyonClient(SERVER_IP) as client:
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
