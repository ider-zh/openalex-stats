# Default versus NUMA interleave: real 50-million-ID index

Completed on server 229, 2026-11-12 04:00:31–04:00:59 +08:00. This comparison used the same prebuilt graph.test binary and 50 million real IDs sampled in 32 nonoverlapping windows across the complete all-ids vector. Index configuration was 32 workers, 256 shards and GOMAXPROCS=46 on the two-node, 48-logical-CPU host. Default placement and interleave across nodes 0 and 1 each ran twice. Production binaries, sources, default settings and system-wide policy were unchanged.

| Placement | Round | Build seconds | Serial lookup ns/op | Concurrent lookup ns/op | Build CPU | Lookup CPU | Peak RSS MiB (build / lookup) |
|---|---:|---:|---:|---:|---:|---:|---:|
| Default | 1 | 0.54470 | 361.3 | 10.040 | 965% | 1922% | 2685.21 / 2685.96 |
| Default | 2 | 0.54511 | 294.1 | 9.519 | 954% | 2257% | 2685.21 / 2686.33 |
| Interleave 0–1 | 1 | 0.56993 | 282.0 | 9.288 | 948% | 2215% | 2685.14 / 2685.96 |
| Interleave 0–1 | 2 | 0.75093 | 283.5 | 9.330 | 865% | 2223% | 2685.14 / 2685.92 |

Interleave improved the measured query times but slowed index construction in both rounds. The default serial lookup and interleave build showed substantial variation. Two rounds do not establish a robust whole-pipeline benefit, so the production default remains unchanged. This completes the requested NUMA candidate measurement without implying that one memory policy is universally faster.

The policy change used a local Go wrapper calling SYS_SET_MEMPOLICY with MPOL_INTERLEAVE and a node mask of 3. The wrapper locks its current OS thread, sets the policy and execs a shell; the shell records its policy and execs graph.test with the same PID. Linux exec inheritance was checked explicitly: default GET_MEMPOLICY returned mode 0 / mask 0 / errno 0; the interleave process tree returned mode 3 / mask 3 / errno 0. No package installation, global configuration or production process modification was needed.

The parent launcher also captured `/proc/PID/numa_maps` while the process comm was actually graph.test. Every default snapshot reported `default`; every interleave snapshot reported `interleave:0-1`. For example, the largest lookup snapshots recorded the following resident pages:

| Placement / round | Node 0 pages | Node 1 pages | Actual mapping policy |
|---|---:|---:|---|
| Default / 1 | 169,719 | 387,760 | default |
| Default / 2 | 193,570 | 349,866 | default |
| Interleave / 1 | 271,427 | 272,024 | interleave:0-1 |
| Interleave / 2 | 271,559 | 272,127 | interleave:0-1 |

These sampled page counts establish policy/application and distribution; they are not peak RSS or a measure of local versus remote accesses. Snapshot timing differs between processes. The raw actual-process snapshots and status files are retained in each `.maps` directory and in `numa-maps.tgz`; `numa-policy-summary.json` records the maximum-resident sampled snapshot per subprocess. Pre-exec shell snapshots and per-case GET_MEMPOLICY JSON are also retained.

Build benchmarks used one timed build; lookup benchmarks used Go's 1-second calibration for each serial/concurrent case. Lookup traffic contains 12.5% confirmed misses and includes query generation. The concurrent benchmark uses Go RunParallel under GOMAXPROCS=46. The order was default then interleave in round 1, and interleave then default in round 2. This is an eight-subprocess run: each placement/round has a build subprocess and a lookup subprocess containing both query modes. All eight subprocesses exited zero and reported zero swaps and zero filesystem input blocks, consistent with warm file pages. The existing v8 web process remained present; no additional compute/audit workload was started.

Build/lookup timings in the table are Go benchmark timings. CPU and RSS are GNU time measurements for their complete subprocesses, which also include loading the ID sample and benchmark setup/calibration. CPU 100% means one logical CPU; it is not normalized to total machine capacity. Thus those CPU figures should not be interpreted as phase-specific utilization or a claim that the entire stage used 90% of the host.

Input vector: `/mnt/nvme1/backup/openalex-stats/core-goal-v1/all-ids.bin`. Independent remote directory: recorded in `session.json`. The exact reused graph.test SHA256 is `aa4ad725f09dbd7d75fdacf7b4d74861fe3f5112f6db9770c70b5ea68cdea9ab`; both policies used that same binary. `source-files.sha256` also records the wrapper, wrapper source and launcher hashes. The wrapper source is saved as `numa_wrapper_linux.go.txt`; launcher scripts are `run.sh` and `exec-case.sh`. `summary.json` retains per-round timings, CPU and RSS. Raw benchmark stdout, GNU time reports, policy probes, host snapshots and dates are retained beside this report.

No production default was changed. No further optimization or benchmark work followed this comparison.
