# Parallel exact membership index

The serial constructor remains unchanged and is still the default. The optional
constructor preserves global dense ordinals and changes only map ownership:

```go
memory, err := graph.EstimateParallelIndexBytes(len(ids), 48, 512)
// Budget memory.PeakBytes plus the caller's source IDs and all other live data.
index, err := graph.NewParallelIndexContext(ctx, ids, 48, 512)
```

Construction uses worker-local histograms over disjoint input ranges, an
exclusive prefix, a single linear scatter of `{ID, Ordinal}` entries, and one
owner for each exact shard map. It is O(N + workers*shards), rather than scanning
the entire input for each shard. Histograms are reused as scatter cursors.

`ParallelIndexMemory` reports the owned ID copy, estimated shard maps, temporary
16-byte entry array, histogram and prefix arrays separately. Temporary arrays
become garbage on return; their RSS need not disappear immediately. The original
caller vector is not included. A zero or duplicate ID and cancellation never
return a partially constructed index.

`Lookup` routes a mixed numeric ID to its shard, then performs the same exact Go
map membership check. There is an extra mixer/routing cost; a faster build does
not imply a faster complete citation workload. Caller-owned IDs are copied and
can be released or reused after successful construction.

## Bounded, real-ID benchmarks

No benchmark is automatically started. By default the benchmark generates only
100,000 synthetic unique IDs. Set the environment variables below to read a
bounded real sample from the existing `all-ids.bin` format (uint64 row count,
then little-endian IDs). It samples 32 nonoverlapping windows across the vector,
without loading the entire input.

```sh
export OPENALEX_INDEX_IDS_FILE=/path/to/all-ids.bin
export OPENALEX_INDEX_SAMPLE_SIZE=5000000
go test ./internal/graph -run '^$' -bench '^BenchmarkIndexBuild$' -benchtime=1x -count=3 -benchmem
go test ./internal/graph -run '^$' -bench '^BenchmarkIndexLookup$' -benchtime=1s -count=3 -benchmem
```

Use an explicit sample size and an available RAM budget. Build results include
`ids/op`; lookup results include both one-goroutine and concurrent random access
with 12.5% confirmed misses. Do not choose a default solely from synthetic IDs or
build time: compare lookup throughput and estimated whole-run time too.

Variants include 64 shards and larger 256/512-shard cases. With 48 workers,
64 equally sized map jobs finish in two rounds with only 16 jobs in the second;
512 smaller jobs reduce this tail. More shards also change map locality and
routing overhead, so the optimum still requires measurement. Powers of two use
mask routing; arbitrary shard counts are supported with modulo routing.

Limits are 1..1024 workers and 1..65536 shards. Worker count is capped at the
number of input IDs. Go map allocation itself cannot be interrupted; loops and
phase boundaries check cancellation.
