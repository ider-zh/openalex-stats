#!/usr/bin/env python3
"""One authorized v8 -> v10 promotion, with identity gates and v8 rollback.

Run only after the independent full SQL audit's explicit success notification.
Arguments are the audit report and its process exit-code evidence file.
"""
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import time
import urllib.request

BASE = Path('/home/ider/tmpdata/openalex-stats-dev-20261112')
EVIDENCE = BASE / 'core-concept-hits-v1'
RESULT = Path('/mnt/nvme0/public/openalex-stats/results/core-concept-hits-v1')
OLD_PID = 877825
OLD_START = 5119097662
V8 = str(BASE / 'oa-stats-v8')
V10 = str(BASE / 'oa-stats-v10')
V8_SHA = '744b93484e7b56fb0099bd04515c145a55ef9527aecbea93fc5a39dac6db682e'
V10_SHA = 'ac0efd9804294ed05c9f96d28a20b477d46d963e881edd836aff969e56b10fba'
RUN_ID = '4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455'
OLD_ARGV = [V8, 'serve', '--data', '/mnt/nvme0/public/openalex-stats/results/core-goal-v2', '--listen', '0.0.0.0:8899']
NEW_ARGV = [V10, 'serve', '--data-core', str(RESULT), '--listen', '0.0.0.0:8899']


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def digest(path):
    result = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(4 << 20), b''):
            result.update(block)
    return result.hexdigest()


def process(pid):
    root = Path('/proc') / str(pid)
    try:
        stat = (root / 'stat').read_text()
        fields = stat.rsplit(') ', 1)[1].split()
        if fields[0] == 'Z':
            return None
        return {'pid': pid, 'exe': os.readlink(root / 'exe'),
                'argv': [x.decode() for x in (root / 'cmdline').read_bytes().split(b'\0') if x],
                'start_clock_ticks': int(fields[19]), 'kernel_stat': stat.strip(),
                'clock_ticks_per_second': os.sysconf('SC_CLK_TCK'),
                'linux_boot_unix_seconds': next(int(line.split()[1]) for line in Path('/proc/stat').read_text().splitlines() if line.startswith('btime '))}
    except FileNotFoundError:
        return None


def exact(pid, exe, argv, start=None):
    p = process(pid)
    if p is None or p['exe'] != exe or p['argv'] != argv or (start is not None and p['start_clock_ticks'] != start):
        raise RuntimeError('refusing process mutation: identity changed: ' + repr(p))
    return p


def fetch(url):
    with urllib.request.urlopen(url, timeout=10) as response:
        return json.load(response)


def persist(state):
    state['updated_at_utc'] = now()
    destination = EVIDENCE / 'production-promotion.json'
    temporary = destination.with_suffix('.json.tmp')
    temporary.write_text(json.dumps(state, indent=2) + '\n')
    temporary.replace(destination)


def wait_stopped(pid, start, seconds=25):
    end = time.monotonic() + seconds
    while time.monotonic() < end:
        p = process(pid)
        if p is None:
            return
        if p['start_clock_ticks'] != start:
            raise RuntimeError('PID reused during stop; refusing further mutation')
        time.sleep(.2)
    raise RuntimeError('original process did not stop after SIGTERM')


