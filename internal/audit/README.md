# Independent production audit

`audit.Run(ctx, Config, progress)` runs **after** a completed immutable production
run. It never launches or resumes production, and it does not read Go edge or
counter intermediates. DuckDB CLI is an independent SQL implementation of the
fixed corpus and weighted representative cells.

Supported scope is intentionally narrow: production-v1, complete full snapshot,
core (`is_xpac=false` **and** `quality_null_xpac=false`) or all (joint core+XPAC, retaining flagged unknown-XPAC rows), the agreed 15-type
blacklist, pre-2000 cited targets retained, citing years 2000–2026, same-work loops
excluded, and either historical fractional-Concept or current whole-Concept six-scenario inventories. Unsupported
choices fail explicitly. Author self-citations are retained; same-work loops are
a different condition.

## Work performed

1. Check production completion/configuration, full-cache source identity, and
   its published normalized-cache content key.
2. Recompute SHA256 for every normalized shard and every published result listed
   in completion.json. No incomplete projection directory glob is used.
3. Save `query.sql`, then execute DuckDB with explicit thread/memory limits.
   `q` is a materialized ID/year/representative-weight table, without copying the
   reference lists. DuckDB temporary spill files stay in the audit work directory.
4. Independently inventory all raw cache rows by type/core/XPAC/unknown flag and
   compute post-blacklist Q for the run's actual corpus. Unknown XPAC flags never become core; all retains them explicitly as the unknown inventory group. Audit JSON records selected counts by core/XPAC/unknown and by type, separately from raw inventory.
5. Perform **one** complete reference join: both endpoints in Q, per-source
   `list_distinct` reference deduplication, citing year, no same-work loop. That
   join produces annual edge counts **and** all nine representative cell sums.
   Future-dated targets are counted and diagnosed consistently with production.
6. Compare annual totals in all four layers of cube and all six statistics
   scenarios, including UNKNOWN. Q independently stores distinct country counts,
   and distinct qualifying Concept counts at both levels and all three thresholds.
   Whole Concepts use max(1,label count); country whole uses max(1,country count).
   Their product supplies whole-country/whole-Concept assignment totals. The
   independent edge join sums target assignment counts in each citing year.
   Country-whole Topics totals measure deduplicated country-Work/edge assignments,
   not unique global Works. Historical fractional Concepts retain conservation.
   Representative cells use the actual main Concept mode; Topics are unchanged.
7. Check every whole-country/whole-Concept count is a finite nonnegative exact
   integer, and every row has valid four-layer dimensions, fixed-dictionary labels
   and scenario metadata. Missing country/classification has one UNKNOWN label.
8. Recheck source size/mtime and result SHA256, save JSON evidence and audit.json.

The representative query trims/de-duplicates country and concept identifiers;
multiple copies of one qualifying concept do not gain extra weight. It attributes
citations to target works, including old targets. A retained whole Concept contributes 1; a retained historical fractional Concept contributes 1/k. Missing labels have a single UNKNOWN assignment, and duplicate IDs are counted only once per level after finite-score thresholding. Global annual integer checks
are exact. Fractional sums use absolute tolerance 1e-6 plus relative 1e-10, with
observed deltas recorded for every check.

Evidence: inventory.json, q.json, publication_year.json, citation_year.json,
output_year.json, output_cells.json, output_counts.json, output_integrity.json, native stdout/stderr,
query.sql, and audit.json with source/output/evidence hashes and pass/fail checks.
The audit directory is locked. An explicit rerun replaces only these known
audit-owned evidence names. Canceled/failed native work cannot be reported passed.

This checks retained citation totals, independent weighted assignment totals,
selected weighted cells, and
all eight annual complexity model series (216 models). It is not a complete
all-cell raw-graph recomputation or proof of historical OpenAlex reference
coverage. Inputs are trusted immutable artifacts; deliberate concurrent changes
preserving file size and mtime are outside scope.

## Independent model numerics

`AuditModels(ctx, resultDir, outputFile)` rebuilds X from the main cube and fixed
dictionary, parsing the actual main scenario ID from scenarios.json, without calling `eci.Compute`, `pipeline.ComputeModels`, or
`results.Dictionary.Matrix`. UNKNOWN is excluded. RCA and the 1e-12 boundary
rule, M, active label indices, components and status are independently verified.

