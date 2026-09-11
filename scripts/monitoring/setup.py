#!/usr/bin/env python3
"""Repeatable monitoring setup for a local Linux Docker/systemd Stacklab host."""

import argparse
import functools
import hashlib
import ipaddress
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import stat
import subprocess
import sys
import tempfile
import time
import urllib.parse

if sys.version_info < (3, 9):
    sys.exit("Monitoring setup requires Python 3.9 or newer.")

try:
    import yaml
except ImportError:
    sys.exit("PyYAML is required; on Debian/Ubuntu install python3-yaml.")

MANAGED = "# Managed by Stacklab monitoring setup.\n"
EXPORTERS = ("node-exporter", "cadvisor")
JOBS = (*EXPORTERS, "stacklab")


def run(*args, capture=False):
    # Do not print command output containing an existing Compose environment.
    result = subprocess.run([str(arg) for arg in args], check=True, text=True,
                            stdout=subprocess.PIPE if capture else None,
                            stderr=subprocess.PIPE if capture else None)
    return result.stdout


@functools.lru_cache(maxsize=1)
def compose_prefix():
    for command in (("docker", "compose"), ("docker-compose",)):
        try:
            version = run(*command, "version", "--short", capture=True).strip().lstrip("v")
            if int(version.split(".")[0]) >= 2:
                return command
        except (OSError, ValueError, subprocess.CalledProcessError):
            continue
    raise ValueError("Docker Compose v2 is required (docker compose or docker-compose)")


def assets_dir():
    here = Path(__file__).resolve().parent
    for candidate in (here / "assets", here.parents[1] / "deploy/monitoring"):
        if (candidate / "stacklab-overview.json").is_file():
            return candidate
    raise ValueError("Monitoring assets are missing from this installation")


def load_yaml(path):
    value = yaml.safe_load(path.read_text())
    if not isinstance(value, dict):
        raise ValueError(f"Expected a YAML mapping: {path}")
    return value


def encoded(value):
    return yaml.safe_dump(value, sort_keys=False, allow_unicode=True).encode()


def address(value):
    parsed = urllib.parse.urlsplit("//" + value)
    if not parsed.hostname or not parsed.port or parsed.path or parsed.username:
        raise ValueError("Listeners must be literal IP:port addresses")
    ipaddress.ip_address(parsed.hostname)
    return parsed.hostname, parsed.port


def absolute(value):
    path = Path(value)
    if not path.is_absolute() or ".." in path.parts or any(c in str(path) for c in '\n\r\t\0%$"\\:'):
        raise ValueError("Paths must be absolute, without traversal or environment substitutions")
    return path


