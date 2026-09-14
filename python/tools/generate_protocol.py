"""Regenerate Python bindings from the adjacent rf-control protocol."""
from pathlib import Path
import subprocess
import sys

root = Path(__file__).resolve().parents[2]
output = root / "python/src/rf_control"
subprocess.run([sys.executable, "-m", "grpc_tools.protoc", f"-I{root}",
                f"--python_out={output}", str(root / "control.proto")], check=True)
# A private pool allows applications to import this standalone library alongside
# the ATP gateway's older control.proto without colliding on protobuf symbols.
path = output / "control_pb2.py"
source = path.read_text().replace("_descriptor_pool.Default()", "_descriptor_pool.DescriptorPool()")
source = source.replace("'control_pb2', _globals", "'rf_control.control_pb2', _globals")
path.write_text(source)
