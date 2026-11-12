"""Read-only verification of the dedicated all-result completion, not an oracle audit."""
import collections
import hashlib
import json
import pathlib
import time

evidence = pathlib.Path('/home/ider/tmpdata/openalex-stats-dev-20261112/all-concept-hits-v1')
output = pathlib.Path('/mnt/nvme0/public/openalex-stats/results/all-concept-hits-v1')
assert (evidence / 'exit-code.txt').read_text().strip() == '0'
pid = int((evidence / 'compute.pid').read_text())
assert not pathlib.Path(f'/proc/{pid}').exists(), 'compute PID still exists'

def read(name):
    return json.loads((output / name).read_text())

def sha256(path):
    h = hashlib.sha256()
    with open(path, 'rb') as stream:
        while block := stream.read(8 << 20):
            h.update(block)
    return h.hexdigest()

completion = read('completion.json')
run = read('run.json')
assert completion['run_id'] == run['run_id']
assert run['status'] == 'full_snapshot_complete'
assert run['source_rows'] == 510372821
assert run['selected_works'] == 404079112
assert run['config']['scope']['corpus'] == 'all'
assert run['config']['scope']['corpus_year_start'] == 0
assert run['config']['year_start'] == 2000 and run['config']['year_end'] == 2026
assert not run['config']['include_work_self_loops']
assert run['models'] == 216
assert all(run['validation'].values()), run['validation']
artifacts = []
for item in [completion['run'], *completion['artifacts']]:
    p = pathlib.Path(item['path'])
    assert p.parent == output, p
    actual = {'path': str(p), 'bytes': p.stat().st_size, 'sha256': sha256(p)}
    assert actual == item, (actual, item)
    artifacts.append(actual)

scenarios = read('scenarios.json')
assert len(scenarios) == 6
assert all(s['concept_mode'] == 'whole' for s in scenarios)
diagnostics = read('model_diagnostics.json')
assert len(diagnostics) == 216
model_keys = {(d['year'], d['scheme'], d['level'], d['metric']) for d in diagnostics}
assert len(model_keys) == 216
assert {d['year'] for d in diagnostics} == set(range(2000, 2027))
assert {d['metric'] for d in diagnostics} == {'publications', 'citations'}
assert {(d['scheme'], d['level']) for d in diagnostics} == {
    ('topics', 'field'), ('topics', 'subfield'), ('concepts', '0'), ('concepts', '1')}
assert {d['scenario_id'] for d in diagnostics} == {'fractional-concept-whole-0.3'}
binary = pathlib.Path('/home/ider/tmpdata/openalex-stats-dev-20261112/oa-stats-v9')
source = binary.parent / 'source-v9-compute.tar.gz'
assert sha256(binary) == '8be7a2e15906ac7f3c80b1b752395d8b9499ddd871077e63e550d87fefea01a6'
assert sha256(source) == '33b93a34feba8a0c2d45a29a6c3254975468745c20a42b51ab49058b886a9d2b'
resources = {k: v for k, v in run['resources'].items() if k != 'trace'}
gnu_wall = next(line.rsplit(': ', 1)[1] for line in (evidence / 'runner.resources.txt').read_text().splitlines()
                if 'Elapsed (wall clock)' in line)
gnu_seconds = sum(float(value) * 60**index for index, value in enumerate(reversed(gnu_wall.split(':'))))
summary = {
    'status': 'passed', 'verified_unix': time.time(), 'compute_pid': pid,
    'scope': 'completion hashes and run invariants; independent raw oracle audit recorded separately',
    'run_id': run['run_id'], 'output': str(output),
    'binary_sha256': sha256(binary), 'source_archive_sha256': sha256(source),
    'external_elapsed_seconds_kernel_approx': float((evidence / 'completed-unix.txt').read_text()) - 1794442586.76,
    'kernel_boot_clock_resolution_seconds': 1,
    'gnu_time_wall_seconds': gnu_seconds,
    'source_rows': run['source_rows'], 'selected_works': run['selected_works'],
    'graph': run['graph'], 'quality': run['quality'], 'validation': run['validation'],
    'dimension_reused': run['dimension_reused'], 'edge_parts_reused': run['edge_parts_reused'],
    'count_partitions_reused': run['count_partitions_reused'],
    'cube_rows': run['cube_rows'], 'statistics_rows': run['statistics_rows'],
    'models': run['models'], 'model_status_counts': dict(collections.Counter(d['status'] for d in diagnostics)),
    'stage_seconds': run['stage_seconds'], 'resources': resources,
    'scenarios': scenarios, 'artifacts': artifacts,
}
(evidence / 'completion-verification.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
print(json.dumps({k: v for k, v in summary.items() if k not in {'artifacts', 'resources', 'quality', 'scenarios'}}, ensure_ascii=False))