def configuration(raw):
    if not isinstance(raw, dict):
        raise ValueError("Setup JSON must contain an object")
    defaults = dict(schema_version=1, mode="standalone", workspace="/srv/stacklab",
                    state_dir="/var/lib/stacklab", stack="monitoring", host_label="server",
                    metrics_url="http://127.0.0.1:8080/metrics", systemd_unit="stacklab.service",
                    service_user="stacklab", prometheus_gid=65534, network="monitoring",
                    host_gateway="auto", docker_root="auto", grafana_listen="127.0.0.1:3000",
                    prometheus_listen="127.0.0.1:9090", node_exporter_port=9100, cadvisor_port=8081)
    optional = {"compose_file", "prometheus_config", "dashboard_dir"}
    unknown = set(raw) - defaults.keys() - optional
    if unknown:
        raise ValueError("Unknown setup options: " + ", ".join(sorted(unknown)))
    cfg = {**defaults, **raw}
    if cfg["schema_version"] != 1 or cfg["mode"] not in ("standalone", "existing"):
        raise ValueError("Supported schema_version is 1; mode is standalone or existing")
    for key in ("stack", "service_user", "network", "systemd_unit"):
        if not isinstance(cfg[key], str) or not re.fullmatch(r"[a-zA-Z0-9][a-zA-Z0-9_.-]*", cfg[key]):
            raise ValueError(f"Invalid {key}")
    if not cfg["systemd_unit"].endswith(".service"):
        raise ValueError("systemd_unit must end in .service")
    if not isinstance(cfg["host_label"], str) or not cfg["host_label"].strip():
        raise ValueError("host_label must be a nonempty string")
    for key in ("node_exporter_port", "cadvisor_port", "prometheus_gid"):
        if type(cfg[key]) is not int or not 0 <= cfg[key] <= (2**31 - 1 if key == "prometheus_gid" else 65535):
            raise ValueError(f"Invalid {key}")
        if key != "prometheus_gid" and cfg[key] == 0:
            raise ValueError(f"Invalid {key}")
    for key in ("grafana_listen", "prometheus_listen"):
        address(cfg[key])
    url = urllib.parse.urlsplit(cfg["metrics_url"])
    if url.scheme not in ("http", "https") or not url.hostname or url.username or url.password or url.query or url.fragment:
        raise ValueError("metrics_url must be an HTTP(S) URL without credentials, query, or fragment")
    # Validate the port before constructing a scrape job.
    _ = url.port
    for key in ("workspace", "state_dir"):
        cfg[key] = absolute(cfg[key])
    workspace = cfg["workspace"]
    cfg["compose_file"] = absolute(cfg.get("compose_file", workspace / "stacks" / cfg["stack"] / "compose.yaml"))
    cfg["prometheus_config"] = absolute(cfg.get("prometheus_config", workspace / "config" / cfg["stack"] / "prometheus/prometheus.yml"))
    cfg["dashboard_dir"] = absolute(cfg.get("dashboard_dir", workspace / "config" / cfg["stack"] / "grafana/dashboards"))
    cfg["token"] = cfg["state_dir"] / "monitoring/metrics-token"
    if cfg["token"].is_relative_to(workspace):
        raise ValueError("state_dir must keep the token outside the Git workspace")
    cfg["env_file"] = Path("/etc/stacklab") / (cfg["systemd_unit"][:-8] + "-monitoring.env")
    cfg["dropin"] = Path("/etc/systemd/system") / (cfg["systemd_unit"] + ".d/90-monitoring.conf")
    cfg["receipt"] = cfg["state_dir"] / "monitoring/setup-state.json"
    return cfg


def bind(source, target):
    return {"type": "bind", "source": str(source), "target": target, "read_only": True,
            "bind": {"create_host_path": False}}


def merge_mount(service, mount, *, replace=False):
    mounts = service.setdefault("volumes", [])
    for index, old in enumerate(mounts):
        target = old.get("target") if isinstance(old, dict) else old.split(":")[1] if ":" in old else old
        if target == mount["target"]:
            if old != mount:
                if not replace:
                    raise ValueError(f"Conflicting mount at {target}; update the existing configuration explicitly")
                mounts[index] = mount
            return
    mounts.append(mount)


