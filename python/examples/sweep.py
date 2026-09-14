"""Explicitly connect to one supplied device and start a repeating customer sweep."""
import argparse
from rf_control import Client, DeviceError, TransportError


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    target = parser.add_mutually_exclusive_group(required=True)
    target.add_argument('--host', help='Ethernet host/address')
    target.add_argument('--usb', help='USB binary control port, e.g. COM5')
    parser.add_argument('--start-mhz', required=True, type=int)
    parser.add_argument('--stop-mhz', required=True, type=int)
    parser.add_argument('--duration-s', required=True, type=float, help='Seconds per sweep')
    parser.add_argument('--power-dbm', type=float, default=-25, help='Nominal output power in dBm')
    parser.add_argument('--external-clock', action='store_true')
    args = parser.parse_args()
    connection = Client.tcp(args.host) if args.host else Client.usb(args.usb)
    with connection as device:
        print(device.get_config())
        result = device.sweep(args.start_mhz, args.stop_mhz, duration_s=args.duration_s,
                              power_dbm=args.power_dbm, external_clock=args.external_clock)
        print(f'Sweep applied: {args.start_mhz}–{args.stop_mhz} MHz, '
              f'{args.duration_s:g} s, nominal {result.nominal_output_dbm:g} dBm')


if __name__ == '__main__':
    try:
        main()
    except (DeviceError, TransportError, ValueError) as error:
        raise SystemExit(str(error))
