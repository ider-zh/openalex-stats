#!/usr/bin/env sh
set -eu
base="$1"
name="$2"
kind="$3"
printf '%s\n' "$$" > "$base/$name.pid"
cat "/proc/$$/numa_maps" > "$base/$name.pre-exec.numa_maps.txt"
"$base/numa-wrapper-v2" --get-policy > "$base/$name.get-policy.json"
case "$kind" in
 build) exec "$base/graph.test" -test.run '^$' -test.bench '^BenchmarkIndexBuild/parallel_w32_s256$' -test.benchtime=1x -test.count=1;;
 lookup) exec "$base/graph.test" -test.run '^$' -test.bench '^BenchmarkIndexLookup/parallel_w32_s256/(serial|concurrent)$' -test.benchtime=1s -test.count=1;;
 *) exit 12;;
esac
