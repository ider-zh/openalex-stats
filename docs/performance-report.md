# Full-run performance evidence

Snapshot: 2026-06-26. Executed binary/source identity: research/execution-v4.json. Formal baseline: research/formal-run-v4.json. These measurements are one actual run, not repeated controlled benchmarks.

| Stage | Seconds | Mean whole-machine CPU | Peak RSS GiB |
|---|---:|---:|---:|
| dimensions | 322.6 | 25.21% | 48.66 |
| indices | 406.17 | 2.11% | 59.61 |
| reference_projection | 111.66 | 90.91% | 114.28 |
| citation_counts | 76.99 | 17.64% | 116.77 |
| dictionary | 2.71 | 1.99% | 116.77 |
| aggregate | 65.59 | 55.61% | 116.77 |
| derive | 38.39 | 4.36% | 111.77 |
| eci | 1.27 | 49.24% | 17.04 |
| export | 40.15 | 2.06% | 17.04 |

Process CPU uses the aggregate /proc/stat denominator for all 48 logical CPUs. The reference projection stage exceeds 90% for its 111.66-second measured interval, satisfying the representative compute-stage target. Other stages and the whole run do not average 90%. The inner interval mean is 22.92%; its initial cache SHA validation was not sampled in executed v4.

The kernel process start was 1794420929.09 Unix seconds; completion.json was committed around 1794422095. External elapsed is approximately 1165.91 seconds (19m26s; filesystem timestamp rounded to a second), including initial validation and output checksum commitment. The inner report covers 18:17:03.184–18:34:51.449 UTC. Do not present the shorter inner interval as total execution latency. This excludes the earlier normalized-cache build.

Peak process RSS: 125,376,851,968 bytes (116.77 GiB). Minimum reported host available memory: 657,868,087,296 bytes (612.69 GiB), above the64GiB floor. Application cap was640GiB. RAM was used for compact dimensions, exact indices and a58,146,046,776-byte target×year counter grid. No dummy allocation was used to seek90% RAM. Existing host swap usage is not evidence of new project swap; v4 did not persist a swap-I/O time series, so a zero-swap-I/O claim is not proven.

## Bottlenecks and changes

The executed baseline builds two single-owner Go maps serially (406.17s), dominating the run. A new O(N) histogram/prefix/scatter index partitions independent maps while preserving exact global ordinals. A5M-real-ID preliminary benchmark shows approximately5.2x faster construction and no lookup regression, but it overlapped the independent DuckDB audit and has only one repetition. It is explicitly not the final default-selection evidence; see research/index-benchmark-20261112. Uncontended repetitions are pending.

A20-second counts-stage CPU profile (research/cpu-counts-v4.pprof) contains266.22 CPU-seconds: reduceOne48.88% flat, SHA25634.32%, counter encoding11.23%, system calls4.09%. This stage is a mix of target-owned numeric reduction, memory access, durable writes and checksums. The profile does not establish memory bandwidth as the sole bottleneck. Read/write pidstat samples begin during counts and continue to completion; they do not cover all earlier stages.

The dense aggregation avoids maps/allocations per work and shares label preparation across six scenarios. Its correctness was checked against the map implementation on randomized fixtures; zero-allocation microbenchmarks are not full-data throughput claims. A hot local benchmark gave roughly55x speedup for this kernel. Full aggregation still includes dispatch, scanning269M compact rows, memory-bandwidth costs and deterministic merges.

Source preparation used46 then16 file readers:46 caused much higher observed HDD I/O pressure, so16 was used for the resumed run. File mix and cache state differ; this is not a controlled46-vs16 speedup comparison. The full normalized cache is31.9GB and removes repeated raw nested-Parquet decoding. The resumed preparation took1411.87s and reused570 committed parts. Evidence: research/full-prepare-run.json.

## Instrumentation improvements after baseline

Current source additionally records stage GC collections/pause/CPU, allocated bytes, process physical/logical I/O and peak process swap. Mean CPU is now weighted by elapsed machine ticks. Cache validation is included explicitly in stage and overall timing. These fields do not retroactively fill missing v4 measurements. Current-source tests pass; measurements from a subsequent run must be identified separately.

