#!/usr/bin/env bash
set -uo pipefail
evidence=/home/ider/tmpdata/openalex-stats-dev-20261112/all-concept-hits-v1
date -u +%Y-%m-%dT%H:%M:%SZ > "$evidence/comparison-started-at.txt"
/usr/bin/time -v -o "$evidence/comparison.resources.txt" /home/ider/.local/bin/duckdb :memory: < "$evidence/compare-core-full.sql" > "$evidence/comparison.stdout.log" 2> "$evidence/comparison.stderr.log"
status=$?
printf '%s\n' "$status" > "$evidence/comparison-exit-code.txt"
date -u +%Y-%m-%dT%H:%M:%SZ > "$evidence/comparison-completed-at.txt"
if test "$status" -eq 0; then
    python3 "$evidence/verify-comparison.py" > "$evidence/comparison-verification.stdout.log" 2> "$evidence/comparison-verification.stderr.log"
    status=$?
fi
printf '%s\n' "$status" > "$evidence/comparison-final-exit-code.txt"
exit "$status"
