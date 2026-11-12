# Bounded independent raw-to-cache audit

This Go utility invokes DuckDB SQL directly and never calls the Go Parquet scanner or projection helpers. It intentionally audits the two fixed real files in the 2026-06-26 snapshot: the first 2016 update shard and `updated_date=2026-06-26/part_0006.parquet`. It compares every row in those files, not just sampled IDs. This is evidence for source-boundary semantics, not a full-dataset second projection or a prevalence estimate.

Build and run on 229:

```sh
go build -o projection-audit ./scripts/projection-audit
./projection-audit \
  --cache-manifest /mnt/nvme0/public/openalex-stats/cache-20260626/manifest.json \
  --work /mnt/nvme0/public/openalex-stats/audits/projection-raw-cache-v1 \
  --duckdb /home/ider/.local/bin/duckdb --timeout 5m
```

DuckDB is limited to two threads and 2GB. Output contains query.sql, inventory.json, comparison.json, up to 20 mismatch examples, stdout/stderr and audit.json. Every mismatch count must be zero, every source/cache ID must be unique, and total compared rows must equal the bound source shard row count. A nonzero engine exit, cancellation, missing result or changed normalized hash fails the audit.

The actual run compared 402,439 rows and passed all 23 field/quality checks in about 13.96 seconds. Concept pair multisets are compared directly; a separate SQL GROUP BY compares per-ID max scores in each level. Repeated refs are compared in both sequence and set form. Source files are checked against size/mtime/fingerprint metadata before and after; the normalized shards are actually SHA256 hashed. This does not claim whole-raw-file SHA256 verification or protection against deliberate same-size/same-time mutation.

Only the utility's known audit evidence filenames are overwritten on a rerun in the dedicated audit directory. It does not alter raw files or normalized cache files. The root production run remains immutable and its completion has a separate identity.
