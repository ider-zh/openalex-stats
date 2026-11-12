# Normalized snapshot cache

`Prepare` streams one complete raw Parquet file per worker into one normalized
Parquet shard. Files are independent, so a slow earlier file does not block other
builders. All rows are retained; corpus policy is applied later.

```go
m, err := cache.Prepare(ctx, input.Paths, cache.Options{
    Directory: "/mnt/nvme0/openalex-stats/cache-20260626",
    Workers: 16,
    BuildID: buildFingerprint,
    ManifestSHA256: input.ManifestSHA256,
    ExpectedRows: input.ManifestWorks,
}, reportProgress)
if err != nil { return err }
_, err = cache.Scan(ctx, m.Paths(), scan.Options{
    ReadDimensions: true,
    ReaderWorkers: 8,
}, consume)
```

`BuildID` must change when normalization semantics change. `Workers` is a tuning
input, not a claim that the mechanical source disk scales with that concurrency.
The default row group is 100,000 rows; writer batches contain at most 128 rows.
Zstandard uses fastest compression and one codec worker per builder.

The schema stores numeric work/reference IDs, deduplicated country sets, primary
field/subfield IDs, and parallel concept-ID/score lists for levels 0 and 1. It
also stores per-row quality counters, so null flags and malformed references do
not disappear during normalization. Concept order is canonicalized by level;
within-level values and scores are preserved. This format supports country-set
counting, not reconstruction of individual-author contributions.

## Completion and recovery

- Source fingerprint: format, build, source-manifest hash, absolute source path,
  input position, size, mtime, and row-group configuration.
- Each shard is written to a temporary file, finalized, fsynced and renamed.
  Its sidecar carries row counts, byte size, content SHA256 and source identity.
- A reused shard must match all identity fields, size, content SHA256 and its
  Parquet footer row count. Corrupt/truncated artifacts are rebuilt.
- Source identity is checked again after writing. Raw files are treated as an
  immutable snapshot; metadata fingerprints cannot detect a deliberate source
  content change that preserves both size and mtime.
- A kernel file lock prevents competing builders. It releases automatically if
  the process dies. After locking, abandoned cache-owned temporary files are
  cleaned; explicitly supplied source files are never removed.
- `manifest.json` is committed only after every source footer row is accounted
  for and any `ExpectedRows` assertion passes. Part sidecars provide recovery
  even if the process dies before the global manifest is committed.
- Old versioned artifacts are not automatically deleted. Storage accounting and
  retirement of unused versions belong to the run coordinator.

`Scan` uses physical column projection and deterministic input-file order. It
validates format/schema, while `Prepare` verifies content checksums. Callers must
not use normalized paths found by an unchecked broad directory glob, or mutate
artifacts between verification and reading.

Tests cover source/cache parity for all projections and row limits, nullable
quality flags, repeated verified reuse, content corruption, truncated sidecars,
source/build changes, row-count failures, concurrent locking, and a subprocess
hard-kill followed by recovery.
