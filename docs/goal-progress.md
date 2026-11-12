# Goal progress

> This page closes the historical v7/v8 Goal under its original fractional Concepts policy. New user-authorized revisions (whole Concepts assignments, core/all datasets, interaction improvements and bump charts) are tracked separately in [concept-hits-release.md](concept-hits-release.md); the completion statement below does not certify those new revisions.

Updated2026-11-12. All required computation, independent validation, recovery, performance conclusions and live display acceptance are complete. Final independent review:25 checks passed with explicit limits and no blocking findings. Contract: [goal-prompt.txt](goal-prompt.txt).

## Formal runs and immutable evidence

Snapshot2026-06-26: **2,446 source files /510,372,821 rows**. Complete normalized cache31,900,483,518 bytes; actual interruption/resume reused570 parts (research/full-prepare-run.json).

Both full runs select **269,194,661 core Works**, with201,267,475 output-year publications and **2,363,318,327 retained edges**. They exclude953,374 same-Work loops and retain/flag13,678,393 future-target edges. Country/subject UNKNOWN remains in statistics and is excluded from ECI/RCA matrices.

- Baseline v4: /mnt/nvme0/public/openalex-stats/results/core-goal-v1; run65644017413394b988bb5f1e81e7e4a055e2a119f4463650b8ce1f22b557d1d7.
- Optimized v7: /mnt/nvme0/public/openalex-stats/results/core-goal-v2; rune1c0135626bc7dfe950894a9e666df5d181d8c0d673023655221f27921445844.
- v7 reused verified dimensions but rebuilt every source-edge and target-counter shard in independent directories. Dimensions reused=true; edges reused=0; count partitions reused=0.
- v7 elapsed inside report576.19 seconds (9m36s); external kernel-start to committed completion was approximately579.80s (9m40s). Exact execution start/source/binary hashes: research/execution-v7.json. It includes initial cache validation. Baseline external elapsed was about19m26s; the baseline built dimensions fresh, so this is not an isolated whole-run index speedup comparison.
- Immutable reports/checksums: research/formal-run-v4.json, formal-completion-v4.json, formal-run-v7.json, formal-completion-v7.json.
- Six count/threshold scenarios; four taxonomy layers;216 annual model entries (8×27 years).

## Independent validation and recovery

- Full independent DuckDB reference join:2141 count/grouping checks passed,0 failed (research/full-audit-v5/audit.json), tied to baseline immutable run.
- Upgraded independent model audit: all216 matrices, first-year/temporal signs and both complete score Parquets passed for v7 (research/model-audit-v7-v2.json).
- Raw→cache independent DuckDB comparison:402,439 real rows in early/latest partitions,23 fields/quality checks, all differences0 (research/projection-audit/). This is bounded projection coverage, not a second full raw-data rescan.
- Cross-version SQL FULL JOIN of all2,562,778 cube rows,14,966,202 statistics rows and29,932,404 RCA rows: no missing/duplicate rows, metadata mismatch or numerical mismatch beyond abs1e-7+rel1e-10. Small floating summation differences are preserved in evidence, not claimed byte-identical. Evidence: research/equivalence-v7/summary.json and compare-v4-v7.sql.
- Exact binary/config v7 replay returned the original run_id and completion time, and left completion.json SHA unchanged. No compute stages reran. Evidence: research/replay-v7-validation.json and replay-v7.resources.txt.
- Original100k regression: Go and SQL agree on48,319 eligible Works,193 retained edges and388 excluded loops (research/regression-100k-v5.json, pilot-oracle-no-loops.json).
- Current Go tests/vet passed; frozen v7 Linux race suite passed (research/race-v7.log). Cache kill/cancel, shard corruption, output repair and no-double-count fixtures passed.

## Resource and query evidence

- Full index stage fell406.17s→9.87s. Full projection changed111.66s→115.28s; combined saving392.68s.
- V7 projection mean whole-machine CPU92.01%; independently reproduced60-second window mean93.27%. Charging all5.1124 GC CPU-seconds from the115s stage to that window still gives93.09%. Some individual one-second readings are below90%; longest every-interval>=90 run46.02s. These are separate facts.
- Independent exact60-second integration and raw trace: research/performance-window-v7-independent.json, performance-window-v7.json and formal-run-v7.json.
- Whole-run mean CPU29.47%, sampled peak RSS144,451,166,208 bytes; process swap0, host swap-out0pages, host swap-in10pages. Host counters are not attributed to this process.
- Real-ID index benchmarks:5M×3 repetitions and50M scale, exact membership/ordinal tests. See research/index-benchmark-20261112/FOLLOWUP.md. After the initial missing-numactl observation, a pure-Go per-process wrapper enabled default/interleave0–1 comparison:50M IDs, two rounds each, actual benchmark numa_maps verified. Build/lookup tradeoffs did not justify changing production placement. No global configuration or tools changed.
- Input SHA validation is parallel with every checksum/source-stat check preserved; the full-input94.35s→5.08s benchmark passed (24workers, warm cache). No performance experiment remains running.
- Final web v8 serves verified optimized data at http://192.168.1.229:8899;24 browser checks passed, including all8 model inputs, actual drawn curves/table/API consistency, mobile, partial2026 and downloads (research/browser-formal-v8/).
- Service PID877825 /v8 is bound to core-goal-v2/run e1c013..., with final24 checks and actual binary/data identity verified.

## Final acceptance

- Full-input validator benchmark and final source/race checks passed (research/cache-validation-benchmark-20261112 and race-v8.log).
- Final service switched to optimized immutable data;24 browser checks passed (research/browser-formal-v8).
- Delivery/performance/methodology/reproduction documents and release manifest provide the artifact inventory.
- Independent requirement review and batch experiment signoff passed: research/independent-final-acceptance.json (25 checks, no blockers). All evidence and runtime/source versions are indexed in research/release-v8.json; the final archive is regenerated to include this review.

No claim that all stages or every one-second CPU sample reaches90%. No artificial RAM target. See [methodology and quality](methodology-and-quality.md), [performance report](performance-report.md) and [independent audit](independent-completion-audit.md).




- Read-batch32/128/512/2048 experiment: four real files, each three complete scans; all12 content/quality hashes identical. Scope-specific throughput/memory tradeoffs and unchanged production default are documented; no proxy speedup is claimed for the full graph.


