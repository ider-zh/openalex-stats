#!/usr/bin/env python3
"""Authorized core -> dual corpus promotion after exact audit and browser gates."""
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
EVIDENCE = BASE / 'all-concept-hits-v1'
V10 = str(BASE / 'oa-stats-v10')
SHA = 'ac0efd9804294ed05c9f96d28a20b477d46d963e881edd836aff969e56b10fba'
CORE = '/mnt/nvme0/public/openalex-stats/results/core-concept-hits-v1'
ALL = '/mnt/nvme0/public/openalex-stats/results/all-concept-hits-v1'
RUNS = {'core': '4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455',
        'all': 'dd679f119b4d24a9d6725e79dec96af9d731265de59844ae04bbd028a719153e'}
CORE_ARGV = [V10, 'serve', '--data-core', CORE, '--listen', '0.0.0.0:8899']
NEW_ARGV = [V10, 'serve', '--data-core', CORE, '--data-all', ALL, '--listen', '0.0.0.0:8899']
STATE = EVIDENCE / 'production-catalog-promotion.json'

def read(path):
    return json.loads(Path(path).read_text())

def digest(path):
    h = hashlib.sha256()
    with open(path, 'rb') as stream:
        for block in iter(lambda: stream.read(4 << 20), b''):
            h.update(block)
    return h.hexdigest()

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
                'linux_boot_unix_seconds': next(int(x.split()[1]) for x in Path('/proc/stat').read_text().splitlines() if x.startswith('btime '))}
    except FileNotFoundError:
        return None

def exact(identity, argv):
    actual = process(identity['pid'])
    if actual is None or actual['exe'] != V10 or actual['argv'] != argv or actual['start_clock_ticks'] != identity['start_clock_ticks']:
        raise RuntimeError('refusing mutation; exact project process identity changed: ' + repr(actual))
    return actual

def persist(state):
    state['updated_at_utc'] = datetime.datetime.now(datetime.timezone.utc).isoformat()
    temporary = STATE.with_suffix('.json.tmp')
    temporary.write_text(json.dumps(state, indent=2) + '\n')
    temporary.replace(STATE)

def get(url):
    with urllib.request.urlopen(url, timeout=10) as response:
        return json.load(response)

def probe(port, corpora):
    base = f'http://127.0.0.1:{port}'
    catalog = get(base + '/api/catalog')
    assert catalog['default_corpus'] == 'core'
    assert [d['id'] for d in catalog['datasets']] == corpora
    evidence = {}
    for corpus in corpora:
        meta = get(base + '/api/meta?corpus=' + corpus)
        run = get(base + '/download?file=run.json&corpus=' + corpus)
        assert meta['ready'] and meta['status'] == 'full_snapshot_complete'
        assert meta['corpus'] == meta['scope']['corpus'] == corpus
        assert meta['eci_scenario_id'] == 'fractional-concept-whole-0.3'
        assert run['run_id'] == RUNS[corpus]
        evidence[corpus] = {'run_id': run['run_id'], 'selected_works': meta['selected_works']}
    return {'catalog': catalog, 'datasets': evidence}

def stop(identity, argv):
    exact(identity, argv)
    os.kill(identity['pid'], signal.SIGTERM)
    end = time.monotonic() + 25
    while time.monotonic() < end:
        current = process(identity['pid'])
        if current is None:
            return
        if current['start_clock_ticks'] != identity['start_clock_ticks']:
            raise RuntimeError('PID reused during termination; refusing further mutation')
        time.sleep(.2)
    raise RuntimeError('exact project service did not stop after SIGTERM')

def launch(argv, logname, gomaxprocs):
    environment = os.environ.copy()
    if gomaxprocs is None:
        environment.pop('GOMAXPROCS', None)
    else:
        environment['GOMAXPROCS'] = gomaxprocs
    with (EVIDENCE / logname).open('ab', buffering=0) as output:
        child = subprocess.Popen(['nohup', *argv], cwd=BASE, stdin=subprocess.DEVNULL,
                                 stdout=output, stderr=subprocess.STDOUT, start_new_session=True, env=environment)
    end = time.monotonic() + 10
    while time.monotonic() < end:
        identity = process(child.pid)
        if identity and identity['exe'] == V10 and identity['argv'] == argv:
            return child, identity
        if child.poll() is not None:
            raise RuntimeError('new service exited before identity was established')
        time.sleep(.05)
    raise RuntimeError('new service identity could not be established')

def wait_ready(child, identity, argv, corpora):
    end = time.monotonic() + 900
    last = None
    while time.monotonic() < end:
        if child.poll() is not None:
            raise RuntimeError('new service exited with code ' + str(child.returncode))
        exact(identity, argv)
        try:
            return probe(8899, corpora)
        except Exception as error:
            last = str(error)
        time.sleep(2)
    raise RuntimeError('readiness timeout: ' + str(last))

def audit_gate(report_path, exit_path, corpus):
    audit = read(report_path)
    assert Path(exit_path).read_text().strip() == '0'
    assert audit['passed'] is True and audit['status'] == 'passed' and audit['failed_checks'] == 0
    assert audit['run_id'] == RUNS[corpus]
    assert audit['checks'] and all(c['passed'] is True for c in audit['checks'])
    assert audit['model_audit_passed'] is True and audit['model_checks'] == 216
    return {'path': report_path, 'sha256': digest(report_path), 'exit_file': exit_path, 'run_id': audit['run_id']}

