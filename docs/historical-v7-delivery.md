> 2026-11-12计数更新：Concepts已改为同层标签去重、命中即计1篇；国家全计数应为整数，国家均分仍可小数。当前规范见 [counting-policy](docs/counting-policy.md)。下方v7/v8与core-goal-v2为已冻结的历史分摊版本；新正式结果重算与部署后将更新入口。

# OpenAlex statistics

Go pipeline for country × year × discipline publication counts, annual received citations, RCA, ECI and PCI. Full snapshot2026-06-26: **510,372,821 source Works →269,194,661 eligible core Works →2,363,318,327 citation edges**.

Accepted optimized data is stored at /mnt/nvme0/public/openalex-stats/results/core-goal-v2 on229. Final delivery/service acceptance is tracked in [progress](docs/goal-progress.md); a completion manifest certifies immutable calculation output, not every project acceptance gate.

- [Contract](docs/goal-prompt.txt)
- [Methodology, country attribution and quality](docs/methodology-and-quality.md)
- [Performance evidence and limits](docs/performance-report.md)
- [Independent requirement audit](docs/independent-completion-audit.md)
- [Server profile](configs/server-229.profile.json)

## Statistical scope

Four separate layers: Topics Field/Subfield and Concepts Level0/1. Output2000–2025, with2026 partial through June26. Main corpus requires is_xpac=false, excludes the15-type blacklist and is_paratext=true, and retains eligible pre-2000 targets. Country/subject missingness remains UNKNOWN in base statistics and is excluded from ECI/RCA matrices.

For edge A→B, both endpoints must belong to the eligible corpus. Citation year belongs to A; country and subject belong to B. References are deduplicated; same-Work loops are excluded. Author self-citations and flagged retracted originals remain. Countries are the distinct union of authorships.countries, preserved as original codes and weighted equally across those countries. Whole counts are also exported.

Six current default scenarios combine fractional/whole countries with Concepts score thresholds0/.3/.5. Each distinct retained same-level Concepts label receives one complete assignment; Topics use primary_topic. Eight annual ECI/PCI models use fractional countries, whole Concepts assignments and threshold.3: four layers × publications/citations. Country ECI describes the whole national portfolio at that layer; subject PCI is one global series. Invalid model values produce gaps. Historical v7/v8 results below used fractional Concepts; do not mix their scenario IDs or statistics with the revised results.

The new core/all calculation, UI and validation plan is recorded in [current release](docs/concept-hits-release.md). The local Java methodology comparison is in [Java method diff](docs/java-method-diff.md).

## Build and checks

Go>=1.26.0, fixed go.mod/go.sum. Application builds without CGO; race tests require a C toolchain.

```sh
go test ./...
go vet ./...
CGO_ENABLED=1 go test -race ./...
go build -trimpath -o oa-stats ./cmd/oa-stats
```

Statistics were produced by the preserved v7 executable; current v8 adds parallel input verification without changing statistical semantics. Exact binary/source identities are versioned under research. Do not overwrite an immutable run with a different build ID.

## Reproduce or resume the actual formal run

SSH to ider@192.168.1.229, then use the preserved **same v7 binary** and exact settings:

```sh
cd /home/ider/tmpdata/openalex-stats-dev-20261112
./oa-stats-v7 compute \
  --manifest /mnt/hg02/openalex-snapshot/data/parquet/manifest.json \
  --cache /mnt/nvme0/public/openalex-stats/cache-20260626 \
  --work /mnt/nvme1/backup/openalex-stats/core-goal-v1 \
  --edge-cache /mnt/nvme1/backup/openalex-stats/core-reference-v2 \
  --counters /mnt/nvme2/public/openalex-stats/core-counts-v2 \
  --output /mnt/nvme0/public/openalex-stats/results/core-goal-v2 \
  --workers 46 --readers 24 --edge-workers 46 --count-workers 32 \
  --index-workers 32 --index-shards 256 --partitions 64 \
  --memory-gib 640 --host-available-floor-gib 64 --pprof 127.0.0.1:8898
```

It verifies completed output and returns its original report; the actual same-config replay left completion SHA and completion time unchanged. For a new build or policy, select a new output directory. Changing Q also requires a separate work identity; changing only weight/threshold scenarios can reuse compatible Q/graph caches. Kernel locks, atomic writes, source/config/build fingerprints and shard hashes prevent concurrent writers and double accumulation.

