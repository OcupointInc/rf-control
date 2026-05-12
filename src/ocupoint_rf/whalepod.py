"""Client for the Whalepod RF frontend.

Wire protocol is identical to Black Canyon's; this is a separate client
class so device-specific behavior or extensions can diverge over time
without touching the Black Canyon code.
"""

from __future__ import annotations

from ._base import BaseClient
from ._generated import whalepod_pb2 as _pb


class WhalepodClient(BaseClient):
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
        p = self._new_packet()
        p.set_attenuation_request.attenuation_db = attenuation_db
        return self._send(p)

    def get_status(self):
        p = self._new_packet()
        p.get_status_request.SetInParent()
        resp = self._send(p)
        return resp.get_status_response