def browser_gate(path, corpus):
    report = read(path)
    assert report['status'] == 'passed' and report['mode'] == 'formal'
    assert report['url'].rstrip('/') == 'http://192.168.1.229:8901'
    assert 'asset_override' not in report and report['dataset']['corpus'] == corpus
    assert report['dataset']['status'] == 'full_snapshot_complete'
    assert not report['errors'] and not report['requests_outside_service']
    assert any(c['name'] == 'core_all_one_click_preserves_filters_and_routes_actual_dataset' and c['pass'] for c in report['checks'])
    return {'path': path, 'sha256': digest(path), 'corpus': corpus, 'checks': len(report['checks'])}

def rollback(state):
    exact(state['new'], NEW_ARGV)
    stop(state['new'], NEW_ARGV)
    child, identity = launch(CORE_ARGV, 'service-catalog-rollback-core.log', state['old_gomaxprocs'])
    state.update(status='rollback_loading', rollback=identity)
    persist(state)
    state.update(status='rollback_ready', rollback_health=wait_ready(child, identity, CORE_ARGV, ['core']))
    persist(state)

def main():
    if sys.argv[1:] == ['--rollback']:
        state = read(STATE)
        assert state['status'] in ['ready', 'new_loading', 'production_browser_failed']
        assert digest(V10) == SHA
        rollback(state)
        print(json.dumps(state), flush=True)
        return
    if len(sys.argv) != 8:
        raise RuntimeError('require core handoff JSON, core audit JSON/exit, all audit JSON/exit, candidate core/all browser reports')
    handoff, core_audit, core_exit, all_audit, all_exit, core_qa, all_qa = sys.argv[1:]
    assert digest(V10) == SHA
    previous = read(handoff)
    assert previous['status'] == 'passed' and previous['run_id'] == RUNS['core']
    assert previous['corpus'] == 'core' and previous['new_binary_sha256'] == SHA
    assert previous['browser_checks'] == 34 and previous['rollback_needed'] is False
    old = exact(previous['new'], CORE_ARGV)
    comparison = read(EVIDENCE / 'comparison-summary.json')
    assert comparison['status'] == 'passed' and all(v == 0 for v in comparison['error_totals'].values())
    assert comparison['whole_country_invalid_publication_cells'] == comparison['whole_country_invalid_citation_cells'] == 0
    completion_evidence = read(EVIDENCE / 'completion-verification.json')
    assert completion_evidence['status'] == 'passed' and completion_evidence['run_id'] == RUNS['all']
    gates = {'core_audit': audit_gate(core_audit, core_exit, 'core'), 'all_audit': audit_gate(all_audit, all_exit, 'all'),
             'core_browser': browser_gate(core_qa, 'core'), 'all_browser': browser_gate(all_qa, 'all')}
    candidate = probe(8901, ['core', 'all'])
    previous_health = probe(8899, ['core'])
    completions = {name: {'run_id': read(Path(directory) / 'completion.json')['run_id'],
                          'sha256': digest(Path(directory) / 'completion.json')} for name, directory in [('core', CORE), ('all', ALL)]}
    assert all(completions[c]['run_id'] == RUNS[c] for c in RUNS)
    old_env = (Path('/proc') / str(old['pid']) / 'environ').read_bytes().split(b'\0')
    gomaxprocs = next((entry.split(b'=', 1)[1].decode() for entry in old_env if entry.startswith(b'GOMAXPROCS=')), None)
    state = {'status': 'prepared', 'old': old, 'old_gomaxprocs': gomaxprocs, 'new_argv': NEW_ARGV,
             'binary_sha256': SHA, 'gates': gates, 'candidate': candidate, 'old_health': previous_health,
             'completions': completions, 'shared_lock': str(BASE / 'production-service-8899.lock')}
    persist(state)
    stopped = False
    child = identity = None
    try:
        stop(old, CORE_ARGV)
        stopped = True
        state.update(status='old_stopped')
        persist(state)
        child, identity = launch(NEW_ARGV, 'service-catalog-production.log', '4')
        state.update(status='new_loading', new=identity)
        (EVIDENCE / 'ui-8899' / 'service.pid').write_text(str(child.pid) + '\n')
        persist(state)
        health = wait_ready(child, identity, NEW_ARGV, ['core', 'all'])
        assert digest(V10) == SHA
        state.update(status='ready', health=health)
        persist(state)
        print(json.dumps(state), flush=True)
    except Exception as error:
        state.update(status='promotion_failed', error=str(error))
        persist(state)
        if child is not None and identity is not None and process(child.pid) is not None:
            rollback(state)
        elif stopped:
            child, restored = launch(CORE_ARGV, 'service-catalog-rollback-core.log', gomaxprocs)
            state.update(status='rollback_loading', rollback=restored)
            persist(state)
            state.update(status='rollback_ready', rollback_health=wait_ready(child, restored, CORE_ARGV, ['core']))
            persist(state)
        raise

if __name__ == '__main__':
    (EVIDENCE / 'ui-8899').mkdir(parents=True, exist_ok=True)
    with (BASE / 'production-service-8899.lock').open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        main()
