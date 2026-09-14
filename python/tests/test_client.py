import math
import unittest
from unittest.mock import patch

from rf_control import Client, DeviceError, ProtocolError, TransportError, pb


class FakeTransport:
    def __init__(self, handler=None):
        self.requests = []
        self.handler = handler
        self.closed = False

    def send(self, packet):
        self.requests.append(packet)
        if self.handler:
            return self.handler(packet)
        name = packet.WhichOneof('message_id').replace('_request', '_response')
        response = pb.Packet()
        getattr(response, name).SetInParent()
        if 'success' in getattr(response, name).DESCRIPTOR.fields_by_name:
            getattr(response, name).success = True
        return response

    def close(self):
        self.closed = True


def status(locked=True, **fields):
    detail = pb.BarracudaDiagnostics(lmx_locked=locked, lmx_requested_frequency_hz=9_600_000_000,
                                     lmx_output_power_code=50, **fields)
    return pb.Packet(get_status_response=pb.GetStatusResponse(
        board_type='barracuda', pll_locked=locked, barracuda=detail))


class ClientTests(unittest.TestCase):
    def test_every_protocol_operation_has_callable_wrapper(self):
        tx = FakeTransport()
        client = Client(tx)
        for descriptor in pb.Packet.DESCRIPTOR.fields:
            if not descriptor.name.endswith('_request'):
                continue
            operation = descriptor.name.removesuffix('_request')
            method = getattr(client, operation)
            with self.subTest(operation=operation):
                response = method(external=False) if operation == 'set_clock_source' else method()
                self.assertEqual(response.DESCRIPTOR, pb.Packet.DESCRIPTOR.fields_by_name[
                    operation + '_response'].message_type)
                self.assertEqual(tx.requests[-1].WhichOneof('message_id'), descriptor.name)

    def test_extended_chirp_fields_round_trip(self):
        tx = FakeTransport()
        Client(tx).set_chirp(start_freq_mhz=10000, deviation_mhz=100, ramp_time_us=2000,
                             mode=pb.CHIRP_MODE_TRIANGLE_TRIGGERED, enabled=True,
                             parabolic=True, delayed_start=True, delay_us=20, txdata_invert=True)
        received = pb.Packet.FromString(tx.requests[0].SerializeToString()).set_chirp_request
        self.assertTrue(received.parabolic)
        self.assertTrue(received.txdata_invert)
        self.assertEqual(received.delay_us, 20)
        self.assertEqual(received.start_freq_mhz, 10000)

    def test_clock_source_polarity_and_dsa_units(self):
        tx = FakeTransport()
        client = Client(tx)
        client.set_clock_source(external=True)
        client.set_dsa_attenuation_db(12.25)
        self.assertTrue(tx.requests[0].set_clock_source_request.external)
        self.assertEqual(tx.requests[1].set_dsa_attenuation_request.quarter_db, 49)
        for value in (-1, 32, 0.1, math.nan, math.inf):
            with self.assertRaises(ValueError):
                client.set_dsa_attenuation_db(value)
        self.assertEqual(len(tx.requests), 2)

    def test_errors_and_context_cleanup(self):
        tx = FakeTransport(lambda _: pb.Packet(error_response=pb.ErrorResponse(
            code=pb.ERROR_CODE_UNSUPPORTED, detail='unsupported on board')))
        with self.assertRaises(DeviceError) as error:
            with Client(tx) as client:
                client.set_adf_power(power_down=True)
        self.assertEqual(error.exception.code, pb.ERROR_CODE_UNSUPPORTED)
        self.assertTrue(tx.closed)
        with self.assertRaises(ProtocolError):
            Client(FakeTransport(lambda _: pb.Packet(get_config_response=pb.GetConfigResponse()))).get_status()

    def test_network_save_preserves_identity_and_surfaces_disconnect(self):
        def handler(packet):
            if packet.HasField('get_config_request'):
                return pb.Packet(get_config_response=pb.GetConfigResponse(
                    serial_number='SN2', mac_address=b'abcdef', mdns_hostname='test', enable_gateway_check=True))
            raise TransportError('reboot before reply')
        tx = FakeTransport(handler)
        with self.assertRaises(TransportError):
            Client(tx).configure_network(ip='192.168.1.5', gateway='192.168.1.1', subnet='255.255.255.0')
        req = tx.requests[-1].save_config_request
        self.assertEqual(req.serial_number, 'SN2')
        self.assertEqual(req.static_ip, b'\xc0\xa8\x01\x05')
        self.assertEqual(req.mac_address, b'abcdef')
        self.assertEqual(len(tx.requests), 2)

    def test_serial_record(self):
        tx = FakeTransport()
        Client(tx).set_serial('SN2')
        self.assertEqual(tx.requests[0].eeprom_write_request.data, b'\xa5\x03SN2')
        tx.handler = lambda _: pb.Packet(eeprom_read_response=pb.EepromReadResponse(data=b'\xa5\x03SN2'))
        self.assertEqual(Client(tx).get_serial(), 'SN2')

    def customer_transport(self, statuses, clock=None):
        queue = iter(statuses)
        def handler(packet):
            if packet.HasField('get_status_request'):
                return next(queue)
            if packet.HasField('set_clock_source_request'):
                return pb.Packet(set_clock_source_response=clock or pb.SetClockSourceResponse())
            return FakeTransport().send(packet)
        return FakeTransport(handler)

    @patch('rf_control.client.time.sleep')
    def test_delayed_lock_single_apply_cw(self, _sleep):
        tx = self.customer_transport([status(), status(False), status()])
        result = Client(tx).configure_barracuda_cw(100, attenuation_db=7.25, force=False)
        self.assertEqual(result.attenuation_db, 7.25)
        self.assertEqual(result.nominal_output_dbm, -32.25)
        self.assertTrue(result.signal_locked)
        self.assertEqual([p.set_dsa_attenuation_request.quarter_db for p in tx.requests
                          if p.HasField('set_dsa_attenuation_request')], [0, 29])
        self.assertEqual(sum(p.HasField('set_pll_frequency_request') for p in tx.requests), 1)

    @patch('rf_control.client.time.sleep')
    @patch('rf_control.client.time.monotonic', side_effect=[0, 3])
    def test_unlock_leaves_zero_not_max_or_requested(self, *_mocks):
        tx = self.customer_transport([status(), status(False)])
        with self.assertRaisesRegex(DeviceError, 'ADF4159 and LMX2595 did not lock'):
            Client(tx).configure_barracuda_cw(100, attenuation_db=20, force=False)
        self.assertEqual([p.set_dsa_attenuation_request.quarter_db for p in tx.requests
                          if p.HasField('set_dsa_attenuation_request')], [0])

    @patch('rf_control.client.time.sleep')
    def test_external_reference_settles_without_reapplying(self, _sleep):
        ready = status(lmk_ref_valid=4, lmk_refsel=1, lmk_dpll_lock=6)
        ready.get_status_response.clock_source_external = True
        tx = self.customer_transport([status(), status(), ready, status()], pb.SetClockSourceResponse(external=True))
        Client(tx).configure_barracuda_cw(100, external_clock=True, force=False)
        self.assertEqual(sum(p.HasField('set_clock_source_request') for p in tx.requests), 1)

    @patch('rf_control.client.time.monotonic', side_effect=[0, 3])
    def test_external_i2c_failure_not_treated_as_lock(self, _clock):
        failed = status(lmk_ref_valid=255, lmk_refsel=255, lmk_dpll_lock=255)
        failed.get_status_response.clock_source_external = True
        tx = self.customer_transport([status(), failed], pb.SetClockSourceResponse(external=True))
        with self.assertRaisesRegex(DeviceError, 'external reference did not lock'):
            Client(tx).configure_barracuda_cw(100, external_clock=True, force=False)
        self.assertFalse(any(p.HasField('set_lo_frequency_request') for p in tx.requests))

    @patch('rf_control.client.time.sleep')
    def test_sweep_uses_live_lock_not_immediate_chirp_response(self, _sleep):
        tx = self.customer_transport([status(), status(False), status()])
        result = Client(tx).configure_barracuda_sweep(50, 1500, 10000, attenuation_db=2, force=False)
        self.assertEqual(result.mode, 'sweep')
        chirp = next(p.set_chirp_request for p in tx.requests if p.HasField('set_chirp_request'))
        self.assertEqual((chirp.start_freq_mhz, chirp.deviation_mhz, chirp.ramp_time_us), (9650, 1450, 10000))

    def test_invalid_customer_plan_sends_nothing(self):
        tx = FakeTransport()
        with self.assertRaises(ValueError):
            Client(tx).configure_barracuda_sweep(200, 100, 1000)
        with self.assertRaises(ValueError):
            Client(tx).configure_barracuda_cw(10)
        self.assertEqual(tx.requests, [])

    def test_simple_cw_converts_power_to_dsa(self):
        for power, quarter_db in [(-25, 0), (-31.25, 25), (-56.75, 127)]:
            with self.subTest(power=power):
                tx = self.customer_transport([status(), status()])
                result = Client(tx).cw(500, power_dbm=power)
                self.assertEqual(result.nominal_output_dbm, power)
                self.assertEqual(tx.requests[-1].set_dsa_attenuation_request.quarter_db, quarter_db)
                self.assertEqual(next(p.set_pll_frequency_request.frequency_mhz for p in tx.requests
                                      if p.HasField('set_pll_frequency_request')), 10100)

    def test_simple_sweep_seconds_and_external_clock(self):
        for seconds, microseconds in [(0.000001, 1), (0.01, 10000), (10, 10000000),
                                       (4294.967295, 2**32 - 1)]:
            with self.subTest(seconds=seconds):
                clock = pb.SetClockSourceResponse(external=True, reference_valid=True,
                    reference_selected=True, dpll_frequency_locked=True, dpll_phase_locked=True)
                tx = self.customer_transport([status(), status()], clock)
                result = Client(tx).sweep(50, 1500, duration_s=seconds, power_dbm=-28,
                                          external_clock=True)
                self.assertEqual(result.sweep_time_us, microseconds)
                self.assertEqual(result.nominal_output_dbm, -28)
                self.assertTrue(result.external_clock)
                chirp = next(p.set_chirp_request for p in tx.requests if p.HasField('set_chirp_request'))
                self.assertEqual(chirp.ramp_time_us, microseconds)
                self.assertEqual(tx.requests[-1].set_dsa_attenuation_request.quarter_db, 12)

    def test_simple_invalid_units_send_nothing(self):
        tx = FakeTransport()
        client = Client(tx)
        for power in [-24, -57, -30.1, math.nan, math.inf, True]:
            with self.subTest(power=power):
                with self.assertRaisesRegex(ValueError, 'power_dbm'):
                    client.cw(500, power_dbm=power)
                with self.assertRaisesRegex(ValueError, 'power_dbm'):
                    client.sweep(50, 1500, duration_s=1, power_dbm=power)
        for seconds in [0, -1, 0.0000001, 0.0000015, 4294.967296, math.nan, math.inf, True]:
            with self.subTest(seconds=seconds):
                with self.assertRaisesRegex(ValueError, 'duration_s'):
                    client.sweep(50, 1500, duration_s=seconds)
        self.assertEqual(tx.requests, [])

    def test_simple_lock_failure_never_applies_requested_or_max_attenuation(self):
        for mode in ['cw', 'sweep']:
            with self.subTest(mode=mode), patch('rf_control.client.time.monotonic', side_effect=[0, 3]):
                tx = self.customer_transport([status(), status(False)])
                with self.assertRaisesRegex(DeviceError, 'did not lock'):
                    if mode == 'cw':
                        Client(tx).cw(500, power_dbm=-40, force=False)
                    else:
                        Client(tx).sweep(50, 1500, duration_s=0.01, power_dbm=-40, force=False)
                self.assertEqual([p.set_dsa_attenuation_request.quarter_db for p in tx.requests
                                  if p.HasField('set_dsa_attenuation_request')], [0])

    def test_force_defaults_to_tuning_without_lock_checks(self):
        for mode in ['cw', 'sweep', 'legacy_cw', 'legacy_sweep']:
            for external in [False, True]:
                with self.subTest(mode=mode, external=external), \
                        patch('rf_control.client.time.sleep', side_effect=AssertionError('unexpected lock wait')):
                    # No diagnostics or locks, and only one status response for board identity.
                    initial = pb.Packet(get_status_response=pb.GetStatusResponse(board_type='barracuda'))
                    tx = self.customer_transport([initial], pb.SetClockSourceResponse(external=external))
                    client = Client(tx)
                    kwargs = dict(external_clock=external)
                    if mode == 'cw':
                        result = client.cw(500, power_dbm=-31.25, **kwargs)
                    elif mode == 'sweep':
                        result = client.sweep(50, 1500, duration_s=0.01, power_dbm=-31.25, **kwargs)
                    elif mode == 'legacy_cw':
                        result = client.configure_barracuda_cw(500, attenuation_db=6.25, **kwargs)
                    else:
                        result = client.configure_barracuda_sweep(50, 1500, 10000, attenuation_db=6.25, **kwargs)
                    names = [p.WhichOneof('message_id') for p in tx.requests]
                    self.assertEqual(names, ['get_status_request', 'set_dsa_attenuation_request',
                        'set_clock_source_request', 'set_lo_frequency_request', 'set_lmx_output_power_request',
                        'set_chirp_request' if 'sweep' in mode else 'set_pll_frequency_request',
                        'set_dsa_attenuation_request'])
                    self.assertIsNone(result.signal_locked)
                    self.assertEqual(result.nominal_output_dbm, -31.25)
                    self.assertEqual([p.set_dsa_attenuation_request.quarter_db for p in tx.requests
                                      if p.HasField('set_dsa_attenuation_request')], [0, 25])

    def test_force_still_validates_settings_and_device(self):
        tx = FakeTransport()
        for call in [lambda: Client(tx).cw(0, force=True),
                     lambda: Client(tx).cw(500, power_dbm=0, force=True),
                     lambda: Client(tx).sweep(500, 50, duration_s=1, force=True),
                     lambda: Client(tx).cw(500, force='False')]:
            with self.assertRaises(ValueError):
                call()
        self.assertEqual(tx.requests, [])
        tx = self.customer_transport([pb.Packet(get_status_response=pb.GetStatusResponse(board_type='straps'))])
        with self.assertRaises(DeviceError):
            Client(tx).cw(500, force=True)
        self.assertEqual(len(tx.requests), 1)

    def test_force_does_not_swallow_transport_or_device_errors(self):
        for error in [TransportError('no reply'), DeviceError(pb.ERROR_CODE_HARDWARE_ERROR, 'write failed')]:
            with self.subTest(error=error):
                tx = self.customer_transport([status(False)])
                original = tx.handler
                def handler(packet):
                    if packet.HasField('set_pll_frequency_request'):
                        raise error
                    return original(packet)
                tx.handler = handler
                with self.assertRaises(type(error)):
                    Client(tx).cw(500, force=True)
                self.assertEqual([p.set_dsa_attenuation_request.quarter_db for p in tx.requests
                                  if p.HasField('set_dsa_attenuation_request')], [0])
