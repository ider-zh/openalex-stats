#!/usr/bin/env bash
set -euo pipefail
base="$1"
cd "$base"
test ! -e completed.txt
export GOMAXPROCS=46 CGO_ENABLED=0
export OPENALEX_INDEX_IDS_FILE=/mnt/nvme1/backup/openalex-stats/core-goal-v1/all-ids.bin
export OPENALEX_INDEX_SAMPLE_SIZE=50000000
date -Iseconds > started.txt
go version > go-version.txt
sha256sum graph.test numa-wrapper-v2 numa_wrapper_linux.go.txt run.sh exec-case.sh > source-files.sha256
ps -eo pid,comm,pcpu,rss --sort=-pcpu | head -12 > host-before.txt
cat /sys/devices/system/node/online > numa-nodes.txt
./numa-wrapper-v2 --get-policy > default-policy-probe.json
./numa-wrapper-v2 "$base/numa-wrapper-v2" --get-policy > interleave-policy-probe.json
for run in 1 2; do
 if test "$run" = 1; then order='default interleave'; else order='interleave default'; fi
 for policy in $order; do
  for kind in build lookup; do
   name="$policy-$kind-run-$run"
   cmd=(/bin/sh "$base/exec-case.sh" "$base" "$name" "$kind")
   if test "$policy" = interleave; then cmd=("$base/numa-wrapper-v2" "${cmd[@]}"); fi
   /usr/bin/time -v -o "$name.resources.txt" "${cmd[@]}" > "$name.txt" 2>&1 &
   timepid=$!
   for attempt in $(seq 1 100); do
    test -e "$name.pid" && break
    kill -0 "$timepid" 2>/dev/null || break
    sleep 0.02
   done
   test -e "$name.pid"
   benchpid=$(cat "$name.pid")
   mkdir "$name.maps"
   for sample in $(seq 1 600); do
    if ! test -r "/proc/$benchpid/numa_maps"; then break; fi
    comm=$(cat "/proc/$benchpid/comm" 2>/dev/null || true)
    # Retain several snapshots of the actual Go benchmark, not just its shell.
    if test "$comm" = graph.test && test "$((sample % 5))" = 0; then
     cat "/proc/$benchpid/numa_maps" > "$name.maps/sample-$sample.txt" || true
     cat "/proc/$benchpid/status" > "$name.maps/status-$sample.txt" || true
    fi
    sleep 0.2
   done
   wait "$timepid"
  done
 done
done
ps -eo pid,comm,pcpu,rss --sort=-pcpu | head -12 > host-after.txt
date -Iseconds > completed.txt
tar -czf numa-maps.tgz *.maps
echo 'NUMA benchmark completed'
