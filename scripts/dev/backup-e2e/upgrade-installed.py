#!/usr/bin/env python3
"""Exercise an installed .deb upgrade on a disposable Stacklab VM.

Unlike isolated-linux.py this intentionally upgrades the running application and
briefly installs a legacy sudoers fixture. It preserves the complete prior state,
helper spool, configuration and installed application in a root-only archive.
A supplied password (or existing bootstrap credential) is used only in memory;
authentication is not reset. Sessions are revoked unless explicitly exported.
Run explicitly as root with a fresh /root/stacklab-backup-upgrade-<id>.
"""

import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.request


def command(args, **kwargs):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, **kwargs)


def sha256(path):
    digest = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--deb", required=True)
    parser.add_argument("--backup-dir", required=True)
    parser.add_argument("--backup-test", required=True)
    parser.add_argument("--base-url", default="http://127.0.0.1:8080")
    credentials = parser.add_mutually_exclusive_group()
    credentials.add_argument("--password-file", help="private file containing the current login password")
    credentials.add_argument("--password-stdin", action="store_true", help="read the current login password from stdin")
    parser.add_argument("--export-browser-session", action="store_true", help="retain a private browser-auth.json session for a subsequent browser check")
    parser.add_argument("--browser-host")
    args = parser.parse_args()
    if args.export_browser_session and not args.browser_host:
        parser.error("--export-browser-session requires --browser-host")
    backup = Path(args.backup_dir)
    if os.geteuid() != 0 or not re.fullmatch(r"/root/stacklab-backup-upgrade-[a-z0-9-]{1,48}", str(backup)):
        raise RuntimeError("requires root and a fresh /root/stacklab-backup-upgrade-<id>")
    if backup.exists() or backup.is_symlink():
        raise RuntimeError("backup directory exists; inspect it before choosing a fresh ID")
    backup.mkdir(mode=0o700)
    config = Path("/etc/stacklab/stacklab.env")
    sudoers = Path("/etc/sudoers.d/stacklab-backup")
    package = Path(args.deb)
    settings = {}
    for line in config.read_text().splitlines():
        if "=" in line and not line.lstrip().startswith("#"):
            key, value = line.split("=", 1)
            settings[key.strip()] = value.strip().strip("\"'")
    if args.password_file:
        secret = Path(args.password_file)
        if secret.is_symlink() or not secret.is_file() or secret.stat().st_mode & 0o077:
            raise RuntimeError("password file must be a private regular file, not a symlink")
        password = secret.read_text().rstrip("\r\n")
    elif args.password_stdin:
        password = sys.stdin.readline().rstrip("\r\n")
    else:
        password = settings.get("STACKLAB_BOOTSTRAP_PASSWORD")
    if not password:
        raise RuntimeError("provide the current login password using --password-file or --password-stdin")
    jar = http.cookiejar.CookieJar()
    api = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))

    def request(path, payload=None):
        data = json.dumps(payload).encode() if payload is not None else None
        req = urllib.request.Request(args.base_url + path, data=data,
                                     headers={"Content-Type": "application/json", "Origin": args.base_url})
        with api.open(req, timeout=15) as response:
            body = response.read()
            return json.loads(body) if body else None

    def login():
        if len(jar):
            request("/api/auth/logout", {})
            jar.clear()
        result = request("/api/auth/login", {"password": password})
        if result.get("authenticated") is not True:
            raise RuntimeError("existing configured credential did not authenticate; password was not changed")

    def ready():
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            try:
                if request("/api/ready").get("status") == "ok":
                    return
            except (OSError, urllib.error.HTTPError):
                pass
            time.sleep(1)
        raise RuntimeError("upgraded application did not become ready")

    def install_sudoers(source):
        temporary = sudoers.with_name("stacklab-backup-upgrade-candidate")
        shutil.copyfile(source, temporary)
        temporary.chmod(0o440)
        try:
            command(["visudo", "-cf", str(temporary)])
            temporary.replace(sudoers)
        finally:
            temporary.unlink(missing_ok=True)

    login()
    before = request("/api/backups")
    policy_before = request("/api/backups/policy")
    report = {"package_sha256": sha256(package), "checks": [], "succeeded": False}
    report["source_version"] = command(["dpkg-query", "-W", "-f=${Version}", "stacklab"]).stdout.decode()
    config_hash = sha256(config)
    original_sudoers = backup / "original.sudoers"
    shutil.copy2(sudoers, original_sudoers)
    original_sudoers.chmod(0o400)
    (backup / "overview-before.json").write_text(json.dumps(before))
    (backup / "policy-before.json").write_text(json.dumps(policy_before))
    needs_restart = False
    legacy_installed = False
    try:
        command(["systemctl", "stop", "stacklab"])
        needs_restart = True
        archive_paths = ["etc/stacklab", "var/lib/stacklab", "var/lib/stacklab-backup-helper",
                         "etc/sudoers.d/stacklab-backup", "usr/lib/stacklab"]
        command(["tar", "--acls", "--xattrs", "--numeric-owner", "-C", "/", "-cpf",
                 str(backup / "before.tar"), *archive_paths])
        (backup / "before.tar").chmod(0o400)
        legacy = backup / "legacy.sudoers"
        helper = "/usr/lib/stacklab/bin/stacklab-backup-helper"
        legacy.write_text(f"stacklab ALL=(root) NOPASSWD: {helper} archive *, {helper} restore *, {helper} check-target *, {helper} service-config\n")
        install_sudoers(legacy)
        legacy_installed = True
        installed = command(["dpkg", "--force-confold", "-i", str(package)],
                            env={**os.environ, "DEBIAN_FRONTEND": "noninteractive"})
        (backup / "dpkg.log").write_bytes(installed.stdout + installed.stderr)
        command(["systemctl", "start", "stacklab"])
        needs_restart = False
        ready()
        login()
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            with_legacy = request("/api/backups")
            if with_legacy.get("helper_ready") is False and "sudoers" in with_legacy.get("helper_error", ""):
                break
            time.sleep(0.2)
        else:
            raise AssertionError("legacy sudoers was not reported as degraded recovery readiness")
        if b"backup sudoers may predate" not in installed.stderr:
            raise AssertionError("package upgrade did not warn about legacy sudoers")
        report["checks"].append({"name": "package_warns_about_legacy_sudoers", "result": "passed"})
        report["checks"].append({"name": "legacy_sudoers_detected", "result": "passed", "helper_ready": False})
        example = Path("/usr/share/stacklab/examples/stacklab-backup.sudoers.example")
        install_sudoers(example)
        legacy_installed = False
        deadline = time.monotonic() + 100
        while time.monotonic() < deadline:
            after = request("/api/backups")
            if after.get("helper_ready") is True:
                break
            time.sleep(2)
        else:
            raise AssertionError("periodic helper readiness did not recover after sudoers update")
        report["checks"].append({"name": "current_sudoers_detected_without_restart", "result": "passed", "helper_ready": True})
        if sha256(config) != config_hash:
            raise AssertionError("upgrade changed existing service configuration")
        for key in ("plans", "destinations"):
            if {item["id"] for item in before[key]} != {item["id"] for item in after[key]}:
                raise AssertionError("upgrade changed existing " + key)
        policy_after = request("/api/backups/policy")
        if (policy_before["paused"], policy_before["host_review_required"]) != (policy_after["paused"], policy_after["host_review_required"]):
            raise AssertionError("ordinary upgrade changed operator pause or host-review requirement")
        report["checks"].append({"name": "existing_config_plans_destinations_policy_preserved", "result": "passed",
                                "plans": len(after["plans"]), "destinations": len(after["destinations"])})
        protected = command(["runuser", "-u", "stacklab", "--", "env", "STACKLAB_TEST_BACKUP_HELPER=" + helper,
                             args.backup_test, "-test.v", "-test.run=^TestNativeProtectedServiceConfigurationRecovery$", "-test.timeout=2m"])
        (backup / "protected-configuration.log").write_bytes(protected.stdout + protected.stderr)
        if b"--- PASS: TestNativeProtectedServiceConfigurationRecovery" not in protected.stdout:
            raise AssertionError("protected configuration test did not execute")
        report["checks"].append({"name": "installed_protected_configuration_recovery", "result": "passed"})
        report["target_version"] = command(["dpkg-query", "-W", "-f=${Version}", "stacklab"]).stdout.decode()
        report["succeeded"] = True
        if args.export_browser_session:
            cookies = [{"name": cookie.name, "value": cookie.value, "domain": args.browser_host,
                        "path": cookie.path, "expires": cookie.expires or -1, "httpOnly": True,
                        "secure": cookie.secure, "sameSite": "Lax"} for cookie in jar]
            auth_state = backup / "browser-auth.json"
            auth_state.write_text(json.dumps({"cookies": cookies, "origins": []}))
            auth_state.chmod(0o600)
        print(json.dumps(report), flush=True)
    finally:
        if legacy_installed:
            install_sudoers(original_sudoers)
        if needs_restart:
            command(["systemctl", "start", "stacklab"])
        if len(jar) and not (args.export_browser_session and report["succeeded"]):
            try:
                request("/api/auth/logout", {})
                jar.clear()
            except (OSError, urllib.error.HTTPError):
                print("Session cleanup failed; revoke test sessions after recovering the service.", file=sys.stderr)
        result = backup / "result.json"
        result.write_text(json.dumps(report, indent=2) + "\n")
        result.chmod(0o600)


if __name__ == "__main__":
    try:
        main()
    except subprocess.CalledProcessError as error:
        detail = (error.stderr or b"").decode("utf-8", errors="replace")[:2000]
        raise RuntimeError(f"upgrade exercise command failed with exit {error.returncode}: {detail}") from None
