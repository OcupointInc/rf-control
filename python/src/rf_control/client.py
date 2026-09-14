"""Typed device controls; protobuf responses retain all firmware diagnostics."""
from __future__ import annotations

from dataclasses import dataclass
import ipaddress
import math
import time
from . import control_pb2 as pb
from .errors import DeviceError, ProtocolError
from .transport import TCPTransport, USBTransport, Transport


@dataclass(frozen=True)
class BarracudaConfiguration:
    """Applied plan; signal_locked is None when force mode skips verification."""
    mode: str
    start_if_mhz: int
    stop_if_mhz: int
    sweep_time_us: int
    attenuation_db: float
    external_clock: bool
    signal_locked: bool | None

    @property
    def nominal_output_dbm(self) -> float:
        return -25.0 - self.attenuation_db


class Client:
    """One synchronous client per device; serialize access across threads.

    All low-level methods return their protobuf response. Firmware validates
    board support and advanced parameter combinations. A command failure never
    triggers a hidden attenuation change or automatic command replay.
    """
    def __init__(self, transport: Transport):
        self.transport = transport

    @classmethod
    def tcp(cls, host: str, port: int = 5000, **options) -> Client:
        return cls(TCPTransport(host, port, **options))

    @classmethod
    def usb(cls, device: str, **options) -> Client:
        return cls(USBTransport(device, **options))

    def close(self) -> None:
        self.transport.close()

    def __enter__(self) -> Client:
        return self

    def __exit__(self, *_args) -> None:
        self.close()

    def request(self, operation: str, **fields):
        """Send any protocol operation, e.g. request('set_phase', mode=2,
        phase_millidegrees=90000). Returns the matching protobuf response.
        """
        name = operation + "_request"
        descriptor = pb.Packet.DESCRIPTOR.fields_by_name.get(name)
        if descriptor is None:
            raise ValueError(f"unknown operation: {operation}")
        packet = pb.Packet()
        request = getattr(packet, name)
        request.CopyFrom(type(request)(**fields))
        reply = self.transport.send(packet)
        actual = reply.WhichOneof("message_id")
        if actual == "error_response":
            raise DeviceError(reply.error_response.code, reply.error_response.detail)
        expected = operation + "_response"
        if actual != expected:
            raise ProtocolError(f"expected {expected}, received {actual}")
        response = getattr(reply, expected)
        if "success" in response.DESCRIPTOR.fields_by_name and not response.success:
            raise DeviceError(pb.ERROR_CODE_HARDWARE_ERROR, f"{operation} reported failure")
        return response

    def set_attenuation(self, db: int):
        """Frontend attenuation in whole dB, 0..31."""
        _integer_range("attenuation", db, 0, 31)
        return self.set_frontend_attenuation(attenuation_db=db)

    def set_dsa_attenuation_db(self, db: float):
        """Barracuda DSA attenuation in dB, 0..31.75, in 0.25 dB steps."""
        return self.set_dsa_attenuation(quarter_db=_quarter_db(db))

    def set_clock_source(self, *, external: bool):
        """True selects the external 10 MHz reference; returns live lock detail."""
        return self.request("set_clock_source", external=external)

    def get_serial(self) -> str:
        """Read the AT24C01D EEPROM serial record (not network-config serial)."""
        data = self.eeprom_read(address=0, length=32).data
        if len(data) < 2 or data[0] != 0xA5 or not 1 <= data[1] <= 30 or len(data) < 2 + data[1]:
            raise ProtocolError("EEPROM serial is absent, unreadable, or invalid")
        return data[2:2 + data[1]].decode("ascii")

    def set_serial(self, serial: str):
        data = serial.encode("ascii")
        if not 1 <= len(data) <= 30:
            raise ValueError("serial must contain 1..30 ASCII characters")
        return self.eeprom_write(address=0, data=bytes((0xA5, len(data))) + data)

    def configure_network(self, *, ip: str, gateway: str, subnet: str,
                          hostname: str | None = None):
        """Preserve MAC/serial and save the supplied IPv4 plan; device reboots.

        A dropped reply remains a TransportError (outcome unknown). Reconnect
        and get_config() at the new address to verify; do not blindly repeat.
        """
        addresses = [ipaddress.IPv4Address(value).packed for value in (ip, gateway, subnet)]
        current = self.get_config()
        return self.save_config(static_ip=addresses[0], static_gateway=addresses[1],
                                static_subnet=addresses[2],
                                mdns_hostname=current.mdns_hostname if hostname is None else hostname,
                                enable_gateway_check=current.enable_gateway_check,
                                mac_address=current.mac_address, serial_number=current.serial_number)

    def cw(self, frequency_mhz: int, *, power_dbm: float = -25,
           external_clock: bool = False, lock_timeout: float = 2,
           force: bool = True) -> BarracudaConfiguration:
        """Generate a CW signal at 50..1500 MHz IF, with nominal output in dBm.

        Power accepts -56.75..-25 dBm in 0.25 dB steps. The internal clock is
        the default. force=True (default) skips reference/synthesizer lock and
        frequency/power readback verification and applies power immediately.
        force=False enables bounded verification; lock failure then raises
        DeviceError and leaves 0 dB attenuation.
        The returned signal_locked is None; device/transport errors still raise.
        """
        return self.configure_barracuda_cw(
            frequency_mhz, attenuation_db=_power_attenuation(power_dbm),
            external_clock=external_clock, lock_timeout=lock_timeout, force=force)

    def sweep(self, start_mhz: int, stop_mhz: int, *, duration_s: float,
              power_dbm: float = -25, external_clock: bool = False,
              lock_timeout: float = 2, force: bool = True) -> BarracudaConfiguration:
        """Start a repeating sawtooth IF sweep; duration_s is seconds per sweep.

        Duration must be a whole number of microseconds (1 us..4294.967295 s).
        Power and lock-failure behavior are the same as cw().
        """
        if isinstance(duration_s, bool) or not math.isfinite(duration_s):
            raise ValueError("duration_s must be finite seconds in whole microseconds")
        microseconds = duration_s * 1_000_000
        if not 0.000001 <= duration_s <= (2**32 - 1) / 1_000_000 or not math.isclose(
                microseconds, round(microseconds), rel_tol=0, abs_tol=1e-6):
            raise ValueError("duration_s must be 0.000001..4294.967295 seconds in whole microseconds")
        return self.configure_barracuda_sweep(
            start_mhz, stop_mhz, round(microseconds),
            attenuation_db=_power_attenuation(power_dbm),
            external_clock=external_clock, lock_timeout=lock_timeout, force=force)

    def configure_barracuda_cw(self, if_frequency_mhz: int, *, attenuation_db: float = 0,
                               external_clock: bool = False, lock_timeout: float = 2,
                               force: bool = True):
        """Apply the 9600 MHz LO customer plan; return the applied configuration.

        By default, force=True skips lock/readback verification and applies
        requested attenuation immediately, returning signal_locked=None. With
        force=False, attenuation stays at 0 dB until both synthesizers lock;
        lock failure raises DeviceError without forcing maximum attenuation.
        """
        _integer_range("IF frequency MHz", if_frequency_mhz, 50, 1500)
        code = _quarter_db(attenuation_db)
        _positive_timeout(lock_timeout)
        if not isinstance(force, bool):
            raise ValueError("force must be True or False")
        self._prepare_barracuda(external_clock, lock_timeout, force=force)
        self.set_pll_frequency(frequency_mhz=9600 + if_frequency_mhz)
        status = None if force else self._wait_barracuda(lock_timeout)
        self.set_dsa_attenuation(quarter_db=code)
        return BarracudaConfiguration("cw", if_frequency_mhz, if_frequency_mhz, 0,
                                      attenuation_db, external_clock, None if status is None else status.pll_locked)

    def configure_barracuda_sweep(self, start_if_mhz: int, stop_if_mhz: int,
                                  sweep_time_us: int, *, attenuation_db: float = 0,
                                  external_clock: bool = False, lock_timeout: float = 2,
                                  force: bool = True):
        """Apply continuous sawtooth IF sweep; same lock/attenuation policy as CW."""
        _integer_range("start IF MHz", start_if_mhz, 50, 1500)
        _integer_range("stop IF MHz", stop_if_mhz, 50, 1500)
        _integer_range("sweep time us", sweep_time_us, 1, 2**32 - 1)
        if stop_if_mhz <= start_if_mhz:
            raise ValueError("stop IF must exceed start IF")
        code = _quarter_db(attenuation_db)
        _positive_timeout(lock_timeout)
        if not isinstance(force, bool):
            raise ValueError("force must be True or False")
        self._prepare_barracuda(external_clock, lock_timeout, force=force)
        self.set_chirp(start_freq_mhz=9600 + start_if_mhz,
                       deviation_mhz=stop_if_mhz - start_if_mhz,
                       ramp_time_us=sweep_time_us,
                       mode=pb.CHIRP_MODE_SAWTOOTH_CONTINUOUS, enabled=True)
        status = None if force else self._wait_barracuda(lock_timeout)
        self.set_dsa_attenuation(quarter_db=code)
        return BarracudaConfiguration("sweep", start_if_mhz, stop_if_mhz, sweep_time_us,
                                      attenuation_db, external_clock, None if status is None else status.pll_locked)

    def _prepare_barracuda(self, external: bool, timeout: float, *, force: bool = True):
        status = self.get_status()
        if status.board_type != "barracuda":
            raise DeviceError(pb.ERROR_CODE_UNSUPPORTED, "customer plan requires Barracuda")
        self.set_dsa_attenuation(quarter_db=0)
        clock = self.set_clock_source(external=external)
        if clock.external != external:
            raise DeviceError(pb.ERROR_CODE_HARDWARE_ERROR,
                              "reference selection failed; attenuation remains at 0 dB")
        if not force and external and not (clock.reference_valid and clock.reference_selected and
                             clock.dpll_frequency_locked and clock.dpll_phase_locked):
            self._wait_external_reference(timeout)
        self.set_lo_frequency(frequency_mhz=9600)
        self.set_lmx_output_power(power_code=50)

    def _wait_external_reference(self, timeout: float):
        deadline = time.monotonic() + timeout
        while True:
            status = self.get_status()
            if not status.HasField("barracuda"):
                raise DeviceError(pb.ERROR_CODE_UNSUPPORTED,
                                  "firmware lacks reference verification; attenuation remains at 0 dB")
            d = status.barracuda
            valid = (d.lmk_ref_valid != 0xFF and d.lmk_refsel != 0xFF and d.lmk_dpll_lock != 0xFF)
            if valid and status.clock_source_external and d.lmk_ref_valid & 4 and d.lmk_refsel & 3 == 1 and d.lmk_dpll_lock & 6 == 6:
                return
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise DeviceError(pb.ERROR_CODE_HARDWARE_ERROR,
                                  "external reference did not lock; attenuation remains at 0 dB")
            time.sleep(min(0.05, remaining))

    def _wait_barracuda(self, timeout: float):
        deadline = time.monotonic() + timeout
        while True:
            status = self.get_status()
            if not status.HasField("barracuda"):
                raise DeviceError(pb.ERROR_CODE_UNSUPPORTED,
                                  "firmware lacks frequency verification; attenuation remains at 0 dB")
            details = status.barracuda
            if status.pll_locked and details.lmx_locked:
                if details.lmx_requested_frequency_hz != 9_600_000_000 or details.lmx_output_power_code != 50:
                    raise DeviceError(pb.ERROR_CODE_HARDWARE_ERROR,
                                      "frequency/power readback mismatch; attenuation remains at 0 dB")
                return status
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                missing = [name for name, locked in (("ADF4159", status.pll_locked),
                                                      ("LMX2595", details.lmx_locked)) if not locked]
                raise DeviceError(pb.ERROR_CODE_HARDWARE_ERROR,
                                  f"{' and '.join(missing)} did not lock; attenuation remains at 0 dB")
            time.sleep(min(0.05, remaining))

    def update_firmware(self, image, *, progress=None, port: int = 5002):
        """Install a validated FirmwareImage; use the dedicated TCP update port."""
        from .firmware import update_firmware
        from .transport import FirmwareTCPTransport
        if isinstance(self.transport, TCPTransport):
            tx = FirmwareTCPTransport(self.transport.host, port)
            try:
                return update_firmware(Client(tx), image, progress=progress)
            finally:
                tx.close()
        if isinstance(self.transport, USBTransport):
            original = self.transport.timeout
            self.transport.timeout = 20
            try:
                return update_firmware(self, image, progress=progress)
            finally:
                self.transport.timeout = original
        return update_firmware(self, image, progress=progress)


    def set_cal_enabled(self, *, enabled: bool = False) -> pb.SetCalibrationEnabledResponse:
        """Send SetCalibrationEnabledRequest; see the protocol field reference in README."""
        return self.request("set_cal_enabled", enabled=enabled)

    def set_frontend_attenuation(self, *, attenuation_db: int = 0) -> pb.SetFrontendAttenuationResponse:
        """Send SetAttenuationRequest; see the protocol field reference in README."""
        return self.request("set_frontend_attenuation", attenuation_db=attenuation_db)

    def set_channels_enabled(self, *, enabled: bool = False) -> pb.SetChannelsEnabledResponse:
        """Send SetChannelsEnabledRequest; see the protocol field reference in README."""
        return self.request("set_channels_enabled", enabled=enabled)

    def get_status(self) -> pb.GetStatusResponse:
        """Send GetStatusRequest; see the protocol field reference in README."""
        return self.request("get_status")

    def set_switches(self, *, rf_switch: int = 0, mixer_switch: int = 0, if_switch: int = 0) -> pb.SetSwitchesResponse:
        """Send SetSwitchesRequest; see the protocol field reference in README."""
        return self.request("set_switches", rf_switch=rf_switch, mixer_switch=mixer_switch, if_switch=if_switch)

    def set_pll_frequency(self, *, frequency_mhz: int = 0) -> pb.SetPllFrequencyResponse:
        """Send SetPllFrequencyRequest; see the protocol field reference in README."""
        return self.request("set_pll_frequency", frequency_mhz=frequency_mhz)

    def set_rf_band(self, *, band: int = 0) -> pb.SetRfBandResponse:
        """Send SetRfBandRequest; see the protocol field reference in README."""
        return self.request("set_rf_band", band=band)

    def set_cal_attenuation(self, *, attenuation_db: int = 0) -> pb.SetCalAttenuationResponse:
        """Send SetAttenuationRequest; see the protocol field reference in README."""
        return self.request("set_cal_attenuation", attenuation_db=attenuation_db)

    def save_config(self, *, static_ip: bytes = b"", static_gateway: bytes = b"", static_subnet: bytes = b"", mdns_hostname: str = "", enable_gateway_check: bool = False, mac_address: bytes = b"", serial_number: str = "") -> pb.SaveConfigResponse:
        """Send SaveConfigRequest; see the protocol field reference in README."""
        return self.request("save_config", static_ip=static_ip, static_gateway=static_gateway, static_subnet=static_subnet, mdns_hostname=mdns_hostname, enable_gateway_check=enable_gateway_check, mac_address=mac_address, serial_number=serial_number)

    def get_config(self) -> pb.GetConfigResponse:
        """Send GetConfigRequest; see the protocol field reference in README."""
        return self.request("get_config")

    def eeprom_read(self, *, address: int = 0, length: int = 0) -> pb.EepromReadResponse:
        """Send EepromReadRequest; see the protocol field reference in README."""
        return self.request("eeprom_read", address=address, length=length)

    def eeprom_write(self, *, address: int = 0, data: bytes = b"") -> pb.EepromWriteResponse:
        """Send EepromWriteRequest; see the protocol field reference in README."""
        return self.request("eeprom_write", address=address, data=data)

    def set_rf_switch_channel(self, *, channel: int = 0) -> pb.SetRfSwitchChannelResponse:
        """Send SetRfSwitchChannelRequest; see the protocol field reference in README."""
        return self.request("set_rf_switch_channel", channel=channel)

    def set_cal_source(self, *, internal: bool = False) -> pb.SetCalSourceResponse:
        """Send SetCalSourceRequest; see the protocol field reference in README."""
        return self.request("set_cal_source", internal=internal)

    def set_lo_frequency(self, *, frequency_mhz: int = 0) -> pb.SetLoFrequencyResponse:
        """Send SetLoFrequencyRequest; see the protocol field reference in README."""
        return self.request("set_lo_frequency", frequency_mhz=frequency_mhz)

    def set_chirp(self, *, start_freq_mhz: int = 0, deviation_mhz: int = 0, ramp_time_us: int = 0, mode: int = 0, enabled: bool = False, parabolic: bool = False, dual_ramp: bool = False, ramp2_deviation_mhz: int = 0, ramp2_time_us: int = 0, fast_ramp: bool = False, fast_ramp_down_time_us: int = 0, fsk_on_ramp_khz: int = 0, delayed_start: bool = False, ramp_delay: bool = False, delay_us: int = 0, triangular_delay: bool = False, txdata_trigger_delay: bool = False, external_step_clock: bool = False, txdata_invert: bool = False, muxout_ramp_complete: bool = False) -> pb.SetChirpResponse:
        """Send SetChirpRequest; see the protocol field reference in README."""
        return self.request("set_chirp", start_freq_mhz=start_freq_mhz, deviation_mhz=deviation_mhz, ramp_time_us=ramp_time_us, mode=mode, enabled=enabled, parabolic=parabolic, dual_ramp=dual_ramp, ramp2_deviation_mhz=ramp2_deviation_mhz, ramp2_time_us=ramp2_time_us, fast_ramp=fast_ramp, fast_ramp_down_time_us=fast_ramp_down_time_us, fsk_on_ramp_khz=fsk_on_ramp_khz, delayed_start=delayed_start, ramp_delay=ramp_delay, delay_us=delay_us, triangular_delay=triangular_delay, txdata_trigger_delay=txdata_trigger_delay, external_step_clock=external_step_clock, txdata_invert=txdata_invert, muxout_ramp_complete=muxout_ramp_complete)

    def set_dsa_attenuation(self, *, quarter_db: int = 0) -> pb.SetDsaAttenuationResponse:
        """Send SetDsaAttenuationRequest; see the protocol field reference in README."""
        return self.request("set_dsa_attenuation", quarter_db=quarter_db)

    def discovery(self) -> pb.DiscoveryResponse:
        """Send DiscoveryRequest; see the protocol field reference in README."""
        return self.request("discovery")

    def fw_update_begin(self, *, size: int = 0, crc32: int = 0, board: str = "", version: str = "") -> pb.FwUpdateBeginResponse:
        """Send FwUpdateBeginRequest; see the protocol field reference in README."""
        return self.request("fw_update_begin", size=size, crc32=crc32, board=board, version=version)

    def fw_update_data(self, *, offset: int = 0, data: bytes = b"") -> pb.FwUpdateDataResponse:
        """Send FwUpdateDataRequest; see the protocol field reference in README."""
        return self.request("fw_update_data", offset=offset, data=data)

    def fw_update_commit(self) -> pb.FwUpdateCommitResponse:
        """Send FwUpdateCommitRequest; see the protocol field reference in README."""
        return self.request("fw_update_commit")

    def fw_update_abort(self) -> pb.FwUpdateAbortResponse:
        """Send FwUpdateAbortRequest; see the protocol field reference in README."""
        return self.request("fw_update_abort")

    def gpio_self_test(self) -> pb.GpioSelfTestResponse:
        """Send GpioSelfTestRequest; see the protocol field reference in README."""
        return self.request("gpio_self_test")

    def lmx2595_register_read(self, *, addresses: tuple = ()) -> pb.Lmx2595RegisterReadResponse:
        """Send Lmx2595RegisterReadRequest; see the protocol field reference in README."""
        return self.request("lmx2595_register_read", addresses=addresses)

    def set_fsk(self, *, center_freq_mhz: int = 0, deviation_khz: int = 0, enabled: bool = False) -> pb.SetFskResponse:
        """Send SetFskRequest; see the protocol field reference in README."""
        return self.request("set_fsk", center_freq_mhz=center_freq_mhz, deviation_khz=deviation_khz, enabled=enabled)

    def set_phase(self, *, mode: int = 0, phase_millidegrees: int = 0) -> pb.SetPhaseResponse:
        """Send SetPhaseRequest; see the protocol field reference in README."""
        return self.request("set_phase", mode=mode, phase_millidegrees=phase_millidegrees)

    def set_adf_ref_config(self, *, r_counter: int = 0, ref_doubler: bool = False, ref_div2: bool = False, prescaler_8_9: bool = False, cp_current_code: int = 0) -> pb.SetAdfRefConfigResponse:
        """Send SetAdfRefConfigRequest; see the protocol field reference in README."""
        return self.request("set_adf_ref_config", r_counter=r_counter, ref_doubler=ref_doubler, ref_div2=ref_div2, prescaler_8_9=prescaler_8_9, cp_current_code=cp_current_code)

    def set_adf_loop_config(self, *, csr: bool = False, negative_bleed: bool = False, negative_bleed_code: int = 0, lol_disable: bool = False, integer_n_mode: bool = False) -> pb.SetAdfLoopConfigResponse:
        """Send SetAdfLoopConfigRequest; see the protocol field reference in README."""
        return self.request("set_adf_loop_config", csr=csr, negative_bleed=negative_bleed, negative_bleed_code=negative_bleed_code, lol_disable=lol_disable, integer_n_mode=integer_n_mode)

    def set_adf_power(self, *, power_down: bool = False, cp_three_state: bool = False, counter_reset: bool = False) -> pb.SetAdfPowerResponse:
        """Send SetAdfPowerRequest; see the protocol field reference in README."""
        return self.request("set_adf_power", power_down=power_down, cp_three_state=cp_three_state, counter_reset=counter_reset)

    def set_lmk_clock_outputs(self, *, lmx_enabled: bool = False, adf_enabled: bool = False, sma_a_enabled: bool = False, sma_b_enabled: bool = False) -> pb.SetLmkClockOutputsResponse:
        """Send SetLmkClockOutputsRequest; see the protocol field reference in README."""
        return self.request("set_lmk_clock_outputs", lmx_enabled=lmx_enabled, adf_enabled=adf_enabled, sma_a_enabled=sma_a_enabled, sma_b_enabled=sma_b_enabled)

    def set_lmk_reference_frequency(self, *, requested_frequency_hz: int = 0) -> pb.SetLmkReferenceFrequencyResponse:
        """Send SetLmkReferenceFrequencyRequest; see the protocol field reference in README."""
        return self.request("set_lmk_reference_frequency", requested_frequency_hz=requested_frequency_hz)

    def run_equalized_sweep(self, *, start_frequency_khz: int = 0, stop_frequency_khz: int = 0, sweep_time_us: int = 0, points: tuple = (), lo_frequency_mhz: int = 0, restore_entry_state: bool = False) -> pb.RunEqualizedSweepResponse:
        """Send RunEqualizedSweepRequest; see the protocol field reference in README."""
        return self.request("run_equalized_sweep", start_frequency_khz=start_frequency_khz, stop_frequency_khz=stop_frequency_khz, sweep_time_us=sweep_time_us, points=points, lo_frequency_mhz=lo_frequency_mhz, restore_entry_state=restore_entry_state)

    def set_lmx_output_power(self, *, power_code: int = 0) -> pb.SetLmxOutputPowerResponse:
        """Send SetLmxOutputPowerRequest; see the protocol field reference in README."""
        return self.request("set_lmx_output_power", power_code=power_code)


def _power_attenuation(power_dbm):
    if isinstance(power_dbm, bool) or not math.isfinite(power_dbm) or not -56.75 <= power_dbm <= -25:
        raise ValueError("power_dbm must be -56.75..-25 dBm in 0.25 dB steps")
    attenuation = -25.0 - power_dbm
    if abs(attenuation * 4 - round(attenuation * 4)) > 1e-9:
        raise ValueError("power_dbm must be -56.75..-25 dBm in 0.25 dB steps")
    return attenuation


def _integer_range(name, value, low, high):
    if isinstance(value, bool) or not isinstance(value, int) or not low <= value <= high:
        raise ValueError(f"{name} must be an integer in {low}..{high}")


def _positive_timeout(value):
    if not math.isfinite(value) or value <= 0:
        raise ValueError("lock_timeout must be positive and finite")


def _quarter_db(db):
    if not math.isfinite(db) or not 0 <= db <= 31.75 or abs(db * 4 - round(db * 4)) > 1e-9:
        raise ValueError("attenuation must be 0..31.75 dB in 0.25 dB steps")
    return round(db * 4)