Independent DuckDB is a correctness oracle, not a replacement production engine. No unsupported alternative-engine speed claim or cold-cache claim is made. CPU and memory budgets apply to the project process; unrelated machine load is excluded.

## Optimized full run v7

Frozen source/binary: research/execution-v7.json. Immutable result core-goal-v2 /run e1c0135626bc7dfe950894a9e666df5d181d8c0d673023655221f27921445844. It reused verified compact dimensions and fully rebuilt all2446 reference shards and64 counter partitions; graph/source counts match baseline exactly. The inner run report took576.19s (9m36s, including input verification); kernel start to completion-manifest commit was approximately579.80s (9m40s, timestamp rounded to one second). Its lower elapsed than baseline is not a controlled whole-run speedup: dimension reuse differs.

| Stage | Seconds | Mean machine CPU | GC CPU seconds | Physical read/write GiB |
|---|---:|---:|---:|---:|
| cache_validation | 103.14 | 2.04% | 0.627 | 2.05 / 0 |
| dictionary_validation | 0.42 | 1.24% | 0.124 | 0 / 0 |
| dimensions | 133.07 | 2.1% | 0.524 | 0.18 / 0 |
| indices | 9.87 | 60.36% | 0.179 | 0 / 0 |
| reference_projection | 115.28 | 92.01% | 5.112 | 0 / 17.63 |
| citation_counts | 68.51 | 22.83% | 0.287 | 0 / 54.15 |
| dictionary | 2.75 | 2.11% | 0 | 0 / 0 |
| aggregate | 60.31 | 56.59% | 0.124 | 0 / 0 |
| derive | 37.33 | 4.45% | 22.699 | 0 / 0 |
| eci | 1.27 | 38.28% | 0 | 0 / 0 |
| export | 41.56 | 2.08% | 0 | 0 / 1.17 |

The index-plus-projection total fell517.82→125.15 seconds, saving392.68s. Parallel index32workers/256shards took9.87s vs406.17s serial. Projection increased111.66→115.28s; this real lookup tradeoff is included. Same corpus, complete source coverage and an independent all-cell result comparison support correctness; data/cache conditions still differ between runs.

Projection mean machine CPU92.0124%. A contiguous60.0003-second sample-aligned window averaged93.2654%. Conservatively charging all5.1124 GC CPU-seconds from the115s stage to that window leaves93.0879%. Independent integration of exactly60seconds gave93.2771% (93.0995% after the same GC bound). This is a window-average claim: six sample intervals in the selected window were below90%, minimum78.20%; the longest sequence with every interval>=90% was46.02s. Do not claim every second stayed above90%. Source and independent recomputation: research/performance-window-v7.json and performance-window-v7-independent.json.

V7 whole-run CPU mean29.47%; sampled peak RSS134.53GiB. Minimum host available memory592.14GiB, far above64GiB floor. Process peak swap0; host swap-in10pages (40KiB), swap-out0. Host counters are system-wide and not attributed to the project. Reference projection read19.26GiB logically but0bytes physically, demonstrating a hot filesystem cache; no cold-cache claim is made.

Process totals:361.00GB allocated across the run (not simultaneous RSS),117GC collections,32.23GC CPU-seconds and0.0821s stop-the-world pause. Physical read2.23GiB/write72.96GiB; logical read119.68GiB/write72.95GiB. Detailed sampled traces and versioned stage resources are in research/formal-run-v7.json and performance-summary-v7.json. Source dispatch has an unbuffered queue (capacity0),46worker limit and logged unfinished-part counts; it does not build an unbounded edge queue.

Counts32workers took68.51s versus baseline24workers76.99s. This is a real same-sized numeric workload comparison with warm-reference differences, not a repeated isolated disk benchmark. Derive/export remain largely serial (37.33s/41.56s) and do not meet90%CPU; no universal utilization or optimality claim is made. The representative full reference stage meets the documented window-average CPU target. Sequential standard SHA integrity verification was the remaining significant startup bottleneck; current source now uses bounded per-file parallel verification, with full-input serial/parallel measurement now complete (see below).

