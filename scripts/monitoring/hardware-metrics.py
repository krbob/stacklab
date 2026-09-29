#!/usr/bin/env python3
"""Read Linux PCIe AER, Btrfs and NVMe health into a node_exporter textfile.

No device settings, counters or self-tests are changed. NVMe devices must be
explicitly selected in a root-owned config; ATA/SATA disks are not polled.
"""

import argparse
import json
import math
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time

HELP = {
    "stacklab_pcie_errors_total": "PCI device AER events since device initialization, by severity.",
    "stacklab_pcie_correctable_errors_total": "Corrected PCI device AER events by error type.",
    "stacklab_pcie_device_info": "PCI address and bound kernel driver for devices exposing AER.",
    "stacklab_btrfs_device_errors_total": "Persisted Btrfs device errors; collection does not reset counters.",
    "stacklab_btrfs_device_missing": "Whether Btrfs reports the filesystem device missing.",
    "stacklab_hardware_devices": "Number of devices observed or configured for the collector.",
    "stacklab_hardware_collector_success": "Whether the latest component collection succeeded.",
    "stacklab_hardware_collection_timestamp_seconds": "Unix time when the latest collection completed.",
    "stacklab_hardware_collection_duration_seconds": "Duration of the latest hardware collection.",
    "stacklab_nvme_collection_success": "Whether the selected NVMe controller health was readable.",
    "stacklab_nvme_health_passed": "Whether the NVMe controller passed the SMART health check.",
    "stacklab_nvme_critical_warning": "NVMe critical-warning bitmask; zero means no active warning.",
    "stacklab_nvme_available_spare_percent": "NVMe normalized remaining spare capacity percentage.",
    "stacklab_nvme_available_spare_threshold_percent": "NVMe controller spare-capacity warning threshold.",
    "stacklab_nvme_percentage_used": "Manufacturer estimate of NVMe endurance consumed, in percent.",
    "stacklab_nvme_media_errors_total": "Lifetime NVMe media and data-integrity errors.",
    "stacklab_nvme_error_log_entries_total": "Lifetime NVMe error-log entries, including command errors.",
    "stacklab_nvme_unsafe_shutdowns_total": "Lifetime NVMe shutdowns without a shutdown notification.",
    "stacklab_nvme_temperature_celsius": "NVMe composite controller temperature in degrees Celsius.",
}


class Metrics:
    def __init__(self):
        self.lines = []
        self.types = {}

    def add(self, name, value, labels=None, kind="gauge"):
        if not isinstance(value, (int, float)) or not math.isfinite(value):
            raise ValueError(f"Invalid numeric value for {name}")
        if name not in self.types:
            self.lines.append(f"# HELP {name} {HELP[name]}")
            self.lines.append(f"# TYPE {name} {kind}")
            self.types[name] = kind
        elif self.types[name] != kind:
            raise ValueError(f"Conflicting metric type: {name}")
        encoded = ",".join(f'{key}={json.dumps(str(value), ensure_ascii=False)}'
                           for key, value in sorted((labels or {}).items()))
        self.lines.append(name + ("{" + encoded + "}" if encoded else "") + " " + str(int(value) if isinstance(value, bool) else value))

    def render(self):
        return "\n".join(self.lines) + "\n"


def counts(path):
    result = {}
    for line in path.read_text().splitlines():
        key, value = line.split()
        number = int(value)
        if number < 0 or key in result:
            raise ValueError(f"Invalid counter in {path}")
        result[key] = number
    return result


def collect_pcie(metrics, sys_root):
    root = sys_root / "bus/pci/devices"
    if not root.is_dir():
        raise ValueError("PCI sysfs is unavailable")
    devices = 0
    for device in sorted(root.iterdir()):
        present = False
        for severity, suffix, total in [("correctable", "correctable", "TOTAL_ERR_COR"),
                                        ("nonfatal", "nonfatal", "TOTAL_ERR_NONFATAL"),
                                        ("fatal", "fatal", "TOTAL_ERR_FATAL")]:
            path = device / ("aer_dev_" + suffix)
            if not path.exists():
                continue
            values = counts(path)
            metrics.add("stacklab_pcie_errors_total", values[total],
                        {"device": device.name, "severity": severity}, "counter")
            present = True
            if severity == "correctable":
                for error, count in values.items():
                    if error != total:
                        metrics.add("stacklab_pcie_correctable_errors_total", count,
                                    {"device": device.name, "error": error}, "counter")
        if present:
            devices += 1
            driver = (device / "driver").resolve().name if (device / "driver").exists() else "unknown"
            metrics.add("stacklab_pcie_device_info", 1, {"device": device.name, "driver": driver})
    metrics.add("stacklab_hardware_devices", devices, {"collector": "pcie"})


def collect_btrfs(metrics, sys_root):
    root = sys_root / "fs/btrfs"
    devices = 0
    for fs in sorted(root.iterdir()) if root.is_dir() else []:
        if not (fs / "devinfo").is_dir():
            continue
        for device in sorted((fs / "devinfo").iterdir()):
            labels = {"filesystem": fs.name, "device_id": device.name}
            values = counts(device / "error_stats")
            # Missing files/fields are an unsupported or broken collection, not zero errors.
            for field in ["write_errs", "read_errs", "flush_errs", "corruption_errs", "generation_errs"]:
                metrics.add("stacklab_btrfs_device_errors_total", values[field],
                            {**labels, "error": field.removesuffix("_errs")}, "counter")
            missing = int((device / "missing").read_text().strip())
            if missing not in (0, 1):
                raise ValueError("Invalid Btrfs missing-device state")
            metrics.add("stacklab_btrfs_device_missing", missing, labels)
            devices += 1
    metrics.add("stacklab_hardware_devices", devices, {"collector": "btrfs"})


