"""Reload the same frozen binary after its first standby wait expired."""
import hashlib
import json
import pathlib
import subprocess
import time

base = pathlib.Path('/home/ider/tmpdata/openalex-stats-dev-20261112/longtail-profiles-v1')
prior = json.loads((base / 'production-v3-process.json').read_text())
assert prior['pid'] == 3595376 and not pathlib.Path('/proc/3595376').exists()
previous = prior['previous']
proc = pathlib.Path('/proc') / str(previous['pid'])
assert [x.decode() for x in proc.joinpath('cmdline').read_bytes().rstrip(b'\0').split(b'\0')] == previous['args']
assert proc.joinpath('stat').read_text().rsplit(')', 1)[1].split()[19] == previous['start_ticks']
args = prior['args'].copy()
assert hashlib.sha256(pathlib.Path(args[0]).read_bytes()).hexdigest() == prior['binary_sha256']
args[args.index('--wait-for-listen') + 1] = '15m'
with (base / 'production-v5.log').open('xb') as log:
    process = subprocess.Popen(args, stdin=subprocess.DEVNULL, stdout=log, stderr=subprocess.STDOUT, start_new_session=True)
record = dict(pid=process.pid, args=args, port=8899, started_at_unix=time.time(),
              start_ticks=pathlib.Path(f'/proc/{process.pid}/stat').read_text().rsplit(')', 1)[1].split()[19],
              binary_sha256=prior['binary_sha256'], previous=previous,
              previous_standby_expired_without_changing_live_service=prior['pid'])
(base / 'production-v5-process.json').write_text(json.dumps(record, indent=2) + '\n')
print(json.dumps({'standby_pid': process.pid, 'unchanged_live_pid': previous['pid'], 'binary_sha256': record['binary_sha256']}))