The initial index experiment could not use numactl (absent, exit127); final per-process Go interleave tests below closed that measurement gap. No global package installation, swap changes or host cache dropping was performed. The optimized data passed full SQL cell equivalence within abs1e-7+rel1e-10, all216 upgraded independent model checks plus both score exports, and exact completed-run replay without recomputation.



## Final v8 input verification

Only pre-computation validation changed after the v7 statistics executable was frozen: the same cache bytes/source stats are checked by a bounded per-file Go worker pool. Statistical kernels and published v7 results were preserved. The CLI reader budget defaults to min(workers,24); explicit --readers remains configurable.

| Full verification | Seconds | Speedup vs serial |
|---|---:|---:|
| Serial |94.348|1x|
|8 workers |13.362|7.06x|
|16 workers |6.833|13.81x|
|24 workers |5.079|18.58x|

Every mode verified2,446files,510,372,821rows and31,900,483,518bytes with identical ordered manifest SHA0494c06b03ac97eea4deae6c9c22dce37ef69992e1cf3f64ea6c5b67c88057d2.24workers was fastest measured, sampled peak RSS45.16MiB and mean whole-machine CPU49.42% (GNU time2324% means about23.24cores, not2324% of the whole machine). All physical read counters were zero: this was a one-pass-per-mode warm-cache benchmark, not cold storage throughput. No hash or source-stat check was omitted. Evidence: research/cache-validation-benchmark-20261112.

This is a measured input-verification improvement, not a claim that the full v7 calculation was rerun by v8 in5seconds. The official statistics identity stays v7; the delivered current validator, source, race checks and service identity are recorded separately. No further performance experiment remains running.

## Read batch-size experiment

The explicitly required batch candidate experiment used four real normalized files (1,434,610rows), four readers and the same all-fields/reference/quality canonical checksum callback. Each candidate ran three times in rotated order. All12 scans had identical content SHA and work/reference/country/concept/quality totals; all were warm-cache with0physical reads and zero swap. Production source/default was unchanged.

| Reader batch | Median seconds | Rows/second | Peak RSS MiB |
|---|---:|---:|---:|
|32|14.044|102151|48.97|
|128 (current)|18.930|75784|51.59|
|512|15.014|95552|62.84|
|2048|13.381|107213|96.97|

2048 reduced this scanner-plus-canonical-hash workload latency29.32% versus128;32 used about half its peak RSS and was only4.72% slower than2048. These are measured batch tradeoffs, not a41.47% full-production speedup. Production reference projection uses independent per-file refs-only scans; this experiment uses ordered full-dimension+reference callbacks and its checksum overhead. We preserve the validated128 production default rather than declare a global optimum or transfer this proxy effect to the completed full run. The representative full-run CPU target is already met and the batch candidate requirement is evidenced. Experiment scope and all raw results: research/batch-benchmark-20261112. Final independent acceptance explicitly checked the12 scans and their source/patch/input/content hashes.


## Final NUMA comparison

A small pure-Go wrapper successfully applied per-process MPOL_INTERLEAVE to nodes0/1 without installing tools or changing global policy. GET_MEMPOLICY after exec returned mode3/mask3, versus defaultmode0/mask0. Actual benchmark-process numa_maps snapshots confirmed the two allocation policies; this is more than a successful wrapper probe.

Same50M real IDs, exact same graph.test,32workers/256shards/GOMAXPROCS46; two repetitions each:

| Placement | Build seconds | Concurrent lookup ns/op |
|---|---|---|
|Default|0.54470 /0.54511|10.040 /9.519|
|Interleave0–1|0.56993 /0.75093|9.288 /9.330|

Interleave slightly improved lookup while slowing construction; the default serial-lookup values also varied substantially. These two rounds do not prove whole-pipeline benefit or global optimality. Production placement remains default. Both policies had approximately2.62GiB peak RSS, zero swap and warm-cache reads. No statistical source, binary or result changed. Full8-process timing/resource/actual-policy/source evidence: research/numa-benchmark-20261112; independently verified in the25-check final acceptance.
