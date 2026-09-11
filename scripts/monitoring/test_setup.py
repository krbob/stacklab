"""Migration, preservation, and host-file lifecycle checks for monitoring setup."""

import importlib.util
import json
import os
from pathlib import Path
import pwd
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

import yaml

SPEC = importlib.util.spec_from_file_location("monitoring_setup", Path(__file__).with_name("setup.py"))
setup = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(setup)
ASSETS = setup.assets_dir()


class SetupTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="stacklab-monitoring-test-")
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name).resolve()
        self.raw = {"workspace": str(self.root / "workspace"), "state_dir": str(self.root / "state"),
                    "docker_root": "/var/lib/docker", "host_gateway": "172.30.0.1"}
        self.cfg = self.config()

    def config(self, **options):
        cfg = setup.configuration({**self.raw, **options})
        cfg["env_file"] = self.root / "etc/stacklab-monitoring.env"
        cfg["dropin"] = self.root / "etc/systemd/90-monitoring.conf"
        return cfg

    def persist(self, files):
        for path, data in files.items():
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_bytes(data)

    def existing(self):
        self.cfg = self.config(mode="existing", metrics_url="http://172.30.0.1:8080/metrics")
        compose = {"name": "monitoring", "services": {
            "prometheus": {"image": "prom/prometheus:existing", "networks": ["monitoring"],
                           "command": ["--storage.tsdb.retention.time=90d"], "volumes": ["history:/prometheus"]},
            "grafana": {"image": "grafana/grafana:existing", "environment": {"GF_AUTH_ANONYMOUS_ENABLED": "false"},
                        "volumes": ["grafana_state:/var/lib/grafana"], "labels": {"custom.routing": "preserve"}},
            "alertmanager": {"image": "example/alertmanager:existing"}},
            "networks": {"monitoring": {}, "proxy": {"external": True}},
            "volumes": {"history": {"name": "old-history"}, "grafana_state": {}}}
        prom = {"global": {"scrape_interval": "30s"}, "rule_files": ["/etc/prometheus/rules/*.yml"],
                "alerting": {"alertmanagers": [{"static_configs": [{"targets": ["alertmanager:9093"]}]}]},
                "scrape_configs": [{"job_name": "stock", "static_configs": [{"targets": ["stock:8080"]}]}]}
        self.persist({self.cfg["compose_file"]: setup.encoded(compose), self.cfg["prometheus_config"]: setup.encoded(prom)})
        return compose, prom

    def test_preview_is_read_only_and_standalone_works_with_loopback(self):
        files = setup.build_plan(self.cfg, ASSETS)
        self.assertFalse(self.cfg["workspace"].exists())
        self.assertNotIn(self.cfg["token"], files)
        model = yaml.safe_load(files[self.cfg["compose_file"]])
        for service in model["services"].values():
            self.assertEqual(service["network_mode"], "host")
            self.assertNotIn("ports", service)
        self.assertEqual(model["services"]["grafana"]["environment"]["GF_SERVER_HTTP_ADDR"], "127.0.0.1")
        jobs = yaml.safe_load(files[self.cfg["prometheus_config"]])["scrape_configs"]
        self.assertEqual(jobs[-1]["static_configs"][0]["targets"], ["127.0.0.1:8080"])

    def test_repeat_and_git_only_migration_preserve_config_without_secret_state(self):
        first = setup.build_plan(self.cfg, ASSETS)
        self.persist(first)
        self.assertEqual(first, setup.build_plan(self.cfg, ASSETS))
        self.assertFalse(self.cfg["receipt"].exists())
        self.assertFalse(self.cfg["token"].exists())
        moved = self.root / "migrated-workspace"
        shutil.copytree(self.cfg["workspace"], moved)
        migrated = self.config(workspace=str(moved), state_dir=str(self.root / "new-state"),
                               host_label="new-server", prometheus_listen="127.0.0.1:19090")
        files = setup.build_plan(migrated, ASSETS)
        compose = yaml.safe_load(files[migrated["compose_file"]])
        mounts = compose["services"]["prometheus"]["volumes"]
        self.assertIn(setup.bind(migrated["token"], "/run/secrets/stacklab-metrics-token"), mounts)
        jobs = yaml.safe_load(files[migrated["prometheus_config"]])["scrape_configs"]
        self.assertEqual(jobs[0]["static_configs"][0]["targets"], ["127.0.0.1:19090"])
        self.assertEqual(jobs[-1]["static_configs"][0]["labels"]["host"], "new-server")

    def test_existing_stack_preserves_unrelated_services_volumes_jobs_and_alerts(self):
        before_compose, before_prom = self.existing()
        files = setup.build_plan(self.cfg, ASSETS)
        after_compose = yaml.safe_load(files[self.cfg["compose_file"]])
        after_prom = yaml.safe_load(files[self.cfg["prometheus_config"]])
        for key in ("networks", "volumes"):
            self.assertEqual(before_compose[key], after_compose[key])
        for name in ("grafana", "alertmanager"):
            self.assertEqual(before_compose["services"][name], after_compose["services"][name])
        self.assertEqual(before_compose["services"]["prometheus"]["command"], after_compose["services"]["prometheus"]["command"])
        for key in ("global", "rule_files", "alerting"):
            self.assertEqual(before_prom[key], after_prom[key])
        self.assertEqual(before_prom["scrape_configs"][0], after_prom["scrape_configs"][0])
        self.persist(files)
        self.assertEqual(files, setup.build_plan(self.cfg, ASSETS))

    def test_conflicting_jobs_and_exporters_require_explicit_adoption(self):
        compose, prom = self.existing()
        prom["scrape_configs"].append({"job_name": "stacklab", "static_configs": []})
        self.cfg["prometheus_config"].write_bytes(setup.encoded(prom))
        with self.assertRaisesRegex(ValueError, "adopt-existing"):
            setup.build_plan(self.cfg, ASSETS)
        compose["services"]["cadvisor"] = {"image": "custom/cadvisor:old"}
        self.cfg["compose_file"].write_bytes(setup.encoded(compose))
        with self.assertRaisesRegex(ValueError, "adopt-existing"):
            setup.build_plan(self.cfg, ASSETS)
        files = setup.build_plan(self.cfg, ASSETS, adopt=True)
        self.assertEqual(len([j for j in yaml.safe_load(files[self.cfg["prometheus_config"]])["scrape_configs"] if j["job_name"] == "stacklab"]), 1)

    def test_rejects_invalid_and_secret_bearing_configuration(self):
        for change in ({"metrics_url": "http://user:password@example.test/metrics"},
                       {"metrics_url": "http://example.test/metrics?token=example"},
                       {"state_dir": str(self.cfg["workspace"] / "secret")},
                       {"workspace": '/tmp/quote"injection'}, {"systemd_unit": "../other.service"},
                       {"grafana_listen": "localhost:3000"}, {"node_exporter_port": 0},
                       {"unexpected": True}):
            with self.subTest(change=change), self.assertRaises(ValueError):
                setup.configuration({**self.raw, **change})

    def test_existing_mode_rejects_unreachable_loopback(self):
        self.existing()
        self.cfg["metrics_url"] = "http://127.0.0.1:8080/metrics"
        with self.assertRaisesRegex(ValueError, "loopback"):
            setup.build_plan(self.cfg, ASSETS)

    def test_verification_uses_prometheus_network_without_reading_token(self):
        cfg = self.config(mode="existing", metrics_url="http://host.docker.internal:8080/metrics")
        calls = []

        def query(_cfg, *args, **kwargs):
            calls.append(args)
            if args[-1].endswith("/targets"):
                return json.dumps({"data": {"activeTargets": [
                    {"health": "up", "labels": {"job": job, "host": cfg["host_label"]}} for job in setup.JOBS]}})
            return json.dumps({"data": {"result": [{"metric": {"__name__": "stacklab_build_info"}}]}})

        with patch.object(setup, "compose_command", side_effect=query):
            setup.verify(cfg)
        self.assertFalse(cfg["token"].exists())
        self.assertTrue(all(call[:3] == ("exec", "-T", "prometheus") for call in calls))
        self.assertNotIn("Authorization", str(calls))

    def test_verification_rejects_exporters_from_another_host(self):
        response = json.dumps({"data": {"activeTargets": [
            {"health": "up", "labels": {"job": job, "host": "different-host"}} for job in setup.JOBS]}})
        with patch.object(setup, "compose_command", return_value=response), self.assertRaisesRegex(ValueError, "Waiting for UP"):
            setup.verify(self.cfg)

    @unittest.skipUnless(shutil.which("docker"), "Docker Compose CLI is unavailable")
    def test_generated_compose_is_valid(self):
        files = setup.build_plan(self.cfg, ASSETS)
        candidate = self.root / "compose.yaml"
        candidate.write_bytes(files[self.cfg["compose_file"]])
        subprocess.run([*setup.compose_prefix(), "--project-directory", str(self.cfg["compose_file"].parent),
                        "-f", str(candidate), "config", "--quiet"], check=True, capture_output=True)


