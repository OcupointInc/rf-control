"""Native transports. TCP retries only connection establishment, never a sent command."""
from __future__ import annotations

import random
import socket
import struct
import time
from typing import Protocol

from google.protobuf.message import DecodeError
from . import control_pb2 as pb
from .errors import ProtocolError, TransportError


class Transport(Protocol):
    def send(self, packet: pb.Packet) -> pb.Packet: ...
    def close(self) -> None: ...


def _decode(data: bytes) -> pb.Packet:
    try:
        result = pb.Packet.FromString(data)
    except DecodeError as exc:
        raise ProtocolError("invalid protobuf response") from exc
    if result.WhichOneof("message_id") is None:
        raise ProtocolError("empty or unsupported response packet")
    return result


def _exact(read, size: int) -> bytes:
    result = bytearray()
    while len(result) < size:
        chunk = read(size - len(result))
        if not chunk:
            raise TransportError("connection closed or timed out during response")
        result.extend(chunk)
    return bytes(result)


def _varint(read) -> tuple[int, bytes]:
    raw = bytearray()
    for shift in range(0, 70, 7):
        byte = _exact(read, 1)[0]
        raw.append(byte)
        if not byte & 128:
            return sum((b & 127) << (7 * i) for i, b in enumerate(raw)), bytes(raw)
    raise ProtocolError("oversized protobuf varint")


def _read_tcp_packet(read) -> pb.Packet:
    # Packet consists of exactly one length-delimited oneof message. Reading
    # that field's wire length handles TCP segmentation without adding framing.
    tag, tag_bytes = _varint(read)
    if tag == 0 or tag & 7 != 2:
        raise ProtocolError("response is not a Packet message field")
    size, size_bytes = _varint(read)
    if size > 65535:
        raise ProtocolError("response exceeds 65535 bytes")
    return _decode(tag_bytes + size_bytes + _exact(read, size))


def _frame(packet: pb.Packet) -> bytes:
    data = packet.SerializeToString()
    if not 0 < len(data) <= 65535:
        raise ValueError("frame payload must be 1..65535 bytes")
    return b"\xaa\x55" + struct.pack("<H", len(data)) + data


def _read_frame(read) -> pb.Packet:
    previous = 0
    while True:
        byte = _exact(read, 1)[0]
        if previous == 0xAA and byte == 0x55:
            size = struct.unpack("<H", _exact(read, 2))[0]
            if size:
                return _decode(_exact(read, size))
        previous = byte


class TCPTransport:
    def __init__(self, host: str, port: int = 5000, *, timeout: float = 5,
                 connect_attempts: int = 3):
        if timeout <= 0 or connect_attempts < 1:
            raise ValueError("timeout and connect_attempts must be positive")
        self.host, self.port = host, port
        self.timeout, self.connect_attempts = timeout, connect_attempts

    def send(self, packet: pb.Packet) -> pb.Packet:
        connection = None
        for attempt in range(self.connect_attempts):
            try:
                connection = socket.create_connection((self.host, self.port), self.timeout)
                break
            except OSError as exc:
                if attempt + 1 == self.connect_attempts:
                    raise TransportError(f"connect {self.host}:{self.port}: {exc}") from exc
                time.sleep(0.15 + random.random() * 0.15)
        try:
            with connection:
                deadline = time.monotonic() + self.timeout
                connection.sendall(packet.SerializeToString())
                def read(size):
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        raise TimeoutError("response deadline expired")
                    connection.settimeout(remaining)
                    return connection.recv(size)
                return _read_tcp_packet(read)
        except OSError as exc:
            raise TransportError(f"request outcome unknown: {exc}") from exc

    def close(self) -> None:
        pass


class USBTransport:
    def __init__(self, device: str, *, timeout: float = 2):
        if timeout <= 0:
            raise ValueError("timeout must be positive")
        try:
            import serial
        except ImportError as exc:
            raise ImportError("USB support requires pip install 'ocupoint-rf-control[usb]'") from exc
        self.timeout = timeout
        try:
            self.port = serial.Serial(device, baudrate=115200, timeout=0.1, write_timeout=timeout)
            self.port.dtr = True
            self.port.rts = True
        except OSError as exc:
            if hasattr(self, "port"):
                self.port.close()
            raise TransportError(f"open USB {device}: {exc}") from exc

    def send(self, packet: pb.Packet) -> pb.Packet:
        try:
            self.port.reset_input_buffer()
            frame = _frame(packet)
            if self.port.write(frame) != len(frame):
                raise TransportError("short USB write; request outcome unknown")
            deadline = time.monotonic() + self.timeout
            def read(size):
                while time.monotonic() < deadline:
                    self.port.timeout = min(0.1, max(0, deadline - time.monotonic()))
                    data = self.port.read(size)
                    if data:
                        return data
                raise TransportError("USB response timed out; request outcome unknown")
            return _read_frame(read)
        except OSError as exc:
            raise TransportError(str(exc)) from exc

    def close(self) -> None:
        self.port.close()


class FirmwareTCPTransport:
    """Persistent framed connection to the firmware-update service (port 5002)."""
    def __init__(self, host: str, port: int = 5002, *, timeout: float = 20):
        self.timeout = timeout
        self.connection = socket.create_connection((host, port), timeout)

    def send(self, packet: pb.Packet) -> pb.Packet:
        try:
            deadline = time.monotonic() + self.timeout
            self.connection.settimeout(self.timeout)
            self.connection.sendall(_frame(packet))
            def read(size):
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise TimeoutError("firmware response deadline expired")
                self.connection.settimeout(remaining)
                return self.connection.recv(size)
            return _read_frame(read)
        except OSError as exc:
            raise TransportError(str(exc)) from exc

    def close(self) -> None:
        self.connection.close()
