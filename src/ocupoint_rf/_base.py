"""Shared base class for Ocupoint RF frontend clients.

Each device subclass binds itself to a generated _pb2 module and exposes
typed helper methods. The base handles connection lifecycle and request/
response plumbing so subclasses only describe the command surface.
"""

from __future__ import annotations

from typing import Optional

from .transport import DEFAULT_PORT, DEFAULT_TIMEOUT_S, ProtoTransport


class BaseClient:
    # Subclasses set _pb to their generated module (e.g. black_canyon_pb2).
    _pb = None

    def __init__(
        self,
        host: str,
        port: int = DEFAULT_PORT,
        timeout: float = DEFAULT_TIMEOUT_S,
    ) -> None:
        if self._pb is None:
            raise TypeError(f"{type(self).__name__} must set _pb to a generated module.")
        self._transport = ProtoTransport(host, port, timeout)

    @property
    def pb(self):
        """Generated protobuf module — exposes message and enum types."""
        return self._pb

    def __enter__(self):
        self._transport.connect()
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        self._transport.close()

    def connect(self) -> None:
        self._transport.connect()

    def close(self) -> None:
        self._transport.close()

    def _send(self, packet):
        return self._transport.request(packet, self._pb.Packet)

    def _new_packet(self):
        return self._pb.Packet()
