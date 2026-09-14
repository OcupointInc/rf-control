import io
import socket
import struct
import threading
import unittest
from unittest.mock import patch

from rf_control import Client, ProtocolError, TCPTransport, TransportError, USBTransport, pb
from rf_control.transport import _frame, _read_frame, _read_tcp_packet
from rf_control.discovery import discover_ethernet


class TransportTests(unittest.TestCase):
    def test_fragmented_tcp_packet(self):
        expected = pb.Packet(get_status_response=pb.GetStatusResponse(board_type='barracuda'))
        data = io.BytesIO(expected.SerializeToString())
        self.assertEqual(_read_tcp_packet(lambda n: data.read(min(n, 1))), expected)

    def test_truncated_tcp_and_malformed_frames(self):
        with self.assertRaises(TransportError):
            _read_tcp_packet(io.BytesIO(b'\x42\x05\x01').read)
        with self.assertRaises(ProtocolError):
            _read_tcp_packet(io.BytesIO(b'\x08\x00').read)
        with self.assertRaises(ProtocolError):
            _read_tcp_packet(io.BytesIO(b'\x80' * 10).read)
        with self.assertRaises(ProtocolError):
            _read_frame(io.BytesIO(b'\xaa\x55\x01\x00\xff').read)

    def test_usb_frame_resync_and_partial_reads(self):
        expected = pb.Packet(get_config_response=pb.GetConfigResponse(serial_number='SN2'))
        raw = io.BytesIO(b'noise\xaa\xaa\x55\x00\x00' + _frame(expected))
        self.assertEqual(_read_frame(lambda n: raw.read(min(1, n))), expected)

    def test_loopback_tcp_fragmentation(self):
        # Only loopback sockets are used; no RF equipment is contacted.
        with socket.socket() as listener:
            listener.bind(('127.0.0.1', 0))
            listener.listen()
            listener.settimeout(2)
            received = []
            def server():
                conn, _ = listener.accept()
                with conn:
                    received.append(_read_tcp_packet(conn.recv))
                    response = pb.Packet(get_config_response=pb.GetConfigResponse(serial_number='SN2'))
                    for byte in response.SerializeToString():
                        conn.sendall(bytes([byte]))
            thread = threading.Thread(target=server, daemon=True)
            thread.start()
            with Client.tcp('127.0.0.1', listener.getsockname()[1], timeout=2) as client:
                self.assertEqual(client.get_config().serial_number, 'SN2')
            thread.join(2)
            self.assertFalse(thread.is_alive())
            self.assertTrue(received[0].HasField('get_config_request'))

    @patch('rf_control.transport.time.sleep')
    @patch('rf_control.transport.socket.create_connection', side_effect=ConnectionRefusedError('busy'))
    def test_connect_retry_bounded(self, connect, _sleep):
        with self.assertRaises(TransportError):
            Client(TCPTransport('uncontacted.invalid')).get_status()
        self.assertEqual(connect.call_count, 3)

    @patch('rf_control.transport.socket.create_connection')
    def test_sent_command_is_never_replayed(self, connect):
        connection = connect.return_value.__enter__.return_value
        # The transport accesses the same socket object inside its context.
        connection = connect.return_value
        connection.recv.side_effect = TimeoutError('no reply')
        with self.assertRaises(TransportError):
            Client(TCPTransport('uncontacted.invalid')).set_phase(mode=pb.PHASE_MODE_STATIC, phase_millidegrees=90000)
        self.assertEqual(connect.call_count, 1)
        self.assertEqual(connection.sendall.call_count, 1)

    def test_usb_send_framing_and_close(self):
        response = pb.Packet(get_config_response=pb.GetConfigResponse(serial_number='SN3'))
        class Port:
            def __init__(self):
                self.source = io.BytesIO(_frame(response))
                self.written = b''
                self.closed = False
            def read(self, n): return self.source.read(min(n, 1))
            def write(self, value): self.written = value; return len(value)
            def reset_input_buffer(self): pass
            def close(self): self.closed = True
        transport = object.__new__(USBTransport)
        transport.port, transport.timeout = Port(), 1
        with Client(transport) as client:
            self.assertEqual(client.get_config().serial_number, 'SN3')
        self.assertEqual(transport.port.written, b'\xaa\x55\x03\x00\x9a\x01\x00')
        self.assertTrue(transport.port.closed)

    def test_loopback_discovery_deduplicates_and_ignores_garbage(self):
        with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as server:
            server.bind(('127.0.0.1', 0))
            server.settimeout(2)
            def reply():
                _, address = server.recvfrom(100)
                server.sendto(b'garbage', address)
                for ip in (b'\xc0\xa8\x01\x03', b'\xc0\xa8\x01\x02', b'\xc0\xa8\x01\x03'):
                    server.sendto(pb.Packet(discovery_response=pb.DiscoveryResponse(
                        ip=ip, mac=b'abcdef', control_port=5000)).SerializeToString(), address)
            thread = threading.Thread(target=reply, daemon=True)
            thread.start()
            devices = discover_ethernet(0.2, broadcast='127.0.0.1', port=server.getsockname()[1])
            thread.join(2)
            self.assertEqual([d.ip[-1] for d in devices], [2, 3])
