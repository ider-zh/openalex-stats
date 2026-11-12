#!/usr/bin/env bash
set -euo pipefail
base="$1"
output="$base/uncontended-5m-3x"
mkdir -p "$output"
if test -e "$output/completed.txt"; then printf '%s\n' 'Existing complete results; refusing overwrite' >&2; exit 10; fi
if kill -0 858720 2>/dev/null; then printf '%s\n' 'Audit still running' >&2; exit 11; fi
if ! grep -q listening /home/ider/tmpdata/openalex-stats-dev-20261112/web-formal-v6.log; then printf '%s\n' 'Web not ready' >&2; exit 12; fi
cd "$base"
export GOMAXPROCS=46
export OPENALEX_INDEX_IDS_FILE=/mnt/nvme1/backup/openalex-stats/core-goal-v1/all-ids.bin
export OPENALEX_INDEX_SAMPLE_SIZE=5000000
date -Iseconds > "$output/started.txt"
ps -eo pid,comm,pcpu,rss --sort=-pcpu | head -12 > "$output/host-before.txt"
pidstat -u -p 861375 1 1 > "$output/web-idle-check.txt"
for round in 1 2 3; do
 case "$round" in
  1) variants='serial parallel_w16_s64 parallel_w48_s512 parallel_w32_s256';;
  2) variants='parallel_w16_s64 parallel_w48_s512 parallel_w32_s256 serial';;
  3) variants='parallel_w48_s512 parallel_w32_s256 serial parallel_w16_s64';;
 esac
 for variant in $variants; do
  prefix="$output/round-$round-$variant"
  /usr/bin/time -v -o "$prefix-build.resources.txt" ./graph.test -test.run '^$' -test.bench "^BenchmarkIndexBuild/$variant$" -test.benchtime=1x -test.count=1 -test.benchmem > "$prefix-build.txt" 2>&1
  /usr/bin/time -v -o "$prefix-lookup.resources.txt" ./graph.test -test.run '^$' -test.bench "^BenchmarkIndexLookup/$variant/(serial|concurrent)$" -test.benchtime=1s -test.count=1 -test.benchmem > "$prefix-lookup.txt" 2>&1
 done
 date -Iseconds > "$output/round-$round-completed.txt"
done
ps -eo pid,comm,pcpu,rss --sort=-pcpu | head -12 > "$output/host-after.txt"
date -Iseconds > "$output/completed.txt"
printf '%s\n' 'Uncontended 5m three-round benchmark completed'