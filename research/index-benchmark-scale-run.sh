#!/usr/bin/env bash
set -euo pipefail
base="$1"
output="$base/uncontended-50m-1x"
mkdir -p "$output"
if test -e "$output/completed.txt"; then printf '%s\n' 'Existing completed scale results; refusing overwrite' >&2; exit 10; fi
cd "$base"
export GOMAXPROCS=46
export OPENALEX_INDEX_IDS_FILE=/mnt/nvme1/backup/openalex-stats/core-goal-v1/all-ids.bin
export OPENALEX_INDEX_SAMPLE_SIZE=50000000
date -Iseconds > "$output/started.txt"
ps -eo pid,comm,pcpu,rss --sort=-pcpu | head -12 > "$output/host-before.txt"
set +e
command -v numactl > "$output/numactl-path.txt" 2>&1
printf '%d\n' "$?" > "$output/numactl-path-exit.txt"
numactl --interleave=all true > "$output/numactl-probe.txt" 2>&1
printf '%d\n' "$?" > "$output/numactl-probe-exit.txt"
set -e
go test -c -o graph.test ./internal/graph
sha256sum source.tgz internal/graph/graph.go internal/graph/parallel_index.go internal/graph/parallel_index_benchmark_test.go > "$output/source-files.sha256"
/usr/bin/time -v -o "$output/correctness.resources.txt" ./graph.test -test.run '^TestParallelIndexRealSample$' -test.v > "$output/correctness.txt" 2>&1
for variant in serial parallel_w16_s64 parallel_w48_s512 parallel_w32_s256; do
 prefix="$output/$variant"
 /usr/bin/time -v -o "$prefix-build.resources.txt" ./graph.test -test.run '^$' -test.bench "^BenchmarkIndexBuild/$variant$" -test.benchtime=1x -test.count=1 -test.benchmem > "$prefix-build.txt" 2>&1
 /usr/bin/time -v -o "$prefix-lookup.resources.txt" ./graph.test -test.run '^$' -test.bench "^BenchmarkIndexLookup/$variant/(serial|concurrent)$" -test.benchtime=1s -test.count=1 -test.benchmem > "$prefix-lookup.txt" 2>&1
done
ps -eo pid,comm,pcpu,rss --sort=-pcpu | head -12 > "$output/host-after.txt"
date -Iseconds > "$output/completed.txt"
printf '%s\n' 'Uncontended 50m scale benchmark completed'