"""Failures distinguish device refusals from uncertain transport outcomes."""
from . import control_pb2 as pb


class DeviceError(RuntimeError):
    def __init__(self, code: int, detail: str):
        self.code, self.detail = code, detail
        try:
            name = pb.ErrorCode.Name(code)
        except ValueError:
            name = str(code)
        super().__init__(f"{name}: {detail}")


class TransportError(OSError):
    """A request could not complete; a sent command may already have applied."""


class ProtocolError(RuntimeError):
    """The reply was malformed or did not match the request."""
