from rf_control import Client

# external_clock=False: internal reference; True: external 10 MHz reference.
with Client.tcp("192.168.1.50") as device:
    device.cw(500, power_dbm=-31.25, external_clock=False)
    input("CW applied. Press Enter to switch to sweep...")

    device.sweep(50, 1500, duration_s=0.01, power_dbm=-28,
                 external_clock=False)
    input("Sweep applied. Press Enter to exit (RF stays on)...")