The country operator is explicitly **non-symmetric**
`T = Dc^-1 M Dd^-1 M^T`; a general `mat.Eigen` decomposition independently
checks the spectrum, lambda2 and eigengap. No `EigenSym(T)` call is made. Valid
ECI/PCI must have zero means and unit population variances, satisfy
`center(T*ECI)=lambda2*ECI`, and match
`PCI=standardize(Dd^-1 M^T ECI)`. Joint axis sign flips preserve these equations,
so the audit additionally constrains the published time-series orientation.
The first valid year must have positive diversity covariance; its zero-covariance
case uses the first nonzero raw-coordinate label tie. The audit recovers the raw
eigenvector normalization independently with stationary country-degree weights.
Subsequent valid years must have nonnegative common-country covariance. Fewer
than two common countries or zero common-country score variance explicitly
reports a diversity/tie fallback rather than claiming a temporal comparison.
Undefined
models must have absent scores, never invented zeros.

`model-audit.json` records all 216 statuses, residuals, gaps and individual
pass/fail checks. Full `Run` invokes this after the SQL comparisons, links each
model pass into audit.json, and hashes the numerical evidence file. Tests use a
hand-derived 3x3 non-symmetric T with closed-form eigenpair/PCI, then corrupt ECI,
PCI, active indices, lambda2 and M to prove that failures are detected. A malicious
whole-year ECI+PCI flip passes the per-year equations but fails orientation.

Both score Parquet files are also streamed independently and matched to all 216
JSON models: every dictionary label, active/valid flag, status, instability flag,
scenario ID and score must agree. Missing, extra, duplicate or modified records
fail. Source hashes cover these exports as well as cube/JSON/dictionary. A
standalone models-only rerun does not rescan the raw reference graph; use a new
report path if retaining a prior audit.json whose evidence hash names the old
model-audit.json bytes.

## Tests

`go test ./internal/audit` tests controlled scope rejection, integer/fractional
check semantics, cancellation, and generated SQL structure. If DuckDB is on PATH
(or `OA_DUCKDB=/path/to/duckdb`), it also executes a 10-row hand-counted fixture:
pre-2000 targets, repeated references, excluded types, XPAC/null XPAC, same-work
loops, a future target, missing country/classification, and threshold/dedup rules.
It verifies seven surviving edges, then changes a cube cell and republishes its
valid hashes to ensure the independent semantic comparison actually fails.

Defaults: 46 DuckDB threads, memory limit 512GB. The caller must schedule this
native phase after Go production and adjust resources for other server workloads.
No full audit is run automatically by these tests.

### Concept-mode revision compatibility

The controlled full oracle independently pins two inventories rather than following mutable production defaults. Historical IDs are `fractional-concept-0.*` / `whole-concept-0.*`; new full-Concept IDs are `fractional-concept-whole-0.*` / `whole-concept-whole-0.*`. Both use thresholds0/.3/.5 and a fractional-country main. Unsupported mixed/custom full-oracle scope is rejected explicitly. The standalone model audit can resolve any valid unique fractional-country main scenario ID, and both score exports must carry that actual owner; a legacy missing scenarios/owner field is accepted only as the historical default. New-mode output cannot borrow historical model ownership.

Tests retain historical fractional fixtures, add a whole-Concept hand graph with duplicated countries/Concepts and different level0/1 threshold sets, and deliberately contaminate an integer cell by .25, change a nonrepresentative Level1 dictionary label, and use the wrong model/export owner. Both modes run against DuckDB with `OA_DUCKDB`; no full-data audit is started by tests.

### Core versus all corpus

The completed run's `Scope.Corpus` chooses the independent SQL membership predicate. Both modes retain the identical blacklist/paratext filters, pre2000-target strategy, year window, reference deduplication and no-loop rule. `all` builds one joint Q and joins both endpoints against it: core->XPAC, XPAC->core and XPAC->XPAC edges are retained along with core->core. Summing separately induced core/XPAC outputs cannot reproduce the joint graph. Hash, assignments, exact whole-count, taxonomy and model checks are unchanged.

The six-row cross-corpus fixture has two eligible core and two eligible XPAC Works, plus excluded core editorial and XPAC dataset. The old core graph has one edge; the separate XPAC graph has two; the joint all graph has eight (five additional cross-corpus edges), with manual yearly totals and future-target/loop counts. Both full oracles must pass these hand-written outputs. It is a tiny integration test, not a full-data audit.
