"""Record exact catalog process, served run IDs, and frozen executable identity."""
import datetime
import json
import os
from pathlib import Path
import runpy
import sys
import time

base = Path('/home/ider/tmpdata/openalex-stats-dev-20261112/all-concept-hits-v1')
helpers = runpy.run_path(str(base / 'promote-catalog.py'))
port = int(sys.argv[1])
assert port in (8899, 8901)
folder = base / f'ui-{port}'
pid = int((folder / 'service.pid').read_text())
argv = [helpers['V10'], 'serve', '--data-core', helpers['CORE'], '--data-all', helpers['ALL'], '--listen', f'0.0.0.0:{port}']

if sys.argv[2:] == ['--stop']:
    assert port == 8901, 'cleanup applies only to the owned temporary dual-corpus candidate'
    identity = json.loads((folder / 'service-identity.json').read_text())['process']
    assert identity['pid'] == pid
    helpers['stop'](identity, argv)
    (folder / 'stopped-at.json').write_text(json.dumps({'status': 'stopped', 'process': identity,
        'stopped_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat()}, indent=2) + '\n')
    print(json.dumps({'status': 'stopped', 'pid': pid, 'port': port}))
    sys.exit(0)

deadline = time.monotonic() + 900
last = None
while time.monotonic() < deadline:
    identity = helpers['process'](pid)
    if identity is None:
        raise RuntimeError('catalog service process no longer exists')
    assert identity['exe'] == helpers['V10'] and identity['argv'] == argv
    assert helpers['digest'](identity['exe']) == helpers['SHA']
    try:
        health = helpers['probe'](port, ['core', 'all'])
        break
    except Exception as error:
        last = str(error)
    time.sleep(2)
else:
    raise RuntimeError('catalog readiness timeout: ' + str(last))

summary = {'status': 'passed', 'port': port, 'process': identity,
    'verified_at_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
    'binary_sha256': helpers['SHA'], 'health': health}
(folder / 'service-identity.json').write_text(json.dumps(summary, indent=2) + '\n')
print(json.dumps(summary))
