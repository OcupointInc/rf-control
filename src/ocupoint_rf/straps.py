"""Client for the Straps RF frontend (banded LO + switched signal path)."""

from __future__ import annotations

from typing import Optional, Union

from ._base import BaseClient
from ._generated import straps_pb2 as _pb


# Re-export enums at module level so callers don't have to dig into pb.
RfSwitchOption = _pb.RfSwitchOption
MixerSwitchOption = _pb.MixerSwitchOption
IfSwitchOption = _pb.IfSwitchOption
RfBand = _pb.RfBand


def _resolve_enum(enum_descriptor, value: Union[int, str]) -> int:
    if isinstance(value, str):
        return enum_descriptor.Value(value)
    return int(value)


class StrapsClient(BaseClient):
    _pb = _pb

    # Re-export on the class too, for callers who already hold the client.
    RfSwitchOption = _pb.RfSwitchOption
    MixerSwitchOption = _pb.MixerSwitchOption
    IfSwitchOption = _pb.IfSwitchOption
    RfBand = _pb.RfBand

    def set_calibration_enabled(self, enabled: bool):
        p = self._new_packet()
        p.set_cal_enabled_request.enabled = enabled
        return self._send(p)

    def set_channels_enabled(self, enabled: bool):
        p = self._new_packet()
        p.set_channels_enabled_request.enabled = enabled
        return self._send(p)

    def set_frontend_attenuation_db(self, attenuation_db: int):
        p = self._new_packet()
        p.set_frontend_attenuation_request.attenuation_db = attenuation_db
        return self._send(p)

    def set_cal_attenuation_db(self, attenuation_db: int):
        p = self._new_packet()
        p.set_cal_attenuation_request.attenuation_db = attenuation_db
        return self._send(p)

    def set_pll_frequency_mhz(self, frequency_mhz: int):
        p = self._new_packet()
        p.set_pll_frequency_request.frequency_mhz = frequency_mhz
        return self._send(p)

    def set_rf_band(self, band: Union[int, str]):
        """Accepts either the enum int value or its name (e.g. "RF_BAND_900_1800MHZ")."""
        p = self._new_packet()
        p.set_rf_band_request.band = _resolve_enum(_pb.RfBand, band)
        return self._send(p)

    def set_switches(
        self,
        rf_switch: Optional[Union[int, str]] = None,
        mixer_switch: Optional[Union[int, str]] = None,
        if_switch: Optional[Union[int, str]] = None,
    ):
        """Set any combination of the three switch banks. Unset banks default to 0 in the wire message."""
        p = self._new_packet()
        req = p.set_switches_request
        if rf_switch is not None:
            req.rf_switch = _resolve_enum(_pb.RfSwitchOption, rf_switch)
        if mixer_switch is not None:
            req.mixer_switch = _resolve_enum(_pb.MixerSwitchOption, mixer_switch)
        if if_switch is not None:
            req.if_switch = _resolve_enum(_pb.IfSwitchOption, if_switch)
        return self._send(p)

    def get_status(self):
        p = self._new_packet()
        p.get_status_request.SetInParent()
        resp = self._send(p)
        return resp.get_status_response
