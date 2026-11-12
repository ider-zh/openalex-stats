# Query-ready results

`DefaultScenarios()` returns six independently labelled combinations: country `fractional`/`whole`, Concepts threshold `0`/`0.3`/`0.5`, always fractional within a concept level. In the defaults `Scenario.Main` marks threshold 0.3. `ResolveScenarios` also accepts external scenario arrays, validates unique unpadded IDs, explicit legal modes, finite thresholds in [0,1], and exactly one `Scenario.ForECI()` (Main plus fractional country and concept weights). Only that owner supplies the eight annual main ECI/PCI models. Changing thresholds never changes corpus Q or its retained citation edges. Arbitrary thresholds such as .2/.4 are supported and tested against the map aggregator.

`aggregate.AddSeries(work, firstCitationYear, annualCounts, includePublication)` prepares the country's and four taxonomy layers' assignments once per work. The caller sets `includePublication` only if the work's publication year lies in the output window. Older targets can still receive citations in the window. Worker aggregators remain independently owned and merged in a stable order.

For production, use `NewDense(dictionary, scenarios, start, end)` and one worker-owned `Dense` per reducer. Its `AddSeries(work, counts)` automatically includes publication only inside the output window, retains citations to older targets, and reuses preallocated bitsets/lists. Duplicate concepts are deduplicated separately in each level by maximum finite score, which is equivalent to independently applying each threshold and then deduplicating. The three thresholds are derived in one scan. Both country weighting modes reuse those assignments. Unknown real dictionary IDs produce explicit errors; reconcile the complete universe before creating worker cubes.

`EstimateDenseBytes` budgets the two continuous float64 cubes and scratch. The current universe (247 countries plus UNKNOWN, 581 discipline labels plus four UNKNOWN slots, 27 years, six scenarios) needs 376,047,360 bytes for the two cube arrays, about 358.6 MiB per worker. Arrays are not replicated per paper. `Merge` checks layout/scenario fingerprints and requires disjoint input shards and fixed worker order. One merge internally parallelizes large array ranges, but concurrent merges into the same destination are not supported. `Inputs()` materializes sparse sorted rows only at export time; no row objects are created on the successful `AddSeries` hot path.

Tests compare six scenarios against the map-based aggregator on hand examples and 800 randomized works, including duplicate/trimmed countries and labels, cross-level identical IDs, duplicate scores, nonfinite scores, missing labels, out-of-window publication years and older citation targets. A separate allocation test requires zero successful hot-path allocations. The local small-dictionary benchmark on an i7-5820K measured about 602–618 microseconds per work for six map-based AddSeries calls versus 10.5–11.3 microseconds for Dense, with 82 versus zero allocations. This is an algorithm microbenchmark with a small hot cube, not a 229 or full-universe throughput claim; full-scale memory-bandwidth profiling remains necessary.

`Derive([]Input)` returns statistics and classification coverage. Each input must have one unique scenario ID, explicit weight labels, its aggregator rows and its raw `Quality`. Duplicate cells inside an input are summed; duplicate scenario IDs are rejected.

For one scenario/year/scheme/layer, let `Xcd` denote a known-country, known-discipline cell. The methods are:

- Publication/citation share: `Xcd / sum_c Xcd`, the country's share within the selected discipline.
- RCA: `(Xcd / sum_d Xcd) / (sum_c Xcd / sum_cd Xcd)`.
- `UNKNOWN` rows keep raw weighted counts but are excluded from all these margins and have false derived-validity flags.
- Whole-country margins count country assignments, not unique world documents. Their shares are normalized assignment shares. Fractional and whole values are never mixed in one denominator.
- Missing cells are sparse zeros for counts. A missing RCA is defined as zero only when the country and discipline margins are both positive; a zero margin means undefined, never a fabricated zero RCA.

`LoadDictionary(ctx, snapshotManifest)` reads only fields/subfields/concepts/countries. Countries are normalized from their dictionary IDs to original two-letter codes; no geographic merging occurs. `Dictionary.Matrix` retains every dictionary label for every year and zero-fills unobserved labels; unknown quality rows are omitted and other unknown real labels cause an error.

If the actual data contain real IDs absent from the entity dictionaries, explicitly call `Dictionary.Reconcile` on the union of all years/scenarios before annual modelling. It adds ID-based fallback labels, records `unlisted_labels`, and keeps the expanded dictionary fixed across years. It does not silently drop these records or mutate the original dictionary.

`Export(dir, rows, coverage, scenarios, models, dictionary)` writes:

- `statistics.parquet`: sparse full-count/fractional statistics with threshold labels, shares, RCA, denominators and validity flags.
- `coverage.json`: raw-work/raw-citation classification coverage for each scenario and layer. Missing-country quality is also preserved. It is not inflated by whole-country assignments.
- `dictionary.json`: `{countries:[{id,display_name}], layers:[{scheme,level,labels:[{id,display_name}]}]}`.
- `scenarios.json`: an array of `Scenario`.
- `eci.json`: the root model-result array, with an explicit scenario_id set to the configured unique ECI owner. An explicitly mismatched model owner is rejected. Old files without an ID remain a legacy read convention for fractional-concept-0.3; new exports do not rely on that fallback.
- `country_eci.parquet` / `discipline_pci.parquet`: long model-score tables, including inactive labels with `valid=false`.
- `rca.parquet`: long RCA table with scenario and metric labels.
- `model_diagnostics.json`: network components, eigengaps, residuals and validity statuses.

`Export` accepts existing root `[]pipeline.ModelResult` through a JSON adapter and therefore does not depend on the pipeline package. Models with disconnected or degenerate networks retain diagnostics and have no valid global scores. A largest-component-only analysis, if ever requested, must be named as a different reference population rather than silently replacing the global model.

Adding explicit scenario_id changes eci.json's encoding/hash compared with the original v4 files; the default model numerical payload and statistical Parquet values are unchanged. Export preserves unrelated root model JSON fields and validates that statistics/coverage scenario IDs belong to the supplied configuration.

Parquet output uses pure-Go Zstandard. Files are staged as `.tmp`, synced and renamed. The root run manifest remains responsible for declaring the whole output set complete only after all files and checksums succeed.
