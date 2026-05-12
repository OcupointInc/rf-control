"""Basic Straps control sequence.

Walks through calibration, attenuation, every RF band, and a manual
switch configuration. The unit advertises itself over mDNS as
`ocp_straps.local`.
"""

import time

from ocupoint_rf import StrapsClient
from ocupoint_rf.straps import RfBand, RfSwitchOption, MixerSwitchOption, IfSwitchOption


SERVER_IP = "ocp_straps.local"


def print_status(client: StrapsClient) -> None:
    s = client.get_status()
    print(
        f"  cal={s.calibration_enabled}  "
        f"channels={s.channels_enabled}  "
        f"atten={s.attenuation_db}dB  "
        f"lo={s.lo_frequency_mhz}MHz  "
        f"rf={RfSwitchOption.Name(s.rf_switch)}  "
        f"mixer={MixerSwitchOption.Name(s.mixer_switch)}  "
        f"if={IfSwitchOption.Name(s.if_switch)}"
    )


def main() -> None:
    with StrapsClient(SERVER_IP) as client:
        print("Initial status:")
        print_status(client)

        print("\nEnable channels, disable calibration, 0 dB frontend atten")
        client.set_channels_enabled(True)
        client.set_calibration_enabled(False)
        client.set_frontend_attenuation_db(0)
        print_status(client)

        print("\nSweep frontend attenuation")
        for db in (0, 10, 20, 30):
            client.set_frontend_attenuation_db(db)
            time.sleep(0.2)
            print_status(client)

        print("\nCycle every RF band")
        for band_name in RfBand.keys():
            print(f"  -> {band_name}")
            client.set_rf_band(band_name)
            time.sleep(0.2)
            print_status(client)

        print("\nManual switch configuration")
        client.set_switches(
            rf_switch="RF_SWITCH_OPTION_2GHZ_LPF",
            mixer_switch="MIXER_SWITCH_OPTION_BYPASS",
            if_switch="IF_SWITCH_OPTION_900MHZ_LPF",
        )
        print_status(client)

        print("\nSet LO frequency to 2250 MHz")
        client.set_pll_frequency_mhz(2250)
        print_status(client)


if __name__ == "__main__":
    main()
