"""Explicit, read-only Ethernet and USB device discovery."""
from __future__ import annotations

import math
import socket
import time
from . import control_pb2 as pb
from .client import Client
from .errors import ProtocolError, TransportError, DeviceError
from .transport import _decode


def discover_ethernet(timeout: float = 1, *, broadcast: str = "255.255.255.255",
                      port: int = 5001) -> list[pb.DiscoveryResponse]:
    if not math.isfinite(timeout) or timeout <= 0:
        raise ValueError("timeout must be positive and finite")
    devices = {}
    try:
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as connection:
            connection.setsockopt(socket.SOL_SOCKET, socket.SO_BROADCAST, 1)
            connection.bind(("0.0.0.0", 0))
            connection.settimeout(timeout)
            packet = pb.Packet(discovery_request=pb.DiscoveryRequest())
            connection.sendto(packet.SerializeToString(), (broadcast, port))
            deadline = time.monotonic() + timeout
            while (remaining := deadline - time.monotonic()) > 0:
                connection.settimeout(remaining)
                try:
                    payload, _ = connection.recvfrom(65535)
                except socket.timeout:
                    break
                try:
                    response = _decode(payload)
                except ProtocolError:
                    continue
                if response.WhichOneof("message_id") != "discovery_response":
                    continue
                device = response.discovery_response
                if len(device.ip) == 4:
                    devices[(device.mac, device.ip)] = device
    except OSError as exc:
        raise TransportError(f"Ethernet discovery failed: {exc}") from exc
    return sorted(devices.values(), key=lambda device: bytes(device.ip))


def list_candidate_ports() -> list[str]:
    """Enumerate Ocupoint VID/PID, falling back to USB CDC names without IDs."""
    try:
        from serial.tools import list_ports
    except ImportError as exc:
        raise ImportError("USB discovery requires 'ocupoint-rf-control[usb]'") from exc
    ports = list(list_ports.comports())
    identified = any(p.vid is not None and p.pid is not None for p in ports)
    if identified:
        return sorted({p.device for p in ports if p.vid == 0x2E8A and p.pid == 0x000A})
    names = {p.device for p in ports}
    return sorted(name for name in names
                  if (name.startswith('/dev/ttyACM') or 'usbmodem' in name or name.startswith('COM'))
                  and not (name.startswith('/dev/tty.') and name.replace('/dev/tty.', '/dev/cu.', 1) in names))


def is_control_port(device: str, *, timeout: float = 1.5) -> bool:
    try:
        with Client.usb(device, timeout=timeout) as client:
            client.get_config()
        return True
    except (OSError, ProtocolError, DeviceError):
        return False


def discover_usb_ports(*, timeout: float = 1.5) -> list[str]:
    """Probe candidate CDC ports with GetConfig; returns all responding ports."""
    return [device for device in list_candidate_ports() if is_control_port(device, timeout=timeout)]