def build_plan(cfg, assets, *, adopt=False):
    existing = cfg["mode"] == "existing"
    compose_path = cfg["compose_file"]
    previous = load_yaml(compose_path) if compose_path.exists() else {}
    marker = previous.get("x-stacklab-monitoring", {})
    managed = marker == {"schema_version": 1, "mode": cfg["mode"]}
    if existing:
        compose = previous
        prom = load_yaml(cfg["prometheus_config"])
        if not {"prometheus", "grafana"} <= compose.get("services", {}).keys():
            raise ValueError("Existing mode requires prometheus and grafana services")
    else:
        if compose_path.exists() and not managed:
            raise ValueError("A stack already exists; use existing mode or a different stack name")
        # Preserve local settings on subsequent runs; exporter/job definitions remain managed.
        compose = previous or load_yaml(assets / "compose.standalone.yaml")
        prom = load_yaml(cfg["prometheus_config"]) if cfg["prometheus_config"].exists() else {
            "global": {"scrape_interval": "15s", "scrape_timeout": "10s", "evaluation_interval": "15s"},
            "scrape_configs": [{"job_name": "prometheus", "static_configs": [{"targets": [cfg["prometheus_listen"]]}]}]}
        compose.setdefault("name", cfg["stack"])
    if compose.get("name", cfg["stack"]) != cfg["stack"]:
        raise ValueError("Configured stack must match the existing Compose project name")
    compose["x-stacklab-monitoring"] = {"schema_version": 1, "mode": cfg["mode"]}
    exporters = load_yaml(assets / "compose.exporters.yaml")["services"]
    node = exporters["node-exporter"]
    cadvisor = exporters["cadvisor"]
    gateway = cfg["host_gateway"]
    if existing and gateway == "auto":
        info = json.loads(run("docker", "network", "inspect", "bridge", capture=True))
        gateway = next(entry["Gateway"] for entry in info[0]["IPAM"]["Config"] if ":" not in entry.get("Gateway", ""))
    if existing:
        ipaddress.ip_address(gateway)
    node_host = gateway if existing else "127.0.0.1"
    node_listen = f'{"[" + node_host + "]" if ":" in node_host else node_host}:{cfg["node_exporter_port"]}'
    node["command"] = [f"--web.listen-address={node_listen}" if flag.startswith("--web.listen-address=") else flag
                       for flag in node["command"]]
    docker_root = cfg["docker_root"]
    if docker_root == "auto":
        docker_root = run("docker", "info", "--format", "{{.DockerRootDir}}", capture=True).strip()
    docker_root = absolute(docker_root)
    cadvisor["volumes"] = [f"{docker_root}:{docker_root}:ro" if mount.startswith("/var/lib/docker:") else mount
                           for mount in cadvisor["volumes"]]
    if existing:
        if cfg["network"] == "default":
            compose.setdefault("networks", {}).setdefault("default", {})
        if cfg["network"] not in compose.get("networks", {}):
            raise ValueError("The exporter network must already exist in the Compose file")
        cadvisor["networks"] = [cfg["network"]]
        prom_networks = compose["services"]["prometheus"].get("networks", ["default"])
        if cfg["network"] not in prom_networks:
            raise ValueError("Prometheus must be attached to the exporter network")
    else:
        cadvisor.pop("networks", None)
        cadvisor["network_mode"] = "host"
        cadvisor["command"] += ["--listen_ip=127.0.0.1", f'--port={cfg["cadvisor_port"]}']
        cadvisor["healthcheck"] = {"test": ["CMD", "wget", "--quiet", "--tries=1", "--spider", f'http://127.0.0.1:{cfg["cadvisor_port"]}/healthz'],
                                   "interval": "30s", "timeout": "5s", "retries": 3}
    for name, service in (("node-exporter", node), ("cadvisor", cadvisor)):
        if name in compose["services"] and not (managed or adopt):
            raise ValueError(f"Service {name} already exists; review it and use --adopt-existing once")
        compose["services"][name] = service
    prometheus = compose["services"]["prometheus"]
    merge_mount(prometheus, bind(cfg["token"], "/run/secrets/stacklab-metrics-token"), replace=managed or adopt)
    url = urllib.parse.urlsplit(cfg["metrics_url"])
    if existing and url.hostname in ("localhost", "127.0.0.1", "::1"):
        raise ValueError("A bridged Prometheus needs a host-reachable metrics_url, not a loopback address")
    if existing:
        hosts = prometheus.setdefault("extra_hosts", [])
        if isinstance(hosts, dict):
            hosts.setdefault("host.docker.internal", "host-gateway")
        elif not any(item.startswith("host.docker.internal:") or item.startswith("host.docker.internal=") for item in hosts):
            hosts.append("host.docker.internal:host-gateway")
    jobs = []
    for name, target in (("node-exporter", node_listen), ("cadvisor", "cadvisor:8080" if existing else f'127.0.0.1:{cfg["cadvisor_port"]}'), ("stacklab", url.netloc)):
        job = {"job_name": name, "static_configs": [{"targets": [target], "labels": {"host": cfg["host_label"]}}]}
        if name == "stacklab":
            job.update(scheme=url.scheme, metrics_path=url.path or "/metrics", authorization={
                "type": "Bearer", "credentials_file": "/run/secrets/stacklab-metrics-token"})
        jobs.append(job)
    old_jobs = prom.setdefault("scrape_configs", [])
    if any(job.get("job_name") in JOBS for job in old_jobs) and not (managed or adopt):
        raise ValueError("Monitoring scrape jobs already exist; review them and use --adopt-existing once")
    prom["scrape_configs"] = [job for job in old_jobs if job.get("job_name") not in JOBS] + jobs
    dashboard = json.loads((assets / "stacklab-overview.json").read_text())
    for variable in dashboard["templating"]["list"]:
        if variable["name"] == "host":
            variable["current"] = {"text": cfg["host_label"], "value": cfg["host_label"]}
    files = {cfg["prometheus_config"]: encoded(prom),
             cfg["dashboard_dir"] / "Homelab/stacklab-overview.json": (json.dumps(dashboard, indent=2, ensure_ascii=False) + "\n").encode()}
    if not existing:
        for job in prom["scrape_configs"]:
            if job.get("job_name") == "prometheus":
                job["static_configs"] = [{"targets": [cfg["prometheus_listen"]]}]
        prometheus["command"] = [f'--web.listen-address={cfg["prometheus_listen"]}' if flag.startswith("--web.listen-address=") else flag
                                 for flag in prometheus["command"]]
        merge_mount(prometheus, bind(cfg["prometheus_config"].parent, "/etc/prometheus"), replace=managed)
        grafana = compose["services"]["grafana"]
        grafana_ip, grafana_port = address(cfg["grafana_listen"])
        grafana["environment"].update(GF_SERVER_HTTP_ADDR=grafana_ip, GF_SERVER_HTTP_PORT=str(grafana_port))
        provisioning = cfg["dashboard_dir"].parent / "provisioning"
        merge_mount(grafana, bind(provisioning, "/etc/grafana/provisioning"), replace=managed)
        merge_mount(grafana, bind(cfg["dashboard_dir"], "/etc/grafana/dashboards"), replace=managed)
        files[provisioning / "datasources/prometheus.yml"] = encoded({"apiVersion": 1, "datasources": [{
            "name": "Prometheus", "uid": "prometheus", "type": "prometheus", "access": "proxy", "isDefault": True,
            "url": "http://" + cfg["prometheus_listen"], "editable": False}]})
        files[provisioning / "dashboards/stacklab.yml"] = encoded({"apiVersion": 1, "providers": [{
            "name": "stacklab", "type": "file", "disableDeletion": True, "allowUiUpdates": False,
            "updateIntervalSeconds": 30, "options": {"path": "/etc/grafana/dashboards", "foldersFromFilesStructure": True}}]})
        # Grafana checks these directories even without alerting/plugin configuration.
        files[provisioning / "alerting/.gitkeep"] = b""
        files[provisioning / "plugins/.gitkeep"] = b""
    files[cfg["prometheus_config"]] = encoded(prom)
    files[compose_path] = encoded(compose)
    files[cfg["env_file"]] = (MANAGED + f'STACKLAB_METRICS_TOKEN_FILE="{cfg["token"]}"\n').encode()
    files[cfg["dropin"]] = (MANAGED + f'[Service]\nEnvironmentFile="{cfg["env_file"]}"\n').encode()
    for path in (cfg["env_file"], cfg["dropin"]):
        if path.exists() and not path.read_text().startswith(MANAGED) and not adopt:
            raise ValueError(f"Unmanaged systemd configuration exists: {path}")
    return files


