#!/usr/bin/env python3
"""Check real SFTP cleanup during restart/package reinstall on an exclusive VM.

Run as root with automatic backups paused. Owns a separate repository, sshd
container, temporary systemd PATH drop-in and rate-limited native-restic wrapper.
No output is fabricated. Failed fixtures remain for inspection; the drop-in is
always removed. Credentials and session cookies are never written to the report.
"""
import argparse
import http.cookiejar
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import shutil
import socket
import subprocess
import time
import urllib.error
import urllib.request


def command(args):
    return subprocess.run(args, check=True, capture_output=True, text=True).stdout.strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--package', required=True)
    parser.add_argument('--expected-commit', required=True)
    parser.add_argument('--initial-commit', help='expected old source when testing a version-changing upgrade')
    parser.add_argument('--image', default='stacklab-sftp-fixture:r4')
    parser.add_argument('--legacy-restic', help='optional native 0.14 binary')
    args = parser.parse_args()
    if os.geteuid() != 0 or not re.fullmatch('[a-z0-9-]{1,30}', args.run_id):
        raise RuntimeError('requires root and a fresh simple run ID')
    package = Path(args.package).resolve(strict=True)
    initial_commit = Path('/usr/lib/stacklab/metadata/commit.txt').read_text().strip()
    if initial_commit != (args.initial_commit or args.expected_commit):
        raise RuntimeError('installed source differs from expected commit')
    if command(['systemctl', 'show', 'stacklab', '-p', 'KillMode', '--value']) != 'mixed':
        raise RuntimeError('installed unit does not use mixed termination')
    os.umask(0o077)
    workspace = Path('/var/lib/stacklab-stop-' + args.run_id)
    report_dir = Path('/root/stacklab-stop-' + args.run_id)
    dropin = Path('/etc/systemd/system/stacklab.service.d/90-stop-' + args.run_id + '.conf')
    if any(p.exists() for p in (workspace, report_dir, dropin)):
        raise RuntimeError('fixture path already exists')
    report_dir.mkdir()
    report = {'commit': args.expected_commit, 'initial_commit': initial_commit, 'checks': [], 'succeeded': False}
    settings = {}
    for line in Path('/etc/stacklab/stacklab.env').read_text().splitlines():
        if '=' in line and not line.lstrip().startswith('#'):
            key, value = line.split('=', 1)
            settings[key.strip()] = value.strip().strip('\"\'')
    password = settings['STACKLAB_BOOTSTRAP_PASSWORD']
    base = 'http://127.0.0.1:8080'
    api = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

    def request(path, payload=None, method=None):
        data = json.dumps(payload).encode() if payload is not None else None
        req = urllib.request.Request(base + path, data=data, method=method,
                                     headers={'Content-Type': 'application/json', 'Origin': base})
        deadline = time.monotonic() + 100
        while True:
            try:
                with api.open(req, timeout=15) as response:
                    raw = response.read()
                    return json.loads(raw) if raw else None
            except urllib.error.HTTPError as error:
                if error.code == 409 and time.monotonic() < deadline:
                    time.sleep(.25)
                    continue
                raise RuntimeError('API request failed: ' + path + ' ' + str(error.code)) from None

    def reconnect():
        deadline = time.monotonic() + 40
        while True:
            try:
                request('/api/auth/login', {'password': password})
                return
            except (OSError, RuntimeError):
                if time.monotonic() > deadline:
                    raise
                time.sleep(.2)

    def check():
        job = request(endpoint + '/check', {'read_data': 'all'})['job']['id']
        return job

    def wait_job(job):
        deadline = time.monotonic() + 150
        while time.monotonic() < deadline:
            result = request('/api/jobs/' + job)['job']
            if result['state'] not in ('queued', 'running'):
                return result
            time.sleep(.15)
        raise TimeoutError('fixture check did not finish')

    def destination():
        return next(d for d in request('/api/backups')['destinations'] if d['id'] == destination_id)

    identity = pwd.getpwnam('stacklab')
    container = 'stacklab-stop-' + args.run_id
    destination_id = None
    reconnect()
    if not request('/api/backups/policy')['paused']:
        raise RuntimeError('pause automatic backups before this exclusive exercise')
    initial = request('/api/backups')
    initial_ids = {d['id'] for d in initial['destinations']}
    workspace.mkdir(mode=0o755)
    os.chown(workspace, identity.pw_uid, identity.pw_gid)
    slow = workspace / 'slow-check'
    wrapper = workspace / 'bin'
    wrapper.mkdir(mode=0o755)
    wrapper.chmod(0o755)
    try:
        for name in ('client_key', 'host_key'):
            command(['ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', str(workspace / name)])
        probe = socket.socket()
        probe.bind(('127.0.0.1', 0))
        port = probe.getsockname()[1]
        probe.close()
        configuration = ('Port 22\nHostKey /fixture/host_key\nAuthorizedKeysFile /fixture/client_key.pub\n'
                         'PasswordAuthentication no\nKbdInteractiveAuthentication no\nUsePAM no\nStrictModes no\n'
                         'AllowUsers fixture\nSubsystem sftp internal-sftp\n')
        (workspace / 'sshd_config').write_text(configuration)
        bootstrap = 'id fixture >/dev/null 2>&1 || { useradd -u "$1" -U -d /fixture -s /bin/sh fixture; passwd -d fixture >/dev/null; }; mkdir -p /run/sshd; exec /usr/sbin/sshd -D -e -f /fixture/sshd_config'
        for item in workspace.iterdir():
            if item.is_file():
                os.chown(item, identity.pw_uid, identity.pw_gid)
        command(['docker', 'run', '-d', '--name', container, '--label', 'stacklab.fixture=service-stop',
                 '-p', f'127.0.0.1:{port}:22', '-v', str(workspace) + ':/fixture', args.image,
                 'sh', '-eu', '-c', bootstrap, '--', str(identity.pw_uid)])
        for _ in range(100):
            try:
                with socket.create_connection(('127.0.0.1', port), .5) as connection:
                    if connection.recv(100).startswith(b'SSH-'):
                        break
            except OSError:
                time.sleep(.1)
        host_key = (workspace / 'host_key.pub').read_text().split()
        known = f'[127.0.0.1]:{port} {host_key[0]} {host_key[1]}\n'
        repository_password = secrets.token_urlsafe(32)
        secret_file = workspace / 'password'
        secret_file.write_text(repository_password)
        os.chown(secret_file, identity.pw_uid, identity.pw_gid)
        payload = workspace / 'payload'
        with payload.open('wb') as output:
            for _ in range(64):
                output.write(os.urandom(1024 * 1024))
        os.chown(payload, identity.pw_uid, identity.pw_gid)
        local_restic = ['runuser', '-u', 'stacklab', '--', 'env', 'RESTIC_PASSWORD_FILE=' + str(secret_file),
                        '/usr/bin/restic', '-r', str(workspace / 'repository')]
        command(local_restic + ['init'])
        command(local_restic + ['backup', str(payload)])
        destination_id = request('/api/backups/destinations', {
            'name': 'Service stop ' + args.run_id, 'kind': 'sftp',
            'repository': f'sftp://fixture@127.0.0.1:{port}//fixture/repository',
            'secrets': {'password': repository_password, 'ssh_private_key': (workspace / 'client_key').read_text(), 'ssh_known_hosts': known},
            'check_schedule': {'frequency': 'manual'}, 'prune_schedule': {'frequency': 'manual'}, 'check_read_data': 'all',
        })['destination']['id']
        endpoint = '/api/backups/destinations/' + destination_id
        dropin.parent.mkdir(exist_ok=True)
        dropin.write_text('[Service]\nEnvironment="PATH=' + str(wrapper) + ':/usr/local/bin:/usr/bin:/bin"\n')
        dropin.chmod(0o644)
        binaries = ['/usr/bin/restic'] + ([args.legacy_restic] if args.legacy_restic else [])
        for binary in binaries:
            if not re.fullmatch('/[a-zA-Z0-9_./-]+', binary):
                raise RuntimeError('invalid native binary path')
            native_version = command([binary, 'version']).split()[1]
            script = '#!/bin/sh\nif [ -f ' + str(slow) + ' ]; then exec ' + binary + ' --limit-download 1024 "$@"; fi\nexec ' + binary + ' "$@"\n'
            (wrapper / 'restic').write_text(script)
            (wrapper / 'restic').chmod(0o755)
            assert command(['runuser', '-u', 'stacklab', '--', 'env', 'PATH=' + str(wrapper) + ':/usr/bin:/bin', 'which', 'restic']) == str(wrapper / 'restic')
            command(['systemctl', 'daemon-reload'])
            command(['systemctl', 'restart', 'stacklab'])
            reconnect()
            assert request('/api/backups')['restic_version'] == native_version
            assert wait_job(check())['state'] == 'succeeded'
            for mode in ('package-install', 'restart', 'restart'):
                baseline = destination()
                slow.touch()
                job = check()
                locks = workspace / 'repository/locks'
                deadline = time.monotonic() + 20
                while not any(locks.iterdir()):
                    if time.monotonic() > deadline:
                        raise TimeoutError('native check did not acquire lock')
                    time.sleep(.05)
                # Allow the native reader to reach data while its 1 MiB/s limit
                # keeps the 64 MiB check active. Storage and SSH remain available.
                time.sleep(2)
                assert request('/api/jobs/' + job)['job']['state'] == 'running'
                started = time.monotonic()
                package_before = command(['dpkg-query', '-W', '-f=${Version}', 'stacklab'])
                if mode == 'restart':
                    command(['systemctl', 'restart', 'stacklab'])
                else:
                    result = subprocess.run(['dpkg', '--force-confold', '-i', str(package)], capture_output=True)
                    (report_dir / ('dpkg-' + native_version + '.log')).write_bytes(result.stdout + result.stderr)
                    command(['apt-mark', 'hold', 'stacklab'])
                    result.check_returncode()
                    package_after = command(['dpkg-query', '-W', '-f=${Version}', 'stacklab'])
                    mode = 'package-upgrade' if package_before != package_after else 'package-reinstall'
                    assert Path('/usr/lib/stacklab/metadata/commit.txt').read_text().strip() == args.expected_commit
                reconnect()
                slow.unlink()
                outcome = wait_job(job)
                current = destination()
                assert outcome['state'] in ('failed', 'cancelled', 'interrupted'), outcome['state']
                assert not list(locks.iterdir()), 'native SFTP check left locks after service stop'
                assert not current.get('check_hold'), 'shutdown created a hold'
                for key in ('metadata_check', 'data_check'):
                    assert current[key] == baseline[key], 'shutdown changed integrity evidence'
                report['checks'].append({'mode': mode, 'restic': native_version, 'state': outcome['state'],
                                         'attempt_error': current.get('last_check_attempt_error'),
                                         'seconds': round(time.monotonic() - started, 3), 'remaining_locks': 0})
                assert wait_job(check())['state'] == 'succeeded'
        request(endpoint, method='DELETE')
        destination_id = None
        assert {d['id'] for d in request('/api/backups')['destinations']} == initial_ids
        assert Path('/usr/lib/stacklab/metadata/commit.txt').read_text().strip() == args.expected_commit
        report['package'] = command(['dpkg-query', '-W', '-f=${Version}', 'stacklab'])
        report['succeeded'] = True
    finally:
        dropin.unlink(missing_ok=True)
        slow.unlink(missing_ok=True)
        command(['systemctl', 'daemon-reload'])
        command(['systemctl', 'restart', 'stacklab'])
        command(['apt-mark', 'hold', 'stacklab'])
        try:
            reconnect()
            request('/api/auth/logout', {})
        except (OSError, RuntimeError):
            pass
        if report['succeeded']:
            command(['docker', 'rm', '-f', container])
            shutil.rmtree(workspace)
        report['destination_id'] = destination_id
        (report_dir / 'result.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report))


if __name__ == '__main__':
    main()