def launch(argv, logname):
    log = EVIDENCE / logname
    with log.open('ab', buffering=0) as output:
        child = subprocess.Popen(['nohup', *argv], cwd=BASE, stdin=subprocess.DEVNULL,
                                 stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
    end = time.monotonic() + 10
    while time.monotonic() < end:
        p = process(child.pid)
        if p and p['exe'] == argv[0] and p['argv'] == argv:
            return child, p, str(log)
        if child.poll() is not None:
            raise RuntimeError('service exited before identity was recorded; inspect ' + str(log))
        time.sleep(.05)
    raise RuntimeError('spawned service identity was not established; inspect ' + str(log))


def wait_ready(child, identity, expected_run, modern=True, seconds=300):
    end = time.monotonic() + seconds
    last = None
    while time.monotonic() < end:
        if child.poll() is not None:
            raise RuntimeError('service exited with ' + str(child.returncode))
        exact(child.pid, identity['exe'], identity['argv'], identity['start_clock_ticks'])
        try:
            suffix = '?corpus=core' if modern else ''
            meta = fetch('http://127.0.0.1:8899/api/meta' + suffix)
            run = fetch('http://127.0.0.1:8899/download?file=run.json' + ('&corpus=core' if modern else ''))
            if meta.get('ready') is True and meta.get('status') == 'full_snapshot_complete' and run.get('run_id') == expected_run:
                if modern and (meta.get('corpus') != 'core' or meta.get('eci_scenario_id') != 'fractional-concept-whole-0.3'):
                    raise RuntimeError('ready endpoint has unexpected corpus or scenario')
                return meta, run
            last = 'ready endpoint has unexpected run/status'
        except Exception as error:
            last = str(error)
        time.sleep(2)
    raise RuntimeError('service readiness timeout: ' + str(last))


def main():
    if len(sys.argv) != 3:
        raise RuntimeError('require audit.json and audit process exit-code file')
    audit_path, exit_path = map(Path, sys.argv[1:])
    audit = json.loads(audit_path.read_text())
    if exit_path.read_text().strip() != '0' or audit.get('passed') is not True or audit.get('status') != 'passed' or audit.get('failed_checks') != 0 or audit.get('run_id') != RUN_ID or not audit.get('checks') or not all(c.get('passed') is True for c in audit['checks']) or audit.get('model_audit_passed') is not True or audit.get('model_checks') != 216:
        raise RuntimeError('independent audit gate has not passed for the exact new core run')
    if digest(V8) != V8_SHA or digest(V10) != V10_SHA:
        raise RuntimeError('frozen executable hash mismatch')
    old = exact(OLD_PID, V8, OLD_ARGV, OLD_START)
    previous_meta = fetch('http://127.0.0.1:8899/api/meta')
    previous_run = fetch('http://127.0.0.1:8899/download?file=run.json')
    candidate = fetch('http://127.0.0.1:8900/api/meta?corpus=core')
    candidate_run = fetch('http://127.0.0.1:8900/download?file=run.json&corpus=core')
    if not previous_meta.get('ready') or not previous_run.get('run_id') or not candidate.get('ready') or candidate_run.get('run_id') != RUN_ID:
        raise RuntimeError('previous or candidate service identity probe failed')
    completion = json.loads((RESULT / 'completion.json').read_text())
    if completion.get('run_id') != RUN_ID:
        raise RuntimeError('completion marker does not identify expected new core run')
    state = {'status': 'prepared', 'started_at_utc': now(), 'audit': str(audit_path),
             'audit_sha256': digest(audit_path), 'audit_exit_file': str(exit_path),
             'audit_checks': len(audit['checks']), 'old': old, 'old_binary_sha256': V8_SHA,
             'old_run_id': previous_run['run_id'], 'new_binary_sha256': V10_SHA,
             'expected_run_id': RUN_ID, 'new_argv': NEW_ARGV, 'candidate_port': 8900,
             'shared_lock': str(BASE / 'production-service-8899.lock')}
    persist(state)
    new_child = None
    new_identity = None
    stopped = False
    try:
        exact(OLD_PID, V8, OLD_ARGV, OLD_START)
        os.kill(OLD_PID, signal.SIGTERM)
        wait_stopped(OLD_PID, OLD_START)
        stopped = True
        state['status'] = 'old_stopped'
        state['old_stopped_at_utc'] = now()
        persist(state)
        new_child, new_identity, log = launch(NEW_ARGV, 'service-production.log')
        state.update(status='new_loading', new=new_identity, log=log)
        (EVIDENCE / 'service-production.pid').write_text(str(new_child.pid) + '\n')
        persist(state)
        meta, run = wait_ready(new_child, new_identity, RUN_ID)
        if digest(V10) != V10_SHA:
            raise RuntimeError('frozen v10 binary changed during service startup')
        state.update(status='ready', ready_at_utc=now(), selected_works=meta['selected_works'],
                     run_id=run['run_id'], corpus=meta['corpus'], eci_scenario=meta['eci_scenario_id'])
        persist(state)
        print(json.dumps(state), flush=True)
    except Exception as error:
        state.update(status='promotion_failed', error=str(error))
        persist(state)
        if new_child is not None and new_identity is not None and process(new_child.pid) is not None:
            exact(new_child.pid, V10, NEW_ARGV, new_identity['start_clock_ticks'])
            os.kill(new_child.pid, signal.SIGTERM)
            wait_stopped(new_child.pid, new_identity['start_clock_ticks'])
        if stopped:
            rollback_child, rollback_identity, rollback_log = launch(OLD_ARGV, 'service-production-rollback-v8.log')
            state.update(status='rollback_loading', rollback=rollback_identity, rollback_log=rollback_log)
            persist(state)
            wait_ready(rollback_child, rollback_identity, previous_run['run_id'], modern=False)
            state.update(status='rollback_ready', rollback_ready_at_utc=now())
            persist(state)
        raise


if __name__ == '__main__':
    with (BASE / 'production-service-8899.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        main()
