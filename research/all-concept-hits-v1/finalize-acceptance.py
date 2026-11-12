"""Publish an evidence summary only after the real dual-corpus production gates pass."""
import datetime
import json
from pathlib import Path
import runpy

base = Path('/home/ider/tmpdata/openalex-stats-dev-20261112/all-concept-hits-v1')
h = runpy.run_path(str(base / 'promote-catalog.py'))
read, digest = h['read'], h['digest']
promotion = read(base / 'production-catalog-promotion.json')
assert promotion['status'] == 'ready'
production = h['exact'](promotion['new'], h['NEW_ARGV'])
assert digest(production['exe']) == h['SHA']
health = h['probe'](8899, ['core', 'all'])
isolation = read(base / 'ui-8899' / 'catalog-download-isolation.json')
assert isolation['status'] == 'passed' and isolation['base_url'] == 'http://127.0.0.1:8899'
qa = []
for port, kind in [(8901, 'candidate'), (8899, 'production')]:
    for corpus in ['core', 'all']:
        path = base / f'ui-{port}' / f'{corpus}-browser-qa.json'
        report = read(path)
        assert report['status'] == 'passed' and report['mode'] == 'formal' and 'asset_override' not in report
        assert report['url'].rstrip('/') == f'http://192.168.1.229:{port}'
        assert report['dataset']['corpus'] == corpus and report['dataset']['status'] == 'full_snapshot_complete'
        assert not report['errors'] and not report['requests_outside_service']
        assert all(c['pass'] for c in report['checks'])
        assert any(c['name'] == 'core_all_one_click_preserves_filters_and_routes_actual_dataset' for c in report['checks'])
        qa.append({'kind': kind, 'corpus': corpus, 'path': str(path), 'sha256': digest(path),
                   'checks': len(report['checks']), 'completed_at': report['completed_at']})
audits = []
for corpus in ['core', 'all']:
    report = f'/mnt/nvme1/backup/openalex-stats/{corpus}-concept-hits-audit-v1/audit.json'
    exitfile = f'/home/ider/tmpdata/openalex-stats-dev-20261112/{corpus}-concept-hits-audit-v1/exit-code.txt'
    gate = h['audit_gate'](report, exitfile, corpus)
    gate['checks'] = len(read(report)['checks'])
    gate['model_checks'] = read(report)['model_checks']
    audits.append(gate)
compute = read(base / 'completion-verification.json')
comparison = read(base / 'comparison-summary.json')
assert compute['status'] == comparison['status'] == 'passed'
assert compute['run_id'] == h['RUNS']['all'] and all(v == 0 for v in comparison['error_totals'].values())
cleanup = read(base / 'ui-8901' / 'stopped-at.json')
assert cleanup['status'] == 'stopped' and h['process'](cleanup['process']['pid']) is None
core_candidate = h['process'](963184)
assert core_candidate and core_candidate['exe'] == h['V10']
assert core_candidate['argv'] == [h['V10'], 'serve', '--data-core', h['CORE'], '--listen', '0.0.0.0:8900']
assert h['probe'](8900, ['core'])['datasets']['core']['run_id'] == h['RUNS']['core']
summary = {'status': 'passed', 'completed_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    'url': 'http://192.168.1.229:8899', 'default_corpus': 'core', 'available_corpora': ['core', 'all'],
    'service_binary_sha256': h['SHA'], 'production': production, 'health': health,
    'completions': promotion['completions'], 'compute': {'run_id': compute['run_id'],
        'source_rows': compute['source_rows'], 'selected_works': compute['selected_works'],
        'publication_works': compute['quality']['publication_works'], 'retained_edges': compute['graph']['retained_edges'],
        'models': compute['models'], 'model_status_counts': compute['model_status_counts'],
        'gnu_time_wall_seconds': compute['gnu_time_wall_seconds'], 'binary_sha256': compute['binary_sha256'],
        'source_archive_sha256': compute['source_archive_sha256']},
    'independent_audits': audits, 'comparison': comparison, 'browser_qa': qa,
    'production_download_isolation': isolation,
    'core_candidate_preserved': core_candidate, 'temporary_dual_candidate_cleanup': cleanup,
    'rollback_needed': False, 'shared_lock': promotion['shared_lock']}
(base / 'final-acceptance.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({'status': summary['status'], 'pid': production['pid'], 'url': summary['url'],
                  'audits': [a['checks'] for a in audits], 'browser_checks': [q['checks'] for q in qa]}, ensure_ascii=False))
