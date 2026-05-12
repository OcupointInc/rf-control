"""Configure a Straps frontend for a specific RF band.

Edit BAND below (or pass it as the first CLI arg) and run:

    python examples/straps/set_rf_band.py
    python examples/straps/set_rf_band.py RF_BAND_900_1800MHZ

Valid bands (from the proto):
    RF_BAND_10_900MHZ
    RF_BAND_900_1800MHZ
    RF_BAND_1800_2700MHZ
    RF_BAND_2700_3600MHZ
    RF_BAND_3600_4500MHZ
"""

import sys

from ocupoint_rf import StrapsClient


SERVER_IP = "ocp_straps.local"
BAND = "RF_BAND_900_1800MHZ"
FRONTEND_ATTENUATION_DB = 0


def main() -> None:
    band = sys.argv[1] if len(sys.argv) > 1 else BAND

    with StrapsClient(SERVER_IP) as client:
        client.set_channels_enabled(True)
        client.set_calibration_enabled(False)
        client.set_rf_band(band)
        # Apply frontend attenuation last so it isn't perturbed by the band change.
        client.set_frontend_attenuation_db(FRONTEND_ATTENUATION_DB)

        status = client.get_status()
        print(
            f"band={band}  "
            f"atten={status.attenuation_db}dB  "
            f"lo={status.lo_frequency_mhz}MHz  "
            f"channels={status.channels_enabled}  "
            f"cal={status.calibration_enabled}"
        )


if __name__ == "__main__":
    main()