def assert_regular(path):
    for parent in (path, *path.parents):
        if parent.is_symlink():
            raise ValueError(f"Refusing symbolic link: {parent}")
    if path.exists() and not path.is_file():
        raise ValueError(f"Expected a regular file: {path}")


def write_atomic(path, data, mode, uid, gid):
    path.parent.mkdir(parents=True, exist_ok=True)
    descriptor, temporary = tempfile.mkstemp(prefix=".stacklab-monitoring-", dir=path.parent)
    try:
        with os.fdopen(descriptor, "wb") as output:
            os.fchmod(output.fileno(), mode)
            os.fchown(output.fileno(), uid, gid)
            output.write(data)
            output.flush()
            os.fsync(output.fileno())
        os.replace(temporary, path)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def compose_command(cfg, *args, capture=False):
    return run(*compose_prefix(), "--project-directory", cfg["compose_file"].parent,
               "-p", cfg["stack"], "-f", cfg["compose_file"], *args, capture=capture)


def verify(cfg):
    # Query from Prometheus's network namespace; host.docker.internal may only
    # resolve there. The token never enters command arguments or verification logs.
    prom_url = "http://" + cfg["prometheus_listen"]
    result = json.loads(compose_command(cfg, "exec", "-T", "prometheus", "wget", "-qO-", prom_url + "/api/v1/targets", capture=True))
    healthy = {entry["labels"].get("job") for entry in result["data"]["activeTargets"]
               if entry["health"] == "up" and entry["labels"].get("host") == cfg["host_label"]}
    if not set(JOBS) <= healthy:
        raise ValueError("Waiting for UP scrape targets: " + ", ".join(sorted(set(JOBS) - healthy)))
    query = 'stacklab_build_info{job="stacklab",host=' + json.dumps(cfg["host_label"]) + '}'
    url = prom_url + "/api/v1/query?" + urllib.parse.urlencode({"query": query})
    result = json.loads(compose_command(cfg, "exec", "-T", "prometheus", "wget", "-qO-", url, capture=True))
    if not result.get("data", {}).get("result"):
        raise ValueError("The Stacklab target has not returned application metrics yet")
    if cfg["mode"] == "standalone":
        url = "http://" + cfg["grafana_listen"] + "/api/dashboards/uid/stacklab-overview"
        dashboard = json.loads(compose_command(cfg, "exec", "-T", "prometheus", "wget", "-qO-", url, capture=True))
        if dashboard.get("dashboard", {}).get("uid") != "stacklab-overview":
            raise ValueError("Grafana has not provisioned the Stacklab dashboard yet")


