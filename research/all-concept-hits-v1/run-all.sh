#!/usr/bin/env bash
set -euo pipefail
if [ "$#" -ne 2 ]; then
  printf 'usage: run-all.sh /absolute/frozen/oa-stats SHA256\n' >&2
  exit 2
fi
binary="$1"
expected_sha="$2"
case "$binary" in
  /home/ider/tmpdata/openalex-stats-dev-20261112/oa-stats-*) ;;
  *) printf 'Refusing unexpected executable location\n' >&2; exit 2 ;;
esac
actual_sha="$(sha256sum "$binary" | cut -d ' ' -f 1)"
if [ "$actual_sha" != "$expected_sha" ]; then
  printf 'Frozen executable SHA mismatch\n' >&2
  exit 3
fi
evidence=/home/ider/tmpdata/openalex-stats-dev-20261112/all-concept-hits-v1
work=/mnt/nvme1/backup/openalex-stats/all-concept-hits-v1
counters=/mnt/nvme2/public/openalex-stats/all-concept-hits-counts
output=/mnt/nvme0/public/openalex-stats/results/all-concept-hits-v1
mkdir -p "$evidence" "$work" "$counters" "$output"
printf '%s\n' "$binary" > "$evidence/binary-path.txt"
printf '%s\n' "$actual_sha" > "$evidence/binary.sha256"
date -u +%FT%TZ > "$evidence/started-at.txt"
date +%s.%N > "$evidence/started-unix.txt"
free -b > "$evidence/memory-before.txt"
df -B1 "$work" "$counters" "$output" > "$evidence/storage-before.txt"
"$binary" compute \
  --manifest /mnt/hg02/openalex-snapshot/data/parquet/manifest.json \
  --cache /mnt/nvme0/public/openalex-stats/cache-20260626 \
  --work "$work" \
  --edge-cache "$work/edges" \
  --counters "$counters" \
  --output "$output" \
  --corpus all --corpus-year-start 0 --year-start 2000 --year-end 2026 \
  --workers 14 --readers 6 --edge-workers 14 --count-workers 12 \
  --index-workers 14 --index-shards 256 --partitions 64 \
  --memory-gib 320 --host-available-floor-gib 64 \
  > "$evidence/compute.stdout.json" 2> "$evidence/compute.stderr.log" &
child="$!"
printf '%s\n' "$child" > "$evidence/compute.pid"
if [ -r "/proc/$child/stat" ]; then
  cat "/proc/$child/stat" > "$evidence/process-stat-at-start.txt"
  readlink "/proc/$child/exe" > "$evidence/process-exe-at-start.txt"
fi
getconf CLK_TCK > "$evidence/clock-ticks-per-second.txt"
sed -n '/^btime /p' /proc/stat > "$evidence/kernel-boot-time.txt"
trap 'kill -TERM "$child" 2>/dev/null || true' TERM INT
set +e
wait "$child"
result="$?"
set -e
printf '%s\n' "$result" > "$evidence/exit-code.txt"
date -u +%FT%TZ > "$evidence/completed-at.txt"
date +%s.%N > "$evidence/completed-unix.txt"
free -b > "$evidence/memory-after.txt"
df -B1 "$work" "$counters" "$output" > "$evidence/storage-after.txt"
exit "$result"

