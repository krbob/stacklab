#!/usr/bin/env python3
"""Destructive backup fixture restricted to one new, explicitly named directory.

Run as root on a disposable Linux Docker host. Does not read or change the
installed Stacklab configuration, journals, repositories or existing containers.
The helper must be compiled with configurationPath and restoreSpoolRoot pointing
inside --work-dir; run-linux.sh does that. No network port is published.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import time


def run(args, **kwargs):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE,
                          stderr=subprocess.PIPE, **kwargs)


def digest(path):
    value = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def exercise(args):
    work = Path(args.work_dir)
    if os.geteuid() != 0:
        raise RuntimeError("run as root on an isolated Linux test host")
    if not re.fullmatch(r"/var/lib/stacklab-backup-e2e-[a-z0-9-]{1,48}", str(work)):
        raise RuntimeError("work directory must be /var/lib/stacklab-backup-e2e-<unique-id>")
    if work.exists() or work.is_symlink():
        raise RuntimeError("work directory already exists; inspect it before choosing a new run ID")
    work.mkdir(mode=0o700)
    helper = work / "helper"
    shutil.copyfile(args.helper, helper)
    helper.chmod(0o700)
    managed = work / "managed"
    data = managed / "data"
    data.mkdir(parents=True)
    (work / "stacklab.env").write_text(
        f"STACKLAB_ROOT={managed}\nSTACKLAB_DATA_DIR={work / 'state'}\n", encoding="utf-8")
    (work / "stacklab.env").chmod(0o600)
    (work / "helper-spool").mkdir(mode=0o700)
    container = work.name + "-postgres"
    evidence = {"workspace": str(work), "helper_sha256": digest(helper), "checks": []}
    created_container = False
    succeeded = False

    def check(name, **details):
        evidence["checks"].append({"name": name, "result": "passed", **details})
        print(json.dumps({"check": name, "result": "passed"}), flush=True)

    def helper_run(action, *options, **kwargs):
        return run([str(helper), action, *map(str, options)], **kwargs)

    def prepare(archive, target, scope="complete", mode="replace"):
        with archive.open("rb") as stream:
            return helper_run("prepare-restore", "--path", target, "--size", archive.stat().st_size,
                              "--mode", mode, "--source-scope", scope, stdin=stream).stdout.decode().strip()

    def archive(source, destination, *extra):
        with destination.open("wb") as stream:
            subprocess.run([str(helper), "archive", "--path", str(source), *extra],
                           check=True, stdout=stream, stderr=subprocess.PIPE)

    def sql(query):
        return run(["docker", "exec", container, "psql", "-U", "postgres", "-d", "fixture",
                    "-v", "ON_ERROR_STOP=1", "-Atc", query]).stdout.decode().strip()

    def start_pg():
        run(["docker", "start", container])
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            # The image starts a temporary server during initdb. It can answer
            # pg_isready immediately before the entrypoint shuts it down; wait
            # until the entrypoint has exec'd the final server as PID 1.
            identity = subprocess.run(["docker", "exec", container, "cat", "/proc/1/comm"],
                                      stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
            if identity.returncode == 0 and identity.stdout.strip() == b"postgres":
                ready = subprocess.run(["docker", "exec", container, "pg_isready", "-U", "postgres"],
                                       stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                if ready.returncode == 0:
                    return
            time.sleep(0.2)
        raise RuntimeError("fixture PostgreSQL did not become ready")

    try:
        for action in ("prepare-restore", "check-restore", "check-restores", "apply-restore", "release-restore", "check-target"):
            probe = json.loads(helper_run(action, "--probe").stdout)
            assert probe == {"protocol": 2, "action": action}, probe
        check("helper_protocol_and_all_required_actions")

        acl_source = data / "acl-source"
        acl_source.mkdir()
        run(["setfacl", "-m", "u:1000:rwx,d:u:1000:rwx", str(acl_source)])
        readonly = acl_source / "readonly"
        readonly.write_text("read-only ACL payload")
        run(["setfattr", "-n", "user.stacklab-test", "-v", "preserved", str(readonly)])
        os.link(readonly, acl_source / "hardlink")
        readonly.chmod(0o444)
        closed = acl_source / "closed"
        closed.mkdir(mode=0o000)
        acl_archive = work / "acl.tar"
        archive(acl_source, acl_archive)
        acl_target = data / "acl-target"
        acl_target.mkdir()
        (acl_target / "newer").write_text("removed by replacement")
        acl_token = prepare(acl_archive, acl_target)
        helper_run("apply-restore", "--token", acl_token)
        assert (acl_target / "readonly").read_text() == "read-only ACL payload"
        assert not (acl_target / "newer").exists()
        assert (acl_target / "readonly").stat().st_ino == (acl_target / "hardlink").stat().st_ino
        for relative in (".", "readonly", "closed"):
            original, recovered = acl_source / relative, acl_target / relative
            assert original.stat().st_mode == recovered.stat().st_mode
            assert run(["getfacl", "-cn", str(original)]).stdout == run(["getfacl", "-cn", str(recovered)]).stdout
        assert os.getxattr(acl_target / "readonly", "user.stacklab-test") == b"preserved"
        helper_run("release-restore", "--token", acl_token)
        check("replacement_preserves_readonly_acl_xattrs_hardlinks_and_zero_mode_directory")

        pg = data / "postgres"
        pg.mkdir()
        run(["docker", "create", "--name", container, "--label", "stacklab.backup-e2e=" + work.name,
             "--mount", f"type=bind,src={pg},dst=/var/lib/postgresql/data",
             "--env", "POSTGRES_HOST_AUTH_METHOD=trust", "--env", "POSTGRES_DB=fixture", args.postgres_image])
        created_container = True
        start_pg()
        sql("CREATE TABLE items(id integer PRIMARY KEY, value text); INSERT INTO items SELECT n, md5(n::text) FROM generate_series(1,1000) n;")
        expected = sql("SELECT count(*) || ':' || md5(string_agg(value, ',' ORDER BY id)) FROM items;")
        run(["docker", "stop", "--time", "30", container])
        pg_archive = work / "postgres.tar"
        archive(pg, pg_archive)
        original_inode = pg.stat().st_ino
        start_pg()
        sql("INSERT INTO items SELECT n, md5(n::text) FROM generate_series(1001,1500) n; CREATE TABLE newer(id integer);")
        run(["docker", "stop", "--time", "30", container])
        (pg / "newer.marker").write_text("must disappear during exact replacement")
        token = prepare(pg_archive, pg)
        helper_run("check-restore", "--token", token)
        helper_run("apply-restore", "--token", token)
        assert pg.stat().st_ino == original_inode
        assert not (pg / "newer.marker").exists()
        start_pg()
        assert sql("SELECT count(*) || ':' || md5(string_agg(value, ',' ORDER BY id)) FROM items;") == expected
        assert sql("SELECT to_regclass('public.newer') IS NULL;") == "t"
        run(["docker", "stop", "--time", "30", container])
        helper_run("release-restore", "--token", token)
        check("postgres_exact_replacement", captured_rows=1000, removed_rows=500,
              removed_newer_table=True, target_inode_preserved=True)

        filtered = data / "filtered"
        filtered.mkdir()
        (filtered / "included.txt").write_text("captured")
        (filtered / "excluded.bin").write_bytes(os.urandom(8192))
        excluded_hash = digest(filtered / "excluded.bin")
        filtered_archive = work / "filtered.tar"
        archive(filtered, filtered_archive, "--exclude", "excluded.bin")
        for scope in ("filtered", ""):
            try:
                prepare(filtered_archive, filtered, scope=scope)
            except subprocess.CalledProcessError:
                pass
            else:
                raise AssertionError("filtered or unknown replacement scope was accepted")
        assert digest(filtered / "excluded.bin") == excluded_hash
        token = prepare(filtered_archive, filtered, scope="filtered", mode="merge")
        helper_run("apply-restore", "--token", token)
        helper_run("release-restore", "--token", token)
        assert digest(filtered / "excluded.bin") == excluded_hash
        try:
            prepare(filtered_archive, data)
        except subprocess.CalledProcessError:
            pass
        else:
            raise AssertionError("managed root replacement was accepted")
        check("filtered_replace_and_managed_root_refused", excluded_file_preserved=True, explicit_merge_passed=True)

        source, target = data / "fault-source", data / "fault-target"
        source.mkdir()
        target.mkdir()
        with (source / "captured.bin").open("wb") as stream:
            stream.truncate(256 * 1024 * 1024)
        (target / "before.txt").write_text("original target data retained for inspection")
        fault_archive = work / "fault.tar"
        archive(source, fault_archive)
        token = prepare(fault_archive, target)
        state_path = work / "helper-spool" / token / "state.json"
        process = subprocess.Popen([str(helper), "apply-restore", "--token", token],
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE, start_new_session=True)
        killed = False
        deadline = time.monotonic() + 30
        while process.poll() is None and time.monotonic() < deadline:
            state = json.loads(state_path.read_text())
            extracted = target / "captured.bin"
            if state["phase"] == "writing" and extracted.exists() and extracted.stat().st_size > 0:
                os.killpg(process.pid, signal.SIGKILL)
                killed = True
                break
            time.sleep(0.001)
        stdout, stderr = process.communicate(timeout=10)
        if not killed:
            raise AssertionError(f"did not interrupt actual extraction: rc={process.returncode}, stderr={stderr[:300]!r}")
        state = json.loads(state_path.read_text())
        rollback = target / state["rollback"]
        assert state["phase"] == "writing", state
        assert (rollback / "before.txt").read_text() == "original target data retained for inspection"
        assert (target / "captured.bin").stat().st_size > 0
        try:
            helper_run("release-restore", "--token", token)
        except subprocess.CalledProcessError:
            pass
        else:
            raise AssertionError("release deleted interrupted recovery evidence")
        assert run(["docker", "inspect", "--format", "{{.State.Running}}", container]).stdout.strip() == b"false"
        check("sigkill_during_actual_target_extraction", phase=state["phase"],
              rollback_preserved=True, partial_target_present=True, services_stopped=True)
        succeeded = True
    finally:
        evidence["succeeded"] = succeeded
        Path(args.result).write_text(json.dumps(evidence, indent=2) + "\n", encoding="utf-8")
        if created_container:
            subprocess.run(["docker", "rm", "--force", container], check=False,
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if succeeded or not args.keep_on_failure:
            shutil.rmtree(work)
        else:
            print(f"Preserved failed fixture for inspection: {work}", file=sys.stderr)
    return evidence


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work-dir", required=True)
    parser.add_argument("--helper", required=True)
    parser.add_argument("--result", required=True)
    parser.add_argument("--postgres-image", default="postgres:17-alpine")
    parser.add_argument("--keep-on-failure", action="store_true")
    args = parser.parse_args()
    try:
        exercise(args)
    except subprocess.CalledProcessError as error:
        # Do not print command arguments: a helper invocation may include its
        # temporary capability token. These fixtures never log credentials.
        detail = (error.stderr or b"").decode("utf-8", errors="replace")[:2000]
        raise RuntimeError(f"fixture command failed with exit {error.returncode}: {detail}") from None


if __name__ == "__main__":
    main()
