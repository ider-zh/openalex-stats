# Full normalized-cache SHA validation benchmark

Completed on server 229, 2026-11-12 03:24:51–03:27:04 +08:00. Each mode validated all 2,446 cache files, 510,372,821 source rows and 31,900,483,518 cache bytes. The old serial implementation and the parallel implementation produced the same complete ordered manifest fingerprint:

`0494c06b03ac97eea4deae6c9c22dce37ef69992e1cf3f64ea6c5b67c88057d2`

All modes passed the full SHA256 checks, raw-source size/mtime checks, row-count check and input-order comparison. This benchmark did not replace content validation with metadata checks.

| Mode | Elapsed seconds | Speedup | GiB/s | Whole-process CPU | Peak RSS MiB | Mean machine CPU |
|---|---:|---:|---:|---:|---:|---:|
| Serial | 94.348 | 1.00× | 0.315 | 100% | 22.30 | 2.10% |
| 8 workers | 13.362 | 7.06× | 2.223 | 792% | 29.78 | 16.64% |
| 16 workers | 6.833 | 13.81× | 4.348 | 1565% | 37.19 | 33.10% |
| 24 workers | 5.079 | 18.58× | 5.850 | 2324% | 45.16 | 49.42% |

Use **24 validation workers** for this server under the total 46-CPU budget. It was the fastest measured candidate, improving elapsed time by a further 25.7% over 16 workers with about 8 MiB more peak RSS. Keep the worker setting configurable; the implementation caps it by GOMAXPROCS and file count. Each worker owns one reusable 1 MiB read buffer and a SHA256 state.

This is a warm-page-cache measurement: every mode recorded zero physical read bytes while reading the complete logical cache payload. Cold-storage throughput may limit scaling. Each complete-data case ran once in serial, 8, 16, 24 order; these results do not establish run-to-run variance. GOMAXPROCS was 46 on a 48-logical-CPU host. GNU time CPU uses 100% per logical CPU for the whole subprocess; the stage trace reports process CPU normalized to 0–100% across the machine. RSS in the table is GNU time's process maximum; stage peaks are sampled and can be lower. The stage mean uses aggregate process/machine tick deltas, including the final shorter interval.

The production replay and DuckDB comparison had finished before this run. Root held the new web process until benchmarking finished; the existing web process was idle. No production binary, source snapshot, normalized-cache content or statistics output was changed. Remote execution used an independent directory:

`/mnt/nvme0/public/openalex-stats/cache-validation-benchmark-20261112-b7a06be80f`

Raw evidence is retained beside this file: each mode's JSON contains manifest comparison, timing and CPU/GC/IO samples; `.resources.txt` contains GNU time output; `.txt` contains test output. `source-files.sha256`, host snapshots, Go version and start/completion timestamps identify the run. `summary.json` is the machine-readable comparison. The opt-in test is `internal/production/cache_validation_full_test.go`; ordinary `go test` skips the full scan unless its benchmark environment variables are supplied. The launcher is `research/cache-validation-benchmark-run.sh`.

No further performance experiments were started after this run.
