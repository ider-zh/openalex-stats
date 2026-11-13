"""Hand off the verified listener without modifying any original result files."""
import hashlib
import json
import os
import pathlib
import signal
import time
import urllib.parse
import urllib.request

base = pathlib.Path('/home/ider/tmpdata/openalex-stats-dev-20261112/longtail-profiles-v1')
record = json.loads((base / 'production-v5-process.json').read_text())
assert record['port'] == 8899
assert record['binary_sha256'] == '13c8a92d0e564391704c59a3e0c37fecc605b9fc2232a22a37d096e8db3b3c52'
assert record['previous']['pid'] == 3547541
default = 'p1000-c1000-x5-d3-v1'

def identity(item):
    proc = pathlib.Path('/proc') / str(item['pid'])
    args = [x.decode() for x in proc.joinpath('cmdline').read_bytes().rstrip(b'\0').split(b'\0')]
    assert args == item['args']
    assert proc.joinpath('stat').read_text().rsplit(')', 1)[1].split()[19] == item['start_ticks']
    assert hashlib.sha256(pathlib.Path(args[0]).read_bytes()).hexdigest() == item['binary_sha256']

def get(path, port=8899):
    with urllib.request.urlopen(f'http://127.0.0.1:{port}' + path, timeout=20) as response:
        return json.load(response)

def metadata_by_id(value):
    # Equal display names can have several distinct subfield IDs. The source
    # metadata only promises display-name ordering, not an ID tie breaker.
    value = json.loads(json.dumps(value))
    for layer in value['layers']:
        layer['labels'].sort(key=lambda label: label['id'])
    value['layers'].sort(key=lambda layer: (layer['scheme'], layer['level']))
    return value

def owns_listener():
    sockets = {p.readlink().name for p in (pathlib.Path('/proc') / str(record['pid']) / 'fd').iterdir()}
    for table in ['tcp', 'tcp6']:
        for line in pathlib.Path('/proc/net', table).read_text().splitlines()[1:]:
            fields = line.split()
            if fields[3] == '0A' and int(fields[1].split(':')[1], 16) == 8899:
                if 'socket:[' + fields[9] + ']' in sockets:
                    return True
    return False

identity(record)
assert 'results loaded; waiting for previous listener' in (base / 'production-v5.log').read_text()
assert not (base / 'production-promotion.json').exists()
candidate = get('/api/catalog', 8898)
assert candidate['default_complexity_profile'] == default
assert len(candidate['complexity_profiles']) == 19
started = time.time()
previous_alive = (pathlib.Path('/proc') / str(record['previous']['pid'])).exists()
if previous_alive:
    identity(record['previous'])
    os.kill(record['previous']['pid'], signal.SIGTERM)
else:
    assert owns_listener(), 'resume only when the exact verified new process owns 8899'
catalog = None
for _ in range(100):
    try:
        value = get('/api/catalog')
        if value.get('default_complexity_profile') == default:
            catalog = value
            break
    except Exception:
        pass
    time.sleep(.2)
assert catalog == candidate, 'production catalog differs from tested candidate'
assert owns_listener(), 'the verified production process must own the listener'
handoff = time.time() - started
checks = []
for version in ['original', 'enriched']:
    for corpus in ['core', 'all']:
        suffix = urllib.parse.urlencode({'country_version': version, 'corpus': corpus})
        meta = get('/api/meta?' + suffix)
        assert metadata_by_id(meta) == metadata_by_id(get('/api/meta?' + suffix, 8898))
        assert meta['country_version'] == version and meta['corpus'] == corpus and meta['ready']
        assert meta['eci_default_method'] == 'java-reflections-v1'
        layer = next(layer for layer in meta['layers'] if layer['scheme'] == 'concepts' and layer['level'] == '0')
        disciplines = ','.join(label['id'] for label in sorted(layer['labels'], key=lambda label: label['id'])[:2])
        for basis in ['publications', 'citations']:
            for metric in ['publications', 'citations', 'rca', 'eci', 'pci']:
                for profile in ['baseline-v1', default]:
                    args = dict(country_version=version, corpus=corpus, scheme='concepts', level='0',
                                disciplines=disciplines,
                                countries='CN,US,SG,AG,EH,KP', metric=metric, basis=basis,
                                method='java-reflections-v1', complexity_profile=profile,
                                scenario=meta['eci_scenario_id'], start=2020, end=2025)
                    url = '/api/series?' + urllib.parse.urlencode(args)
                    value = get(url)
                    assert value == get(url, 8898), args
                    checks.append({'country_version': version, 'corpus': corpus, 'basis': basis,
                                   'metric': metric, 'profile': profile, 'series': len(value['series'])})
for asset in ['app.js', 'app.css', 'index.html', 'bump.js', 'onboarding.js']:
    asset_url = '/' if asset == 'index.html' else '/static/' + asset
    with urllib.request.urlopen('http://127.0.0.1:8899' + asset_url, timeout=10) as response:
        actual = hashlib.sha256(response.read()).hexdigest()
    assert actual == hashlib.sha256((base / 'static-expected' / asset).read_bytes()).hexdigest(), asset
result = dict(status='promoted_verified_profile_default', production=record,
              handoff_seconds_to_catalog=handoff if previous_alive else None,
              verification_resumed_after_label_order_only_comparison=True,
              metadata_comparison='exact fields with layers/labels canonicalized by stable ID; no labels discarded',
              metadata_checks=4, series_checks=checks,
              default_country_version=catalog['default_country_version'], default_complexity_profile=default,
              static_source_sha256_verified=True, original_binaries_and_results_preserved=True,
              promoted_at_unix=time.time())
(base / 'production-promotion.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({k: result[k] for k in ['status', 'handoff_seconds_to_catalog', 'metadata_checks']}))
