"""Client for the Black Canyon 1U RF frontend (VHF / UHF, 8 channels)."""

from __future__ import annotations

from ._base import BaseClient
from ._generated import black_canyon_pb2 as _pb


class BlackCanyonClient(BaseClient):
    _pb = _pb

    def set_calibration_enabled(self, enabled: bool):
        p = self._new_packet()
        p.set_cal_enabled_request.enabled = enabled
        return self._send(p)

    def set_channels_enabled(self, enabled: bool):
        p = self._new_packet()
        p.set_channels_enabled_request.enabled = enabled
        return self._send(p)

    def set_attenuation_db(self, attenuation_db: int):
        """Frontend attenuation in 1 dB steps, 0–32 dB."""
        p = self._new_packet()
        p.set_attenuation_request.attenuation_db = attenuation_db
        return self._send(p)

    def get_status(self):
        """Return the GetStatusResponse with calibration / attenuation / channel state."""
        p = self._new_packet()
        p.get_status_request.SetInParent()
        resp = self._send(p)
        return resp.get_status_response
