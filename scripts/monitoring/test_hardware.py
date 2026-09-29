"""Hardware collection regressions: health failures, missing data and atomic output."""

import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location("hardware_metrics", Path(__file__).with_name("hardware-metrics.py"))
hardware = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(hardware)


def nvme_data(**changes):
    data = {"smart_status": {"passed": True}, "temperature": {"current": 41},
            "serial_number": "private-serial-must-not-be-exported",
            "nvme_smart_health_information_log": {"critical_warning": 0,
                "available_spare": 100, "available_spare_threshold": 10, "percentage_used": 7,
                "media_errors": 0, "num_err_log_entries": 5, "unsafe_shutdowns": 12}}
    data.update(changes)
    return data


class HardwareTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.pci = self.root / "bus/pci/devices/0000:01:00.0"
        self.pci.mkdir(parents=True)
        (self.pci / "aer_dev_correctable").write_text("RxErr 53051\nBadTLP 0\nTOTAL_ERR_COR 53051\n")
        (self.pci / "aer_dev_nonfatal").write_text("DLP 0\nTOTAL_ERR_NONFATAL 0\n")
        (self.pci / "aer_dev_fatal").write_text("DLP 0\nTOTAL_ERR_FATAL 0\n")
        self.btrfs = self.root / "fs/btrfs/example-fsid/devinfo/1"
        self.btrfs.mkdir(parents=True)
        (self.btrfs / "error_stats").write_text("write_errs 0\nread_errs 2\nflush_errs 0\ncorruption_errs 0\ngeneration_errs 0\n")
        (self.btrfs / "missing").write_text("0\n")

    def smart(self, data=None, code=0):
        return subprocess.CompletedProcess([], code, json.dumps(data or nvme_data()), "")

    def test_collects_counters_without_resetting_or_exporting_identifiers(self):
        before = (self.btrfs / "error_stats").read_bytes()
        with patch.object(hardware.subprocess, "run", return_value=self.smart()) as run:
            result = hardware.collect(["/dev/nvme0"], self.root)
        self.assertIn('stacklab_pcie_errors_total{device="0000:01:00.0",severity="correctable"} 53051', result)
        self.assertIn('stacklab_btrfs_device_errors_total{device_id="1",error="read",filesystem="example-fsid"} 2', result)
        self.assertIn('stacklab_nvme_health_passed{device="/dev/nvme0"} 1', result)
        self.assertNotIn("private-serial", result)
        self.assertEqual(before, (self.btrfs / "error_stats").read_bytes())
        self.assertEqual(run.call_args.args[0], ["/usr/sbin/smartctl", "--json", "--health", "--attributes", "/dev/nvme0"])
        self.assertEqual(run.call_args.kwargs["timeout"], 10)
        types = [line for line in result.splitlines() if line.startswith("# TYPE")]
        self.assertEqual(len(types), len(set(types)), "Each family must declare TYPE exactly once")

    def test_failing_disk_is_a_successful_collection_with_failed_health(self):
        data = nvme_data(smart_status={"passed": False})
        data["nvme_smart_health_information_log"]["critical_warning"] = 8
        with patch.object(hardware.subprocess, "run", return_value=self.smart(data, 8)):
            result = hardware.collect(["/dev/nvme0"], self.root)
        self.assertIn('stacklab_nvme_health_passed{device="/dev/nvme0"} 0', result)
        self.assertIn('stacklab_nvme_critical_warning{device="/dev/nvme0"} 8', result)
        self.assertIn('stacklab_hardware_collector_success{collector="nvme"} 1', result)

    def test_unreadable_smart_does_not_turn_into_healthy_or_zero_counters(self):
        for response in [self.smart(code=2), self.smart(code=4), self.smart({"smart_status": {"passed": True}}),
                         subprocess.CompletedProcess([], 0, "not json", "")]:
            with self.subTest(response=response), patch.object(hardware.subprocess, "run", return_value=response):
                result = hardware.collect(["/dev/nvme0"], self.root)
            self.assertIn('stacklab_nvme_collection_success{device="/dev/nvme0"} 0', result)
            self.assertNotIn("stacklab_nvme_health_passed", result)
            self.assertNotIn("stacklab_nvme_media_errors_total", result)
            self.assertIn('stacklab_hardware_collector_success{collector="pcie"} 1', result)

    def test_missing_smartctl_and_timeouts_preserve_other_collectors(self):
        for failure in [FileNotFoundError(), subprocess.TimeoutExpired("smartctl", 10)]:
            with self.subTest(failure=failure), patch.object(hardware.subprocess, "run", side_effect=failure):
                result = hardware.collect(["/dev/nvme0"], self.root)
            self.assertIn('stacklab_hardware_collector_success{collector="nvme"} 0', result)
            self.assertIn('stacklab_hardware_collector_success{collector="btrfs"} 1', result)
            self.assertIn("stacklab_hardware_collection_timestamp_seconds", result)

    def test_missing_btrfs_counters_mark_failure_without_partial_values(self):
        (self.btrfs / "error_stats").unlink()
        result = hardware.collect([], self.root)
        self.assertIn('stacklab_hardware_collector_success{collector="btrfs"} 0', result)
        self.assertNotIn("stacklab_btrfs_device_errors_total", result)
        self.assertIn("stacklab_pcie_errors_total", result)

    def test_unsupported_aer_omits_counters_instead_of_fabricating_zero(self):
        for p in self.pci.glob("aer_*"):
            p.unlink()
        result = hardware.collect([], self.root)
        self.assertIn('stacklab_hardware_devices{collector="pcie"} 0', result)
        self.assertNotIn("stacklab_pcie_errors_total", result)

    def test_malformed_aer_does_not_publish_partial_metrics(self):
        (self.pci / "aer_dev_fatal").write_text("bad -1\n")
        result = hardware.collect([], self.root)
        self.assertIn('stacklab_hardware_collector_success{collector="pcie"} 0', result)
        self.assertNotIn("stacklab_pcie_errors_total", result)

    def test_configuration_requires_explicit_controller_paths(self):
        config = self.root / "config.json"
        for devices in [["/dev/nvme0n1"], ["/dev/sda"], ["/dev/nvme0", "/dev/nvme0"], "auto", ["/dev/../etc/passwd"]]:
            config.write_text(json.dumps({"nvme_devices": devices}))
            with self.subTest(devices=devices), self.assertRaises(ValueError):
                hardware.configuration(config)
        config.write_text(json.dumps({"nvme_devices": ["/dev/nvme0"]}))
        self.assertEqual(hardware.configuration(config), ["/dev/nvme0"])

    def test_atomic_write_preserves_previous_scrape_on_failure(self):
        output = self.root / "hardware.prom"
        output.write_text("previous 1\n")
        with patch.object(hardware.os, "replace", side_effect=OSError("disk full")), self.assertRaises(OSError):
            hardware.write_atomic(output, "new 2\n")
        self.assertEqual(output.read_text(), "previous 1\n")
        self.assertFalse(list(self.root.glob(".stacklab-hardware-*")))
        hardware.write_atomic(output, "new 2\n")
        self.assertEqual(output.read_text(), "new 2\n")
        self.assertEqual(output.stat().st_mode & 0o777, 0o644)


if __name__ == "__main__":
    unittest.main()
