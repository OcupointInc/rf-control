import struct
import unittest

from rf_control import Client, FirmwareImage, ProtocolError, TransportError, pb
from test_client import FakeTransport


def image():
    return FirmwareImage.parse(struct.pack('<II24s16s24s', 0x4F435550, 0x46574930,
                                           b'barracuda', b'1.7.3', b'test') + b'payload')


class FirmwareTests(unittest.TestCase):
    def test_image_validation(self):
        self.assertEqual(image().board, 'barracuda')
        for bad in (b'', b'UF2', struct.pack('<II', 0x4F435550, 0x46574930), b'X' * (0x0FB000 + 1)):
            with self.assertRaises(ValueError):
                FirmwareImage.parse(bad)

    def transfer(self, *, data_handler=None, crc_ok=True):
        written = bytearray()
        def handler(packet):
            if packet.HasField('fw_update_begin_request'):
                return pb.Packet(fw_update_begin_response=pb.FwUpdateBeginResponse(max_chunk=30))
            if packet.HasField('fw_update_data_request'):
                request = packet.fw_update_data_request
                if data_handler:
                    return data_handler(request)
                self.assertEqual(request.offset, len(written))
                written.extend(request.data)
                return pb.Packet(fw_update_data_response=pb.FwUpdateDataResponse(next_offset=len(written)))
            if packet.HasField('fw_update_commit_request'):
                return pb.Packet(fw_update_commit_response=pb.FwUpdateCommitResponse(crc_ok=crc_ok))
            return FakeTransport().send(packet)
        return FakeTransport(handler), written

    def test_complete_firmware_transfer(self):
        tx, written = self.transfer()
        progress = []
        Client(tx).update_firmware(image(), progress=lambda done, total: progress.append((done, total)))
        self.assertEqual(bytes(written), image().data)
        self.assertEqual(progress[0], (0, len(written)))
        self.assertEqual(progress[-1], (len(written), len(written)))
        self.assertFalse(any(p.HasField('fw_update_abort_request') for p in tx.requests))

    def test_bad_ack_and_crc_abort(self):
        for options in ({'data_handler': lambda _: pb.Packet(fw_update_data_response=pb.FwUpdateDataResponse(next_offset=1000))},
                        {'crc_ok': False}):
            tx, _ = self.transfer(**options)
            with self.assertRaises(ProtocolError):
                Client(tx).update_firmware(image())
            self.assertTrue(tx.requests[-1].HasField('fw_update_abort_request'))

    def test_dropped_ack_is_not_blindly_replayed(self):
        def timeout(_):
            raise TransportError('lost acknowledgement')
        tx, _ = self.transfer(data_handler=timeout)
        with self.assertRaises(TransportError):
            Client(tx).update_firmware(image())
        self.assertEqual(sum(p.HasField('fw_update_data_request') for p in tx.requests), 1)
        self.assertTrue(tx.requests[-1].HasField('fw_update_abort_request'))

    def test_modified_metadata_rejected_before_sending(self):
        valid = image()
        forged = FirmwareImage(valid.data, 'wrong-board', valid.version, valid.build_id, valid.crc32)
        tx = FakeTransport()
        with self.assertRaises(ValueError):
            Client(tx).update_firmware(forged)
        self.assertEqual(tx.requests, [])
