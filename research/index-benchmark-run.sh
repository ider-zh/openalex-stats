#!/usr/bin/env bash
set -euo pipefail
cd "$1"
export GOMAXPROCS=46
export OPENALEX_INDEX_IDS_FILE=/mnt/nvme1/backup/openalex-stats/core-goal-v1/all-ids.bin
export OPENALEX_INDEX_SAMPLE_SIZE=5000000
date -Iseconds > started.txt
go version > go-version.txt
go test -c -o graph.test ./internal/graph
/usr/bin/time -v -o correctness.resources.txt ./graph.test -test.run '^TestParallelIndexRealSample$' -test.v > correctness.txt 2>&1
for variant in serial parallel_w16_s64 parallel_w48_s512; do
  /usr/bin/time -v -o "$variant-build.resources.txt" ./graph.test -test.run '^$' -test.bench "^BenchmarkIndexBuild/$variant$" -test.benchtime=1x -test.count=1 -test.benchmem > "$variant-build.txt" 2>&1
  /usr/bin/time -v -o "$variant-lookup.resources.txt" ./graph.test -test.run '^$' -test.bench "^BenchmarkIndexLookup/$variant/(serial|concurrent)$" -test.benchtime=1s -test.count=1 -test.benchmem > "$variant-lookup.txt" 2>&1
done
date -Iseconds > completed.txt