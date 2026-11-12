#!/usr/bin/env bash
# Launch only; never signals another process. Run after all acceptance gates.
set -euo pipefail
port=${1:?usage: serve-catalog.sh 8901-or-8899}
case "$port" in 8901|8899) ;; *) exit 64;; esac
binary=/home/ider/tmpdata/openalex-stats-dev-20261112/oa-stats-v10
expected=ac0efd9804294ed05c9f96d28a20b477d46d963e881edd836aff969e56b10fba
actual=$(sha256sum "$binary" | cut -d' ' -f1)
test "$actual" = "$expected"
core=/mnt/nvme0/public/openalex-stats/results/core-concept-hits-v1
all=/mnt/nvme0/public/openalex-stats/results/all-concept-hits-v1
test -f "$core/completion.json"
test -f "$all/completion.json"
evidence=/home/ider/tmpdata/openalex-stats-dev-20261112/all-concept-hits-v1/ui-$port
mkdir -p "$evidence"
date -u +%Y-%m-%dT%H:%M:%SZ > "$evidence/started-at.txt"
printf '%s\n' "$actual" > "$evidence/binary.sha256"
exec env GOMAXPROCS=4 "$binary" serve --data-core "$core" --data-all "$all" --listen "0.0.0.0:$port"
