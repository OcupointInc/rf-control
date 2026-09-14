"""Validated, framed OTA firmware update, matching the Go library protocol."""
from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import struct
import time
import zlib
from .errors import ProtocolError


@dataclass(frozen=True)
class FirmwareImage:
    data: bytes
    board: str
    version: str
    build_id: str
    crc32: int

    @classmethod
    def load(cls, path: str | Path) -> FirmwareImage:
        return cls.parse(Path(path).read_bytes())

    @classmethod
    def parse(cls, data: bytes) -> FirmwareImage:
        data = bytes(data)
        if not 0 < len(data) <= 0x0FB000:
            raise ValueError("firmware size must be 1..1028096 bytes")
        magic = struct.pack("<II", 0x4F435550, 0x46574930)
        for offset in range(0, len(data) - 7, 4):
            if data[offset:offset + 8] != magic:
                continue
            if offset + 72 > len(data):
                raise ValueError("truncated firmware identity")
            block = data[offset:offset + 72]
            board, version, build_id = (value.split(b'\x00', 1)[0].decode('ascii')
                                        for value in (block[8:32], block[32:48], block[48:72]))
            if not board:
                raise ValueError("empty firmware board identity")
            return cls(data, board, version, build_id, zlib.crc32(data))
        raise ValueError("OTA application .bin identity not found (UF2 is not an OTA image)")


def update_firmware(client, image: FirmwareImage, *, progress=None) -> None:
    """Transfer on an already selected update transport; abort on failure.

    Data acknowledgement offsets may rewind to recover a partial transfer.
    Ambiguous transport failures abort instead of replaying stateful commands.
    """
    verified = FirmwareImage.parse(image.data)
    if verified != image:
        raise ValueError("firmware image metadata or CRC differs from its data")
    total = len(image.data)
    try:
        begin = client.fw_update_begin(size=total, crc32=image.crc32, board=image.board, version=image.version)
        if not 0 < begin.max_chunk <= 60000:
            raise ProtocolError("invalid firmware chunk size")
        if progress:
            progress(0, total)
        offset = high_water = stalls = 0
        deadline = time.monotonic() + 300
        while offset < total:
            if time.monotonic() >= deadline:
                raise TimeoutError("firmware transfer exceeded five minutes")
            end = min(offset + begin.max_chunk, total)
            response = client.fw_update_data(offset=offset, data=image.data[offset:end])
            next_offset = response.next_offset
            if next_offset > end:
                raise ProtocolError("firmware acknowledged bytes that were not sent")
            if next_offset > high_water:
                high_water, stalls = next_offset, 0
            else:
                stalls += 1
                if stalls > 100:
                    raise ProtocolError("firmware transfer stalled")
            offset = next_offset
            if progress:
                progress(offset, total)
        if not client.fw_update_commit().crc_ok:
            raise ProtocolError("device rejected firmware CRC")
    except BaseException:
        try:
            client.fw_update_abort()
        except Exception:
            pass
        raise
