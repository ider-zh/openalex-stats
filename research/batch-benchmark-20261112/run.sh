#!/usr/bin/env bash
set -euo pipefail
base="$1"
cd "$base"
if test -e completed.txt; then echo 'Refusing to overwrite completed evidence' >&2; exit 10; fi
export GOMAXPROCS=46 CGO_ENABLED=0
manifest=/mnt/nvme0/public/openalex-stats/bench-cache-4/manifest.json
date -Iseconds > started.txt
ps -eo pid,comm,pcpu,rss --sort=-pcpu | head -12 > host-before.txt
go version > go-version.txt
cp "$manifest" input-manifest.json
python3 - <<'PY' > input-sha256.txt
import hashlib,json
m=json.load(open('input-manifest.json'))
assert m['complete'] and len(m['parts'])==4 and m['rows']==1434610
for p in m['parts']:
 h=hashlib.sha256()
 with open(p['path'],'rb') as f:
  for b in iter(lambda:f.read(1024*1024),b''):h.update(b)
 assert h.hexdigest()==p['sha256'],p['path']
 print(h.hexdigest(),p['path'])
PY
cp internal/cache/scan.go scan.baseline.go.txt
sha256sum source.tgz cmd/batchbench/main.go > source-files.sha256
for batch in 32 128 512 2048; do
 python3 - "$batch" <<'PY'
import sys
s=open('scan.baseline.go.txt').read()
needle='batch := make([]normalizedRow, 128)'
assert s.count(needle)==1
open('internal/cache/scan.go','w').write(s.replace(needle,'batch := make([]normalizedRow, '+sys.argv[1]+')'))
PY
 diff -u --label baseline-scan.go --label batch-scan.go scan.baseline.go.txt internal/cache/scan.go > "batch-$batch.patch" || test "$?" = 1
 sha256sum internal/cache/scan.go "batch-$batch.patch" > "batch-$batch.source.sha256"
 go build -o "batch-$batch" ./cmd/batchbench
done
./batch-128 "$manifest" warmup.json 128 0 > warmup.txt
# Rotate first/last position to reduce a consistent ordering advantage.
for run in 1 2 3; do
 case "$run" in 1) order='32 128 512 2048';; 2) order='512 2048 32 128';; 3) order='2048 512 128 32';; esac
 for batch in $order; do
  name="batch-$batch-run-$run"
  /usr/bin/time -v -o "$name.resources.txt" "./batch-$batch" "$manifest" "$name.json" "$batch" "$run" > "$name.txt" 2>&1
 done
done
python3 - <<'PY' > verification.txt
import glob,json
reports=[json.load(open(p)) for p in glob.glob('batch-*-run-*.json')]
assert len(reports)==12
first=reports[0]
for r in reports:
 assert r['totals']==first['totals'],r
 assert r['stats']==first['stats'],r
print('PASS: all 12 complete scans have identical ordered full-field SHA256, totals and scan Stats')
print(json.dumps(first['totals'],sort_keys=True))
PY
ps -eo pid,comm,pcpu,rss --sort=-pcpu | head -12 > host-after.txt
date -Iseconds > completed.txt
echo 'Batch benchmark completed'
