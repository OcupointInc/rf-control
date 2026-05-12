"""Ocupoint RF frontend control library.

Each ethernet-attached RF frontend speaks the same wire protocol: a single
protobuf `Packet` per request, with a oneof discriminator for the request /
response type. Devices share `transport.ProtoTransport` and inherit
`_base.BaseClient`; per-device subclasses expose typed methods.
"""

from .black_canyon import BlackCanyonClient
from .straps import StrapsClient
from .transport import ProtoTransport, TransportError

__all__ = [
    "BlackCanyonClient",
    "StrapsClient",
    "ProtoTransport",
    "TransportError",
]
