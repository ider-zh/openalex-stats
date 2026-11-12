# Normalized-cache scan batch comparison

Completed on server 229, 2026-11-12 03:44:31–03:48:00 +08:00. This last acceptance experiment compared Parquet read batches of 32, 128 (production baseline), 512 and 2048 rows. It used the same four real `bench-cache-4` files, with both dimensions and references enabled, four readers and GOMAXPROCS=46. Each candidate scanned all 1,434,610 rows three times in a separate process. Production sources, the v8 binary and the default batch size were not changed.

| Batch rows | Median seconds | Range seconds | Median rows/s | Mean process CPU | Maximum RSS MiB |
|---|---:|---:|---:|---:|---:|
| 32 | 14.044 | 13.881–14.168 | 102,151 | 138.50% | 48.97 |
| 128 | 18.930 | 18.834–18.954 | 75,784 | 121.18% | 51.59 |
| 512 | 15.014 | 14.971–15.210 | 95,552 | 121.16% | 62.84 |
| 2048 | 13.381 | 13.220–13.423 | 107,213 | 126.67% | 96.97 |

Batch 2048 was fastest in this experiment: median elapsed time was 29.32% lower than batch 128, corresponding to 41.47% higher throughput. Batch 32 used about half batch 2048's maximum RSS; batch 2048 reduced elapsed time by only 4.72% relative to batch 32. The difference from batch 128 persisted across the three rotated runs. These results support treating batch size as a material tuning parameter, but do not establish a new production default.

Every scan produced exactly the same full-field ordered SHA256:

`2e04815e8704a98874dffe8b56d979aa5d544d2d400a7eba3266f316553ba7b2`

All 12 scans agreed on the following totals and complete scan Stats:

| Value | Count |
|---|---:|
| Rows read and yielded | 1,434,610 |
| Reference entries | 10,853,745 |
| Country entries | 1,436,748 |
| Concept level 0 entries | 3,414,682 |
| Concept level 1 entries | 3,394,727 |
| Rows with Field / Subfield | 863,977 / 863,977 |
| Missing years | 3,788 |
| Invalid references / concepts / null XPAC | 0 / 0 / 0 |

The callback hashes every Work scalar, all strings with explicit length framing, country/concept/reference list lengths and entries, exact concept score bits and all per-row quality flags. Thus the check includes row order and all projected content, not just aggregate totals. Each input file's full SHA256 was separately verified against its cache manifest before running the candidates. The input is 90,342,117 compressed bytes; it is the original four-file pilot, not a full snapshot scan.

The only candidate source difference was the literal in `batch := make([]normalizedRow, 128)` inside the independent copy of `internal/cache/scan.go`. The 128 patch is intentionally empty. Each candidate was built into a distinct binary. Candidate patches and source/patch SHA256 files are retained here, along with the source archive hash and exact helper source. The independent remote directory is recorded in `session.json`.

One untimed batch-128 warmup preceded the measured scans. Candidate order was `32,128,512,2048`, then `512,2048,32,128`, then `2048,512,128,32`. All measured scans had zero physical read-byte deltas and zero swaps, so these are warm-page-cache results. Source decoding, Work materialization, ordered callback delivery, full-content serialization and hashing are included in elapsed time. They are not isolated decoder measurements or a direct prediction of the production pipeline's per-file parallel scans. The unchanged cache.Scan ordered callback can serialize useful work despite four configured readers; the reported CPU use is consequently about 1.2–1.4 logical CPUs, and no claim of 90% machine utilization is made for this experiment. The existing v8 web process remained present; no audit or compute job was started by this benchmark.

Process CPU is measured with Getrusage deltas over each timed scan (100% means one logical CPU), averaged across the three runs. Maximum RSS is the largest process high-water mark among the three runs. Each `.resources.txt` independently records GNU time user/system CPU, elapsed time, maximum RSS, page faults, filesystem input/output and exit status. Each JSON retains Go allocation/GC metrics and `/proc/self/io` before/after values. `verification.txt` records the cross-candidate equality assertion; `summary.json` contains all raw run metrics and the comparison.

The launcher and helper are `run.sh` and `main.go.txt`. The helper is deliberately stored as text in the workspace; it is compiled only as `cmd/batchbench/main.go` within the isolated remote module. The source archive copied only go.mod/go.sum and cache/scan/model plus this benchmark helper. No additional performance experiments or default changes followed this run.