def apply(cfg, files, *, deploy=False):
    if sys.platform != "linux" or os.geteuid() != 0:
        raise ValueError("--apply requires root on the local Linux Docker/systemd host")
    account = pwd.getpwnam(cfg["service_user"])
    actual_user = run("systemctl", "show", cfg["systemd_unit"], "-p", "User", "--value", capture=True).strip()
    if actual_user != cfg["service_user"]:
        raise ValueError("service_user must match the installed systemd unit")
    for path in (*files, cfg["token"], cfg["receipt"]):
        assert_regular(path)
    token = cfg["token"].read_bytes() if cfg["token"].exists() else (secrets.token_hex(32) + "\n").encode()
    token_value = token.strip()
    if not 32 <= len(token_value) <= 256 or any(chr(c).isspace() for c in token_value):
        raise ValueError("Existing metrics token is invalid; it was not replaced")
    files = {**files, cfg["token"]: token}
    files[cfg["receipt"]] = (json.dumps({"schema_version": 1, "stack": cfg["stack"],
        "files": {str(path): hashlib.sha256(data).hexdigest() for path, data in files.items() if path != cfg["token"]}}, indent=2) + "\n").encode()
    def metadata(path):
        if path == cfg["token"]:
            return 0o440, account.pw_uid, cfg["prometheus_gid"]
        if path in (cfg["env_file"], cfg["dropin"], cfg["receipt"]):
            return (0o644 if path == cfg["dropin"] else 0o600), 0, 0
        return 0o644, account.pw_uid, account.pw_gid

    def matches(path, data):
        if not path.exists() or path.read_bytes() != data:
            return False
        info = path.stat()
        return (stat.S_IMODE(info.st_mode), info.st_uid, info.st_gid) == metadata(path)

    changed = {path: data for path, data in files.items() if not matches(path, data)}
    # Check Compose syntax before touching the host configuration.
    with tempfile.TemporaryDirectory(prefix="stacklab-monitoring-check-") as temporary:
        candidate = Path(temporary) / "compose.yaml"
        candidate.write_bytes(files[cfg["compose_file"]])
        run(*compose_prefix(), "--project-directory", cfg["compose_file"].parent,
            "-p", cfg["stack"], "-f", candidate, "config", "--quiet")
    if not changed:
        print("Configuration and token already match; no files or service settings changed.")
        if deploy:
            deploy_stack(cfg)
        return
    backup = cfg["state_dir"] / "monitoring/backups" / (time.strftime("%Y%m%dT%H%M%SZ", time.gmtime()) + f"-{time.time_ns() % 1000000000:09d}")
    backup.mkdir(parents=True, mode=0o700, exist_ok=False)
    # Docker resolves the single-file bind as root; Stacklab must traverse the host parent.
    os.chown(cfg["token"].parent, account.pw_uid, account.pw_gid)
    os.chmod(cfg["token"].parent, 0o750)
    os.chmod(backup.parent, 0o700)
    snapshots = {}
    for index, path in enumerate(changed):
        if path.exists():
            destination = backup / str(index)
            run("cp", "-a", path, destination)
            snapshots[path] = destination
        else:
            snapshots[path] = None
    (backup / "restore.json").write_text(json.dumps({str(path): str(old) if old else None for path, old in snapshots.items()}, indent=2) + "\n")
    print(f"Backup: {backup}", flush=True)
    restart = any(path in changed for path in (cfg["token"], cfg["env_file"], cfg["dropin"]))
    try:
        for path, data in changed.items():
            mode, uid, gid = metadata(path)
            # Newly created workspace parents must be editable by Stacklab.
            missing = []
            parent = path.parent
            while not parent.exists():
                missing.append(parent)
                parent = parent.parent
            for parent in reversed(missing):
                parent.mkdir(mode=0o755 if parent.is_relative_to(cfg["workspace"]) else 0o750)
                if parent.is_relative_to(cfg["workspace"]) or parent == cfg["token"].parent:
                    os.chown(parent, account.pw_uid, account.pw_gid)
            write_atomic(path, data, mode, uid, gid)
        if deploy:
            compose_command(cfg, "run", "--rm", "--no-deps", "-T", "--entrypoint", "promtool",
                            "prometheus", "check", "config", "/etc/prometheus/prometheus.yml")
        if restart:
            run("systemctl", "daemon-reload")
            run("systemctl", "restart", cfg["systemd_unit"])
        if deploy:
            recreate = []
            if cfg["prometheus_config"] in changed or cfg["token"] in changed:
                recreate.append("prometheus")
            if cfg["mode"] == "standalone" and any("provisioning" in path.parts for path in changed):
                recreate.append("grafana")
            deploy_stack(cfg, recreate=recreate)
    except BaseException:
        # Keep all data volumes and a durable recovery map even if runtime recovery fails.
        for path, old in snapshots.items():
            if old:
                run("cp", "-a", old, path)
            else:
                path.unlink(missing_ok=True)
        run("systemctl", "daemon-reload")
        if restart:
            run("systemctl", "restart", cfg["systemd_unit"])
        print(f"Files restored. Backup: {backup}. Reconcile the monitoring containers with the restored Compose file.", file=sys.stderr)
        raise
    print("Configuration applied. Commit the generated workspace files and your setup JSON; keep state_dir outside Git.")


