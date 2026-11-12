#!/usr/bin/env bash
set -Eeuo pipefail
base=/home/ider/tmpdata/openalex-stats-dev-20261112
binary="$base/oa-stats-v9"
evidence="$base/all-concept-hits-audit-v1"
work=/mnt/nvme1/backup/openalex-stats/all-concept-hits-audit-v1
output=/mnt/nvme0/public/openalex-stats/results/all-concept-hits-v1
expected=8be7a2e15906ac7f3c80b1b752395d8b9499ddd871077e63e550d87fefea01a6
mkdir -p "$evidence"
test ! -e "$evidence/exit-code.txt"
finish() {
 code="$?"
 trap - EXIT
 printf '%s\n' "$code" > "$evidence/exit-code.txt"
 date -u +%FT%TZ > "$evidence/completed-at.txt"
 free -b > "$evidence/memory-after.txt"
 exit "$code"
}
trap finish EXIT
printf '%s\n' "$$" > "$evidence/wrapper.pid"
test "$(cat "$base/all-concept-hits-v1/exit-code.txt")" = 0
test "$(cat "$base/core-concept-hits-audit-v1/exit-code.txt")" = 0
if kill -0 958654 2>/dev/null; then printf 'All process has not terminated\n' >&2; exit 3; fi
actual=$(sha256sum "$binary" | cut -d ' ' -f 1)
test "$actual" = "$expected"
printf '%s\n' "$actual" > "$evidence/binary.sha256"
available=$(awk '/^MemAvailable:/{printf "%.0f",$2*1024}' /proc/meminfo)
test "$available" -ge 68719476736
free -b > "$evidence/memory-before.txt"
df -B1 /mnt/nvme1/backup/openalex-stats > "$evidence/storage-before.txt"
date -u +%FT%TZ > "$evidence/started-at.txt"
export GOMAXPROCS=42
/usr/bin/time -v -o "$evidence/resources.txt" "$binary" audit \
 --data "$output" \
 --cache /mnt/nvme0/public/openalex-stats/cache-20260626 \
 --work "$work" \
 --duckdb /home/ider/.local/bin/duckdb \
 --threads 42 --memory-limit 384GB \
 > "$evidence/audit.stdout.json" 2> "$evidence/audit.stderr.log"
