#!/usr/bin/env python3
"""Regenerate `_pb2.py` modules from the proto files in `proto/`.

Run from anywhere; the script always writes into `src/ocupoint_rf/_generated/`.
Requires `protoc` on PATH (any version >= 3.20).
"""

import subprocess
import sys
from pathlib import Path


REPO = Path(__file__).resolve().parent.parent
PROTO_DIR = REPO / "proto"
OUT_DIR = REPO / "src" / "ocupoint_rf" / "_generated"


def main() -> int:
    OUT_DIR.mkdir(parents=True, exist_ok=True)
    protos = sorted(PROTO_DIR.glob("*.proto"))
    if not protos:
        print(f"No .proto files found in {PROTO_DIR}", file=sys.stderr)
        return 1
    cmd = [
        "protoc",
        f"--proto_path={PROTO_DIR}",
        f"--python_out={OUT_DIR}",
        *(str(p) for p in protos),
    ]
    print(" ".join(cmd))
    return subprocess.call(cmd)


if __name__ == "__main__":
    sys.exit(main())
