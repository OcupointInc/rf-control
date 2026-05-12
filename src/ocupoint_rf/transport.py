"""TCP transport for protobuf Packet messaging used by Ocupoint RF frontends.

Wire format: each request is a single SerializeToString() with no length prefix.
The device replies with exactly one response Packet that fits in a single recv().
"""

from __future__ import annotations

import socket
from typing import Optional


DEFAULT_PORT = 5000
DEFAULT_TIMEOUT_S = 5.0
DEFAULT_BUFFER_BYTES = 4096


class TransportError(Exception):
    """Raised when a request/response cycle fails."""


class ProtoTransport:
    def __init__(
        self,
        host: str,
        port: int = DEFAULT_PORT,
        timeout: float = DEFAULT_TIMEOUT_S,
        buffer_size: int = DEFAULT_BUFFER_BYTES,
    ) -> None:
        self.host = host
        self.port = port
        self.timeout = timeout
        self.buffer_size = buffer_size
        self._sock: Optional[socket.socket] = None

    def __enter__(self) -> "ProtoTransport":
        self.connect()
        return self

    def __exit__(self, exc_type, exc, tb) -> None:
        self.close()

    def connect(self) -> None:
        if self._sock is not None:
            return
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.settimeout(self.timeout)
        s.connect((self.host, self.port))
        s.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        self._sock = s

    def close(self) -> None:
        if self._sock is not None:
            try:
                self._sock.close()
            finally:
                self._sock = None

    def request(self, packet, response_cls):
        """Send a Packet and parse the response with response_cls.

        response_cls is the Packet class from the same generated module as
        the request (e.g. black_canyon_pb2.Packet).
        """
        if self._sock is None:
            raise TransportError("Transport is not connected; call connect() first.")
        data = packet.SerializeToString()
        self._sock.sendall(data)
        recv = self._sock.recv(self.buffer_size)
        if not recv:
            raise TransportError("Device closed the connection without responding.")
        response = response_cls()
        response.ParseFromString(recv)
        return response