def smart_metrics(metrics, device, data):
    log = data["nvme_smart_health_information_log"]
    labels = {"device": device}
    passed = data["smart_status"]["passed"]
    if type(passed) is not bool:
        raise ValueError("Missing SMART health result")
    fields = {
        "critical_warning": ("critical_warning", "gauge"),
        "available_spare": ("available_spare_percent", "gauge"),
        "available_spare_threshold": ("available_spare_threshold_percent", "gauge"),
        "percentage_used": ("percentage_used", "gauge"),
        "media_errors": ("media_errors_total", "counter"),
        "num_err_log_entries": ("error_log_entries_total", "counter"),
        "unsafe_shutdowns": ("unsafe_shutdowns_total", "counter"),
    }
    values = {}
    for key, (suffix, kind) in fields.items():
        value = log[key]
        if type(value) not in (int, float) or not math.isfinite(value) or value < 0:
            raise ValueError(f"Invalid NVMe {key}")
        values[suffix] = (value, kind)
    metrics.add("stacklab_nvme_health_passed", passed, labels)
    for suffix, (value, kind) in values.items():
        metrics.add("stacklab_nvme_" + suffix, value, labels, kind)
    temperature = data.get("temperature", {}).get("current", log.get("temperature"))
    if type(temperature) in (int, float) and 0 <= temperature <= 150:
        metrics.add("stacklab_nvme_temperature_celsius", temperature, labels)


def collect_nvme(metrics, devices, smartctl="/usr/sbin/smartctl"):
    successful = True
    metrics.add("stacklab_hardware_devices", len(devices), {"collector": "nvme"})
    for device in devices:
        ok = False
        try:
            result = subprocess.run([smartctl, "--json", "--health", "--attributes", device],
                                    capture_output=True, text=True, timeout=10, check=False)
            # Bits 0-2 are read/command errors. Bits 3-7 describe health findings:
            # a failing disk must remain visible with a successful collection.
            if result.returncode < 0 or result.returncode & 7:
                raise ValueError("smartctl could not read NVMe health")
            data = json.loads(result.stdout)
            smart_metrics(metrics, device, data)
            ok = True
        except (OSError, ValueError, KeyError, TypeError, subprocess.TimeoutExpired) as error:
            print(f"NVMe collection failed for {device}: {type(error).__name__}", file=sys.stderr)
        metrics.add("stacklab_nvme_collection_success", ok, {"device": device})
        successful = successful and ok
    return successful


def collect(devices, sys_root=Path("/sys"), smartctl="/usr/sbin/smartctl"):
    metrics = Metrics()
    start = time.monotonic()
    for name, collector in [("pcie", collect_pcie), ("btrfs", collect_btrfs)]:
        part = Metrics()
        part.types.update(metrics.types)
        try:
            collector(part, sys_root)
            metrics.lines.extend(part.lines)
            metrics.types.update(part.types)
            ok = True
        except (OSError, ValueError, KeyError) as error:
            ok = False
            print(f"{name} collection failed: {type(error).__name__}", file=sys.stderr)
        metrics.add("stacklab_hardware_collector_success", ok, {"collector": name})
    metrics.add("stacklab_hardware_collector_success", collect_nvme(metrics, devices, smartctl), {"collector": "nvme"})
    metrics.add("stacklab_hardware_collection_timestamp_seconds", time.time())
    metrics.add("stacklab_hardware_collection_duration_seconds", time.monotonic() - start)
    return metrics.render()


def configuration(path):
    data = json.loads(path.read_text())
    if not isinstance(data, dict) or set(data) != {"nvme_devices"}:
        raise ValueError("Config must contain only nvme_devices")
    devices = data["nvme_devices"]
    if not isinstance(devices, list) or len(devices) > 8 or any(
            not isinstance(d, str) or not re.fullmatch(r"/dev/nvme[0-9]+", d) for d in devices):
        raise ValueError("nvme_devices must list up to eight controller paths, e.g. /dev/nvme0")
    if len(set(devices)) != len(devices):
        raise ValueError("Duplicate NVMe controller")
    return devices


def write_atomic(path, contents):
    # Textfile readers see a whole scrape or its predecessor, never a partial file.
    fd, name = tempfile.mkstemp(prefix=".stacklab-hardware-", dir=path.parent)
    try:
        with os.fdopen(fd, "w") as output:
            os.fchmod(output.fileno(), 0o644)
            output.write(contents)
            output.flush()
            os.fsync(output.fileno())
        os.replace(name, path)
    finally:
        if os.path.exists(name):
            os.unlink(name)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True)
    parser.add_argument("--output", type=Path, help="atomic .prom output; defaults to stdout")
    args = parser.parse_args()
    output = collect(configuration(args.config))
    if args.output:
        write_atomic(args.output, output)
    else:
        print(output, end="")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, TypeError) as error:
        sys.exit(f"Hardware monitoring failed: {type(error).__name__}")
