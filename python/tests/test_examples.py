"""Verify the example waits for Enter before switching from CW to sweep."""
from contextlib import nullcontext
from pathlib import Path
import runpy
import unittest
from unittest.mock import Mock, patch

from rf_control import DeviceError, pb


EXAMPLE = Path(__file__).resolve().parents[1] / 'examples/barracuda.py'


class InteractiveExampleTests(unittest.TestCase):
    def test_enter_switches_cw_to_sweep_then_exits(self):
        device = Mock()
        steps = iter([['cw'], ['cw', 'sweep']])

        def enter(_prompt):
            self.assertEqual([call[0] for call in device.method_calls], next(steps))
            return ''

        with patch('rf_control.Client.tcp', return_value=nullcontext(device)) as tcp, \
                patch('builtins.input', side_effect=enter) as prompt:
            runpy.run_path(str(EXAMPLE))
        tcp.assert_called_once_with('192.168.1.50')
        self.assertEqual(prompt.call_count, 2)
        device.cw.assert_called_once_with(500, power_dbm=-31.25, external_clock=False)
        device.sweep.assert_called_once_with(50, 1500, duration_s=0.01, power_dbm=-28,
                                             external_clock=False)

    def test_eof_leaves_cw_without_starting_sweep(self):
        device = Mock()
        with patch('rf_control.Client.tcp', return_value=nullcontext(device)), \
                patch('builtins.input', side_effect=EOFError), self.assertRaises(EOFError):
            runpy.run_path(str(EXAMPLE))
        self.assertEqual([call[0] for call in device.method_calls], ['cw'])

    def test_device_error_stops_without_retry_or_mute(self):
        device = Mock()
        device.cw.side_effect = DeviceError(pb.ERROR_CODE_HARDWARE_ERROR, 'device rejected command')
        with patch('rf_control.Client.tcp', return_value=nullcontext(device)), \
                patch('builtins.input') as enter, self.assertRaises(DeviceError):
            runpy.run_path(str(EXAMPLE))
        enter.assert_not_called()
        self.assertEqual([call[0] for call in device.method_calls], ['cw'])