@unittest.skipUnless(sys.platform == "linux" and os.geteuid() == 0, "host-file lifecycle checks run as root in an isolated Linux test environment")
class HostLifecycleTests(SetupTests):
    def setUp(self):
        super().setUp()
        self.cfg = self.config(service_user="daemon")
        self.calls = []

    def fake_run(self, *args, **kwargs):
        self.calls.append(args)
        if args[-2:] == ("version", "--short"):
            return "2.26.1\n"
        if args[:2] == ("systemctl", "show"):
            return self.cfg["service_user"] + "\n"
        if args[0] == "cp":
            shutil.copy2(args[-2], args[-1])
        return ""

    def test_apply_twice_preserves_token_inode_and_does_not_restart(self):
        with patch.object(setup, "run", side_effect=self.fake_run):
            setup.apply(self.cfg, setup.build_plan(self.cfg, ASSETS))
            token = self.cfg["token"].read_bytes()
            inode = self.cfg["token"].stat().st_ino
            self.calls.clear()
            setup.apply(self.cfg, setup.build_plan(self.cfg, ASSETS))
        self.assertEqual(token, self.cfg["token"].read_bytes())
        self.assertEqual(inode, self.cfg["token"].stat().st_ino)
        self.assertEqual(self.cfg["token"].stat().st_mode & 0o777, 0o440)
        self.assertEqual(self.cfg["token"].stat().st_uid, pwd.getpwnam("daemon").pw_uid)
        self.assertEqual(self.cfg["token"].stat().st_gid, 65534)
        self.assertFalse(any(call[:2] == ("systemctl", "restart") for call in self.calls))
        for path in self.cfg["workspace"].rglob("*"):
            if path.is_file():
                self.assertNotIn(token.strip(), path.read_bytes())

    def test_failed_restart_restores_existing_files_and_removes_new_secret(self):
        files = setup.build_plan(self.cfg, ASSETS)
        old_env = (setup.MANAGED + "OLD=preserved\n").encode()
        self.persist({self.cfg["env_file"]: old_env})
        failed = False

        def restart_failure(*args, **kwargs):
            nonlocal failed
            if args[:2] == ("systemctl", "restart") and not failed:
                failed = True
                raise subprocess.CalledProcessError(1, args)
            return self.fake_run(*args, **kwargs)

        with patch.object(setup, "run", side_effect=restart_failure), self.assertRaises(subprocess.CalledProcessError):
            setup.apply(self.cfg, files)
        self.assertEqual(self.cfg["env_file"].read_bytes(), old_env)
        self.assertFalse(self.cfg["token"].exists())
        self.assertFalse(self.cfg["compose_file"].exists())
        self.assertEqual(len(list((self.cfg["state_dir"] / "monitoring/backups").glob("*/restore.json"))), 1)

    def test_token_symlink_is_rejected_before_any_write(self):
        files = setup.build_plan(self.cfg, ASSETS)
        victim = self.root / "untouched"
        victim.write_text("preserve me")
        self.cfg["token"].parent.mkdir(parents=True)
        self.cfg["token"].symlink_to(victim)
        with patch.object(setup, "run", side_effect=self.fake_run), self.assertRaisesRegex(ValueError, "symbolic link"):
            setup.apply(self.cfg, files)
        self.assertEqual(victim.read_text(), "preserve me")
        self.assertFalse(self.cfg["compose_file"].exists())


if __name__ == "__main__":
    unittest.main()