def deploy_stack(cfg, *, recreate=()):
    # Existing Prometheus/Grafana images are retained. Pull only missing images.
    services = ["prometheus", *EXPORTERS] + (["grafana"] if cfg["mode"] == "standalone" else [])
    if recreate:
        compose_command(cfg, "up", "-d", "--pull", "missing", "--force-recreate", "--no-deps", *recreate)
    compose_command(cfg, "up", "-d", "--pull", "missing", *services)
    compose_command(cfg, "exec", "-T", "prometheus", "promtool", "check", "config", "/etc/prometheus/prometheus.yml")
    deadline = time.monotonic() + 120
    while True:
        try:
            verify(cfg)
            print("Verified: Stacklab bearer authentication and all three Prometheus targets UP.")
            return
        except (OSError, ValueError, subprocess.CalledProcessError):
            if time.monotonic() >= deadline:
                raise
            print("Waiting for monitoring targets...", flush=True)
            time.sleep(5)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", type=Path, required=True, help="Git-safe setup JSON; see setup.example.json")
    parser.add_argument("--apply", action="store_true", help="write configuration, preserve/create the token, and enable metrics")
    parser.add_argument("--deploy", action="store_true", help="also start/reconcile containers and verify scrapes (requires --apply)")
    parser.add_argument("--verify", action="store_true", help="only verify the running integration")
    parser.add_argument("--adopt-existing", action="store_true", help="explicitly adopt existing exporter services and scrape jobs")
    args = parser.parse_args()
    if args.deploy and not args.apply or args.verify and (args.apply or args.deploy):
        parser.error("--deploy requires --apply; --verify is read-only")
    cfg = configuration(json.loads(args.config.read_text()))
    if args.verify:
        verify(cfg)
        print("Monitoring targets are UP.")
        return
    files = build_plan(cfg, assets_dir(), adopt=args.adopt_existing)
    print(f'Mode: {cfg["mode"]}; Compose project: {cfg["stack"]}; metrics URL: {cfg["metrics_url"]}')
    for path, data in files.items():
        print(("KEEP  " if path.exists() and path.read_bytes() == data else "WRITE ") + str(path))
    print(f'Token: {cfg["token"]} (existing value is preserved; a missing token is generated on apply)')
    if args.apply:
        apply(cfg, files, deploy=args.deploy)
    else:
        print("Preview only. Use --apply to configure, or --apply --deploy to configure and start monitoring.")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, KeyError, StopIteration, subprocess.CalledProcessError, yaml.YAMLError) as error:
        sys.exit(f"Monitoring setup failed: {error}")
