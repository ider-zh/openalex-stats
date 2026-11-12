"""Verify deployed corpus routing against the exact immutable completion hashes."""
import hashlib
import json
import pathlib
import sys
import urllib.request

port = int(sys.argv[1])
assert port in (8901, 8899)
base = f'http://127.0.0.1:{port}'
root = pathlib.Path('/mnt/nvme0/public/openalex-stats/results')
evidence = pathlib.Path('/home/ider/tmpdata/openalex-stats-dev-20261112/all-concept-hits-v1') / f'ui-{port}'

def get(path, headers=None):
    with urllib.request.urlopen(urllib.request.Request(base + path, headers=headers or {}), timeout=120) as response:
        return response.read(), dict(response.headers), response.status

catalog = json.loads(get('/api/catalog')[0])
assert catalog['default_corpus'] == 'core'
assert [d['id'] for d in catalog['datasets']] == ['core', 'all']
checks = []
for corpus, expected_q in [('core', 269194661), ('all', 404079112)]:
    output = root / f'{corpus}-concept-hits-v1'
    completion = json.loads((output / 'completion.json').read_text())
    meta = json.loads(get('/api/meta?corpus=' + corpus)[0])
    assert meta['corpus'] == corpus and meta['scope']['corpus'] == corpus
    assert meta['ready'] and meta['status'] == 'full_snapshot_complete'
    assert meta['selected_works'] == expected_q
    dataset = next(d for d in catalog['datasets'] if d['id'] == corpus)
    assert dataset['selected_works'] == expected_q
    downloads = []
    # Exact bytes prove download isolation, even where both corpora share schema.
    for name in ['run.json', 'coverage.json', 'model_diagnostics.json']:
        artifact = next(a for a in [completion['run'], *completion['artifacts']] if pathlib.Path(a['path']).name == name)
        data, headers, status = get(f'/download?file={name}&corpus={corpus}')
        digest = hashlib.sha256(data).hexdigest()
        assert status == 200 and len(data) == artifact['bytes'] and digest == artifact['sha256']
        assert 'attachment' in headers.get('Content-Disposition', '')
        downloads.append({'file': name, 'bytes': len(data), 'sha256': digest})
    data, headers, status = get(f'/download?file=cube.parquet&corpus={corpus}', {'Range': 'bytes=0-3'})
    assert status == 206 and data == b'PAR1'
    cube = next(a for a in completion['artifacts'] if pathlib.Path(a['path']).name == 'cube.parquet')
    assert headers['Content-Range'] == f"bytes 0-3/{cube['bytes']}"
    # Read the actual served Parquet too; identical schemas/signatures alone do not prove corpus isolation.
    cube_digest = hashlib.sha256()
    cube_bytes = 0
    with urllib.request.urlopen(base + f'/download?file=cube.parquet&corpus={corpus}', timeout=120) as stream:
        while block := stream.read(1 << 20):
            cube_digest.update(block)
            cube_bytes += len(block)
    assert cube_bytes == cube['bytes'] and cube_digest.hexdigest() == cube['sha256']
    downloads.append({'file': 'cube.parquet', 'bytes': cube_bytes, 'sha256': cube_digest.hexdigest()})
    checks.append({'corpus': corpus, 'run_id': completion['run_id'], 'selected_works': expected_q,
                   'completion_sha256': hashlib.sha256((output / 'completion.json').read_bytes()).hexdigest(),
                   'downloads': downloads, 'parquet_range_status': status})
default_run = get('/download?file=run.json')[0]
assert hashlib.sha256(default_run).hexdigest() == checks[0]['downloads'][0]['sha256']
summary = {'status': 'passed', 'base_url': base, 'catalog': catalog, 'checks': checks,
           'default_download_corpus': 'core'}
(evidence / 'catalog-download-isolation.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')
print(json.dumps(summary, ensure_ascii=False))