Source snapshot is read-only. Actual writable directories are /mnt/nvme0/public/openalex-stats, /mnt/nvme1/backup/openalex-stats and /mnt/nvme2/public/openalex-stats. Before any new full job, check host load, disk space and the64GiB host available-memory floor. RAM cap defaults640GiB; useful arrays/cache, not dummy allocation, determine consumption.

To prepare a new normalized cache:

```sh
./oa-stats-v8 prepare \
  --manifest /mnt/hg02/openalex-snapshot/data/parquet/manifest.json \
  --cache /mnt/nvme0/public/openalex-stats/cache-20260626 \
  --workers 16 --memory-gib 640
```

The normalized cache retains all source rows. Production compute has no row limit; the development run command defaults to100000 rows and marks partial inputs pilot_complete. Only exact unrestricted manifest coverage can produce full_snapshot_complete.

Policies are configurable through --policy, --corpus, --corpus-year-start, --include-work-self-loops and --scenarios. [Default scenario JSON](configs/scenarios.default.json) reproduces the six current default scenarios. Scenario IDs must be unique, weights legal, thresholds finite in[0,1], and exactly one main scenario must use fractional countries; its Concepts weighting is explicitly recorded. Its actual ID is propagated into all model outputs. The controlled full oracle explicitly refuses unsupported custom policies rather than pretending it checked them.

## Independent validation

```sh
./oa-stats-v8 audit \
  --data /mnt/nvme0/public/openalex-stats/results/core-goal-v2 \
  --cache /mnt/nvme0/public/openalex-stats/cache-20260626 \
  --work /mnt/nvme1/backup/openalex-stats/audit-new-run \
  --duckdb /home/ider/.local/bin/duckdb --threads 46 --memory-limit 512GB

./oa-stats-v8 audit --models-only \
  --data /mnt/nvme0/public/openalex-stats/results/core-goal-v2 \
  --work /mnt/nvme1/backup/openalex-stats/audit-models-new
```

Full DuckDB audit independently rebuilds membership and citation joins from normalized source, checking all annual totals and representative groups. Separate matrix checks use a nonsymmetric solver, verify RCA/M/active labels, temporal sign and both score Parquets. A bounded direct-raw projection audit additionally checked402,439 real rows/23 fields. The optimized result was compared against the audited baseline for **every** exported cube/statistics/RCA cell, with declared floating tolerance; raw integer edge/counter equivalence is recorded separately.

Evidence is separate from immutable statistics. Full oracle: research/full-audit-v5; optimized all-cell comparison: research/equivalence-v7; optimized216-model audit: research/model-audit-v7-v2.json; exact replay: research/replay-v7-validation.json.

## Display and downloads

```sh
./oa-stats-v8 serve \
  --data /mnt/nvme0/public/openalex-stats/results/core-goal-v2 \
  --listen 0.0.0.0:8899
```

LAN URL: http://192.168.1.229:8899. For SSH-only access bind127.0.0.1:8899 and forward with ssh -L 8899:127.0.0.1:8899 ider@192.168.1.229. Only one service should own the port. The project-owned service PID/log are recorded in the delivery manifest. The service survives SSH logout; after a server reboot run the command again.

UI supports taxonomy/layer/subject, multiple countries, years, counts/citations/shares/RCA/ECI/PCI and publication/citation model input, with CSV and full-artifact downloads. ECharts is embedded locally. Browser QA uses the pinned optional Playwright1.56.1 and an isolated headless browser; see scripts/browser-qa.mjs.

Artifacts: statistics.parquet (six scenarios), cube.parquet (main scenario), rca.parquet, country_eci.parquet, discipline_pci.parquet, eci.json, model_diagnostics.json, coverage.json, dictionary.json, scenarios.json, methodology.json, run.json and completion.json.

## Interpretation limits

Country is missing on51.31% of output-year Works. These remain UNKNOWN; country comparisons do not represent a census of all national science.2026 is partial and hidden by default. ECI/PCI are relative yearly structural scores, not absolute accumulated knowledge.

V7 measured a60-second window averaging93.27% whole-machine CPU; conservative non-GC bound93.09%. Individual one-second readings can be lower. Whole-run mean29.47%, sampled peak RSS134.53GiB; other stages are not uniformly90%. No90%-RAM, cold-cache or universal-optimality claim is made. See the performance report for all stage timings and experiments.


