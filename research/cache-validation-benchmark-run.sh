#!/usr/bin/env bash
set -euo pipefail
base="$1"
cd "$base"
if test -e completed.txt; then printf '%s\n' 'Complete evidence already exists; refusing overwrite' >&2; exit 10; fi
if kill -0 869128 2>/dev/null || kill -0 871494 2>/dev/null || pgrep -x duckdb >/dev/null; then printf '%s\n' 'A heavy validation job is still active' >&2; exit 11; fi
export GOMAXPROCS=46
export CGO_ENABLED=0
export OPENALEX_FULL_CACHE_BENCH_DIRECTORY=/mnt/nvme0/public/openalex-stats/cache-20260626
export OPENALEX_FULL_CACHE_BENCH_MANIFEST=/mnt/hg02/openalex-snapshot/data/parquet/manifest.json
date -Iseconds > started.txt
ps -eo pid,comm,pcpu,rss --sort=-pcpu | head -12 > host-before.txt
go version > go-version.txt
go test -c -o validation.test ./internal/production
sha256sum source.tgz internal/production/cache_validation.go internal/production/cache_validation_full_test.go > source-files.sha256
for case in serial parallel8 parallel16 parallel24; do
 export OPENALEX_FULL_CACHE_BENCH_OUTPUT="$base/$case.json"
 if test "$case" = serial; then
  export OPENALEX_FULL_CACHE_BENCH_MODE=serial
  export OPENALEX_FULL_CACHE_BENCH_WORKERS=1
 else
  export OPENALEX_FULL_CACHE_BENCH_MODE=parallel
  export OPENALEX_FULL_CACHE_BENCH_WORKERS="${case#parallel}"
 fi
 /usr/bin/time -v -o "$case.resources.txt" ./validation.test -test.run '^TestFullCacheValidationBenchmark$' -test.v > "$case.txt" 2>&1
 date -Iseconds > "$case-completed.txt"
done
ps -eo pid,comm,pcpu,rss --sort=-pcpu | head -12 > host-after.txt
date -Iseconds > completed.txt
printf '%s\n' 'Full cache validation benchmark completed'