#!/usr/bin/env python3
"""Run the installed Stacklab API ACL regression in an exclusive disposable VM.

Run as root after upgrading Stacklab, with a new --run-id and --expected-commit.
Reads its existing bootstrap password only in memory. Preserves fixtures on
failure; removes only its own plan, destination, recovery results and paths on
success. Evidence intentionally contains no credentials or session cookies.
"""
import argparse
import hashlib
import http.cookiejar
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import shutil
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


def command(args):
    return subprocess.run(args, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE).stdout.decode()


def checksum(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run-id', required=True)
    parser.add_argument('--expected-commit', required=True)
    parser.add_argument('--work-dir', required=True)
    parser.add_argument('--evidence-dir', required=True)
    parser.add_argument('--base-url', default='http://127.0.0.1:8080')
    credentials = parser.add_mutually_exclusive_group()
    credentials.add_argument('--password-file', help='private file containing the current login password')
    credentials.add_argument('--password-stdin', action='store_true', help='read the current login password from stdin')
    args = parser.parse_args()
    if os.geteuid() != 0 or not re.fullmatch('[a-z0-9-]{1,40}', args.run_id):
        raise RuntimeError('requires root and a simple fresh run ID')
    if not re.fullmatch('[0-9a-f]{7,40}', args.expected_commit):
        raise RuntimeError('expected commit must be an explicit Git commit ID')
    api_url = urllib.parse.urlparse(args.base_url)
    if (api_url.scheme not in ('http', 'https') or api_url.hostname not in ('localhost', '127.0.0.1', '::1')
            or api_url.username or api_url.password or api_url.path or api_url.query or api_url.fragment):
        raise RuntimeError('the API must use a local HTTP(S) origin without a path or credentials')
    os.umask(0o077)
    settings = {}
    for line in Path('/etc/stacklab/stacklab.env').read_text().splitlines():
        if '=' in line and not line.lstrip().startswith('#'):
            key, value = line.split('=', 1)
            settings[key.strip()] = value.strip().strip('\"\'')
    if args.password_file:
        secret = Path(args.password_file)
        if secret.is_symlink() or not secret.is_file() or secret.stat().st_mode & 0o077:
            raise RuntimeError('password file must be a private regular file')
        password = secret.read_text().rstrip('\r\n')
    elif args.password_stdin:
        password = sys.stdin.readline().rstrip('\r\n')
    else:
        password = settings.get('STACKLAB_BOOTSTRAP_PASSWORD')
    if not password:
        raise RuntimeError('provide the current password with --password-file or --password-stdin; do not reset authentication')
    installed = Path('/usr/lib/stacklab/metadata/commit.txt').read_text().strip()
    if not re.fullmatch('[0-9a-f]{7,40}', installed):
        raise RuntimeError('installed package commit metadata is invalid')
    if not (installed.startswith(args.expected_commit) or args.expected_commit.startswith(installed)):
        raise RuntimeError('installed package commit does not match expected source')
    root = Path(settings.get('STACKLAB_ROOT', '/srv/stacklab'))
    source = root / 'config' / ('stacklab-r3-acl-' + args.run_id)
    target = root / 'config' / ('stacklab-r3-acl-target-' + args.run_id)
    workspace = Path(args.work_dir)
    evidence = Path(args.evidence_dir)
    if str(workspace) != '/var/lib/stacklab-backup-api-' + args.run_id or str(evidence) != '/root/stacklab-backup-api-' + args.run_id:
        raise RuntimeError('work and evidence directories must use their dedicated run ID prefixes')
    for item in (source, target, workspace, evidence):
        if item.exists() or item.is_symlink():
            raise RuntimeError('fixture/evidence path already exists: ' + str(item))
    evidence.mkdir(mode=0o700)
    shutil.copyfile(__file__, evidence / 'reproduce.py')
    report = {'source_commit': installed, 'package_version': command(['dpkg-query', '-W', '-f=${Version}', 'stacklab']),
              'binary_sha256': checksum(Path('/usr/lib/stacklab/bin/stacklab')), 'checks': [], 'jobs': [], 'succeeded': False}
    jar = http.cookiejar.CookieJar()
    api = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(jar))
    plan_id = destination_id = None
    restore_ids = []
    report['fixtures'] = {'source': str(source), 'target': str(target), 'workspace': str(workspace)}
    report['restore_ids'] = restore_ids
    identity = pwd.getpwnam('stacklab')

    def request(path, payload=None, method=None):
        data = json.dumps(payload).encode() if payload is not None else None
        req = urllib.request.Request(args.base_url + path, data=data, method=method,
                                     headers={'Content-Type': 'application/json', 'Origin': args.base_url})
        try:
            with api.open(req, timeout=60) as response:
                body = response.read()
                return json.loads(body) if body else None
        except urllib.error.HTTPError as error:
            body = error.read().decode(errors='replace')
            raise RuntimeError(f'API {req.method} {path} returned {error.code}: {body}') from None

    def save_report():
        (evidence / 'result.json').write_text(json.dumps(report, indent=2) + '\n')

    def wait_job(started, name, expected_state="succeeded"):
        job_id = started['job']['id']
        deadline = time.monotonic() + 180
        while time.monotonic() < deadline:
            job = request('/api/jobs/' + job_id)['job']
            if job['state'] not in ('queued', 'running'):
                report['jobs'].append({'name': name, 'id': job_id, 'state': job['state']})
                events = request('/api/jobs/' + job_id + '/events')
                (evidence / (name + '-events.json')).write_text(json.dumps(events, indent=2) + '\n')
                if job['state'] != expected_state:
                    save_report()
                    raise AssertionError(name + ' job failed: ' + json.dumps(job))
                return job_id
            time.sleep(0.2)
        raise TimeoutError(name + ' job did not finish in 180 seconds')

    def restored(job_id):
        results = [item for item in request('/api/backups/restores')['items'] if item.get('job_id') == job_id]
        if len(results) != 1:
            raise AssertionError('completed restore did not create one recovery result')
        result = results[0]
        restore_ids.append(result['id'])
        return result

    def metadata(path):
        info = path.stat()
        return {'mode': oct(stat.S_IMODE(info.st_mode)), 'uid': info.st_uid, 'gid': info.st_gid,
                'acl': command(['getfacl', '-cpn', str(path)]).strip(),
                'user_xattrs': {key: os.getxattr(path, key).hex() for key in sorted(os.listxattr(path)) if key.startswith('user.')},
                'sha256': checksum(path) if path.is_file() else None}

    def verify_tree(actual, name, selected=False):
        paths = ['readonly/nested.txt'] if selected else ['.', 'config.txt', 'readonly', 'readonly/nested.txt', 'zzz-hardlink.txt']
        observed = {}
        for relative in paths:
            actual_path, expected_path = actual / relative, source / relative
            want, got = metadata(expected_path), metadata(actual_path)
            if selected:
                # Selected-file recovery is a private inspection copy. The
                # untouched containing archive retains original ACLs/owners.
                matches = (got['sha256'] == want['sha256'] and got['mode'] == '0o600'
                           and got['uid'] == identity.pw_uid and got['gid'] == identity.pw_gid
                           and got['user_xattrs'] == {})
            else:
                matches = got == want
            if not matches:
                raise AssertionError(name + ' metadata mismatch at ' + relative + ': ' + json.dumps({'expected': want, 'actual': got}))
            observed[relative] = got
        if not selected and (actual / 'config.txt').stat().st_ino != (actual / 'zzz-hardlink.txt').stat().st_ino:
            raise AssertionError(name + ' did not preserve hardlink identity')
        if selected and (actual / 'config.txt').exists():
            raise AssertionError('selected-file restore materialized an unselected file')
        report['checks'].append({'name': name, 'result': 'passed', 'metadata': observed})
        save_report()

    try:
        if request('/api/auth/login', {'password': password}).get('authenticated') is not True:
            raise RuntimeError('authentication failed')
        before = request('/api/backups')
        before_ids = {key: {item['id'] for item in before[key]} for key in ('plans', 'destinations')}
        before_policy = request('/api/backups/policy')
        if not before_policy['paused']:
            raise RuntimeError('pause automatic backups before the exclusive manual-repair exercise')
        if before_policy['host_review_required'] or before.get('recovery_error'):
            raise RuntimeError('existing automatic recovery/host-review block; leave operator state unchanged')
        if before.get('helper_ready') is not True:
            raise RuntimeError('installed helper is not ready')
        source.mkdir(mode=0o750)
        target.mkdir(mode=0o750)
        workspace.mkdir(mode=0o700)
        for item in (source, target, workspace):
            os.chown(item, identity.pw_uid, identity.pw_gid)
        command(['setfacl', '-m', 'u::rwx,u:65534:r-x,g::r-x,m::r-x,o::---,d:u::rwx,d:u:65534:r-x,d:g::r-x,d:m::r-x,d:o::---', str(source)])
        payload = source / 'config.txt'
        payload.write_bytes(b'Stacklab R3 inherited ACL and read-only content\n')
        os.chown(payload, identity.pw_uid, identity.pw_gid)
        os.setxattr(payload, 'user.stacklab-r3', b'inherited-acl-regression')
        payload.chmod(0o444)
        os.link(payload, source / 'zzz-hardlink.txt')
        nested = source / 'readonly'
        nested.mkdir(mode=0o750)
        os.chown(nested, identity.pw_uid, identity.pw_gid)
        nested_file = nested / 'nested.txt'
        nested_file.write_bytes(b'Read-only nested directory with inherited ACL\n')
        os.chown(nested_file, identity.pw_uid, identity.pw_gid)
        nested_file.chmod(0o444)
        nested.chmod(0o555)
        (target / 'unarchived.txt').write_text('replace must remove this fixture-only file\n')
        filesystem = request('/api/backups/filesystem?path=' + urllib.parse.quote(str(workspace), safe=''))['device']
        repository_password = secrets.token_urlsafe(32)
        destination = request('/api/backups/destinations', {'name': 'R3 ACL API ' + args.run_id, 'kind': 'local',
            'repository': str(workspace / 'repository'), 'expected_filesystem': filesystem,
            'secrets': {'password': repository_password}, 'check_schedule': {'frequency': 'manual'},
            'check_read_data': 'metadata', 'prune_schedule': {'frequency': 'manual'}})['destination']
        destination_id = destination['id']
        report['destination_id'] = destination_id
        wait_job(request('/api/backups/destinations/' + destination_id + '/initialize', {}), 'initialize')
        source_id = 'source-r3-acl'
        plan = request('/api/backups/plans', {'name': 'R3 ACL API ' + args.run_id, 'enabled': True, 'consistency': 'files',
            'sources': [{'id': source_id, 'kind': 'managed_config', 'path': str(source)}],
            'destinations': [{'destination_id': destination_id, 'enabled': True, 'schedule': {'frequency': 'manual'}, 'retention': {'daily': 1, 'weekly': 0, 'monthly': 0}}],
            'staging_max_bytes': 10485760, 'staging_ttl_hours': 1, 'stop_timeout_seconds': 30})['plan']
        plan_id = plan['id']
        report['plan_id'] = plan_id
        started = request('/api/backups/plans/' + plan_id + '/run', {})
        wait_job(started, 'capture')
        run = next(item for item in request('/api/backups')['runs'] if item['id'] == started['run']['id'])
        copy = run['copies'][0]
        if run['state'] != 'succeeded' or copy['verification'] != 'recovery_objects_verified':
            raise AssertionError('capture did not create verified recovery objects')
        snapshot = copy['snapshot_id']
        if not re.fullmatch('[0-9a-f]{64}', snapshot):
            raise AssertionError('capture did not publish a full immutable snapshot ID')
        report['snapshot_id'] = snapshot
        before_unlock = next(item for item in request('/api/backups')['destinations'] if item['id'] == destination_id)
        wait_job(request('/api/backups/destinations/' + destination_id + '/unlock', {}), 'remove-stale-locks')
        after_unlock = next(item for item in request('/api/backups')['destinations'] if item['id'] == destination_id)
        if (after_unlock.get('last_operation') != 'backup_unlock' or after_unlock.get('last_operation_status') != 'succeeded'
                or any(before_unlock.get(key) != after_unlock.get(key) for key in ('metadata_check', 'data_check', 'check_status', 'check_error'))):
            raise AssertionError('explicit unlock did not record success independently of repository integrity')
        report['checks'].append({'name': 'unlock_preserves_integrity_evidence', 'result': 'passed'})
        full_job = wait_job(request('/api/backups/destinations/' + destination_id + '/restore', {'snapshot_id': snapshot[:8]}), 'isolated-full-restore')
        full = restored(full_job)
        if full.get('snapshot_id') != snapshot or full.get('recovery_status') != 'complete' or not full.get('classified_at'):
            raise AssertionError('short-ID full restore did not preserve the resolved snapshot identity and complete provenance')
        report['checks'].append({'name': 'short_snapshot_id_preserves_complete_provenance', 'result': 'passed',
                                 'requested_snapshot_id': snapshot[:8], 'resolved_snapshot_id': full['snapshot_id'],
                                 'recovery_status': full['recovery_status'], 'classified_at': full['classified_at']})
        verify_tree(Path(full['target']) / 'files' / source_id, 'isolated-full-restore')
        test_job = wait_job(request('/api/backups/destinations/' + destination_id + '/restore', {'snapshot_id': snapshot, 'test': True}), 'full-restore-test')
        tested = restored(test_job)
        verify_tree(Path(tested['target']) / 'files' / source_id, 'full-restore-test')
        snapshots = request('/api/backups/destinations/' + destination_id + '/snapshots')['items']
        proof = next(item for item in snapshots if item['id'] == snapshot)
        if not proof.get('restore_tested_at'):
            raise AssertionError('full restore test did not publish snapshot proof')
        selected_job = wait_job(request('/api/backups/destinations/' + destination_id + '/restore', {'snapshot_id': snapshot,
            'files': [{'source_id': source_id, 'path': 'readonly/nested.txt'}]}), 'selected-file-restore')
        selected = restored(selected_job)
        verify_tree(Path(selected['target']) / 'files' / source_id, 'selected-file-restore', selected=True)
        apply_request = {'restore_id': full['id'], 'snapshot_id': snapshot,
                         'mappings': [{'source_id': source_id, 'target': str(target), 'mode': 'replace'}], 'resume_services': False}
        review = request('/api/backups/destinations/' + destination_id + '/restore-preview', apply_request)
        if review['preview'].get('recovery_status') != 'complete' or review['preview'].get('classified_at') != full['classified_at']:
            raise AssertionError('review preview did not preserve the recovery result provenance')
        report['checks'].append({'name': 'review_preview_preserves_recovery_provenance', 'result': 'passed',
                                 'recovery_status': review['preview']['recovery_status'],
                                 'classified_at': review['preview']['classified_at']})
        if review['preview']['writers']:
            raise AssertionError('fixture target unexpectedly has container writers; do not apply')
        apply_request['review_token'] = review['review_token']
        wait_job(request('/api/backups/destinations/' + destination_id + '/restore', apply_request), 'reviewed-apply')
        verify_tree(target, 'reviewed-apply')
        if (target / 'unarchived.txt').exists():
            raise AssertionError('reviewed replace did not remove unarchived fixture file')
        report['checks'].append({'name': 'replace_removed_unarchived_fixture', 'result': 'passed'})
        # A real truncated encrypted pack verifies the installed integrity gate
        # and evidence-bound API review. Only this fixture repository is touched.
        endpoint = '/api/backups/destinations/' + destination_id
        wait_job(request(endpoint + '/check', {'read_data': 'all'}), 'healthy-full-check')
        pack = max((item for item in (workspace / 'repository' / 'data').rglob('*') if item.is_file()),
                   key=lambda item: item.stat().st_size)
        original_pack = pack.read_bytes()
        if len(original_pack) <= 64:
            raise AssertionError('fixture has no encrypted pack to truncate')
        pack.write_bytes(original_pack[:64])
        wait_job(request(endpoint + '/check', {'read_data': 'all'}), 'truncated-pack-check', 'failed')

        def current_destination():
            return next(item for item in request('/api/backups')['destinations'] if item['id'] == destination_id)

        first_failure = current_destination()
        first_token = first_failure.get('maintenance_review_token')
        if first_failure.get('data_check', {}).get('status') != 'failed' or not first_token:
            raise AssertionError('native damage did not publish failure and a review revision')

        def refused_prune(payload):
            req = urllib.request.Request(args.base_url + endpoint + '/prune', data=json.dumps(payload).encode(),
                headers={'Content-Type': 'application/json', 'Origin': args.base_url})
            try:
                with api.open(req, timeout=15) as response:
                    raise AssertionError('missing/stale acknowledgement was admitted: ' + str(response.status))
            except urllib.error.HTTPError as error:
                body = error.read().decode(errors='replace')
                if error.code != 409 or 'maintenance_acknowledgement_required' not in body:
                    raise AssertionError('unexpected acknowledgement rejection: ' + str(error.code) + ' ' + body) from None

        refused_prune({})
        refused_prune({'acknowledge_integrity_failure': True})
        wait_job(request(endpoint + '/check', {'read_data': 'all'}), 'new-truncated-pack-check', 'failed')
        second_failure = current_destination()
        second_token = second_failure.get('maintenance_review_token')
        if not second_token or second_token == first_token:
            raise AssertionError('new failed check did not invalidate the old review')
        refused_prune({'acknowledge_integrity_failure': True, 'acknowledgement_review_token': first_token})
        # Restore the exact original bytes before deliberate cleanup. The saved
        # failure must remain until a successful full check, even after prune.
        pack.write_bytes(original_pack)
        wait_job(request(endpoint + '/prune', {'acknowledge_integrity_failure': True,
            'acknowledgement_review_token': second_token}), 'current-evidence-prune')
        if current_destination().get('data_check', {}).get('status') != 'failed':
            raise AssertionError('prune erased unresolved integrity evidence')
        wait_job(request(endpoint + '/check', {'read_data': 'all'}), 'repaired-full-check')
        repaired = current_destination()
        if repaired.get('check_status') != 'verified' or repaired.get('maintenance_review_token') or repaired.get('check_hold'):
            raise AssertionError('full successful check did not resolve the saved failure')
        report['checks'].append({'name': 'native_corruption_and_current_evidence_prune_acknowledgement', 'result': 'passed'})
        save_report()
        # Exercise the documented offline procedure against only this owned
        # repository. Unlike the previous byte-restoration case, restic rebuilds
        # the index itself, as the actual installed service user.
        request('/api/auth/logout', {})
        command(['systemctl', 'stop', 'stacklab'])
        try:
            repository = workspace / 'repository'
            shutil.copytree(repository, evidence / 'independent-repository-copy')
            password_file = workspace / 'repair-password'
            password_file.write_text(repository_password)
            password_file.chmod(0o600)
            os.chown(password_file, identity.pw_uid, identity.pw_gid)
            restic = ['runuser', '-u', identity.pw_name, '--', 'env',
                'RESTIC_REPOSITORY=' + str(repository), 'RESTIC_PASSWORD_FILE=' + str(password_file),
                'restic', '--no-cache']
            indexes = list((repository / 'index').iterdir())
            if not indexes:
                raise AssertionError('manual repair fixture has no index')
            for index in indexes:
                index.unlink()
            damaged = subprocess.run(restic + ['check', '--read-data'], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            (evidence / 'missing-index-check.log').write_bytes(damaged.stdout + damaged.stderr)
            if damaged.returncode == 0:
                raise AssertionError('index deletion did not fail the native check')
            version = command(['restic', 'version']).strip()
            repair = ['rebuild-index'] if re.match(r'restic 0\.(14|15)\.', version) else ['repair', 'index']
            (evidence / 'native-index-repair.log').write_text(command(restic + repair))
            if any(index.stat().st_uid != identity.pw_uid for index in (repository / 'index').iterdir()):
                raise AssertionError('manual repair created indexes with the wrong owner')
            (evidence / 'native-repaired-full-check.log').write_text(command(restic + ['check', '--read-data']))
            password_file.unlink()
            report['checks'].append({'name': 'offline_native_index_repair_as_service_user', 'result': 'passed',
                'restic': version, 'repair_command': repair, 'uid': identity.pw_uid})
        finally:
            command(['systemctl', 'start', 'stacklab'])
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            try:
                if request('/api/ready').get('status') == 'ok':
                    break
            except (urllib.error.URLError, RuntimeError):
                pass
            time.sleep(0.2)
        else:
            raise RuntimeError('service did not become ready after manual repair')
        request('/api/auth/login', {'password': password})
        wait_job(request(endpoint + '/check', {'read_data': 'all'}), 'post-index-repair-full-check')
        repair_restore = restored(wait_job(request(endpoint + '/restore', {'snapshot_id': snapshot, 'test': True}), 'post-index-repair-restore'))
        verify_tree(Path(repair_restore['target']) / 'files' / source_id, 'post-index-repair-recovery')
        for restore_id in restore_ids:
            request('/api/backups/restores/' + restore_id, method='DELETE')
        request('/api/backups/plans/' + plan_id, method='DELETE')
        request('/api/backups/destinations/' + destination_id, method='DELETE')
        for item in (source, target, workspace):
            shutil.rmtree(item)
        after = request('/api/backups')
        after_policy = request('/api/backups/policy')
        if any(before_ids[key] != {item['id'] for item in after[key]} for key in before_ids):
            raise AssertionError('existing plans or destinations changed')
        if (before_policy['paused'], before_policy['host_review_required']) != (after_policy['paused'], after_policy['host_review_required']):
            raise AssertionError('existing operator policy changed')
        report['checks'].append({'name': 'owned_fixtures_cleaned_existing_configuration_preserved', 'result': 'passed'})
        report['succeeded'] = True
    except Exception as error:
        report['error'] = str(error)
        raise
    finally:
        original_error = sys.exc_info()[0] is not None
        logout_failed = False
        if len(jar):
            try:
                request('/api/auth/logout', {})
            except Exception:
                report['session_cleanup_failed'] = True
                report['succeeded'] = False
                logout_failed = True
                report.setdefault('error', 'Test-session cleanup failed; revoke the session after inspecting the service.')
        save_report()
        print(json.dumps({'succeeded': report['succeeded'], 'source_commit': installed, 'evidence': str(evidence),
                          'checks': [item['name'] for item in report['checks']], 'jobs': report['jobs'],
                          'error': report.get('error')}), flush=True)
        if logout_failed and not original_error:
            raise RuntimeError('Test-session cleanup failed; the exercise is not complete.')


if __name__ == '__main__':
    main()
