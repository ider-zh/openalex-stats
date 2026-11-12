# Local statistics explorer

`web.New(dataDir string) (http.Handler, error)` loads an immutable result directory.
The root command owns listener addresses, graceful shutdown, and optional SSH
forwarding. No registration, external hosting, CDN, remote fonts, or publication
is performed by this package. HTML, CSS, JavaScript, and Apache ECharts 5.6.0 are
embedded in the Go binary.

`web.NewCatalog(map[string]string{"core": coreDir, "all": allDir})` serves both
immutable versions on the same port. Core is required/default; all is optional.
Only these fixed keys are allowed; ready state and recorded scope must match.
`GET /api/catalog` lists available versions. `corpus=core|all` is routed before
normal parameter validation for metadata, series/CSV and downloads. Unknown,
empty or repeated selectors fail. `New` keeps its original single-directory API
and additionally lists/accepts its own corpus. Query responses identify corpus,
downloads carry `X-OpenAlex-Corpus`, and CSV appends corpus without moving the
previous columns. The UI reloads selected metadata and keeps valid filters on
switch; it never adds core and all results together.

## Input artifacts

Required for ready state: completed `run.json` and `statistics.parquet` (or the
older `cube.parquet`). Optional: `dictionary.json`, `scenarios.json`,
`coverage.json`, and `eci.json`. Results are read-only. Missing/not-completed
results show an explicit not-ready page; malformed completed artifacts fail
construction. Restart the service to select a newly completed run/directory.

Formal dictionary: `countries:[{id,display_name}]`,
`layers:[{scheme,level,labels:[{id,display_name}]}]`. Missing display names use IDs
and the metadata/UI explicitly report fallbacks. Formal statistics scenarios use
their recorded IDs. Existing `cube.parquet` is exposed as `current`.
Optional label `parent_ids` supplies real navigation relationships. Topics
subfields use the snapshot field parent. The 284 snapshot Concepts L1 ancestor
fields and matching official API entries were all null (probe evidence in
research/concepts-api-hierarchy-probe.json); the tree explicitly lists missing
parents instead of guessing. Tree choices are limited to the selected layer.

New defaults use `fractional-concept-whole-0.0/0.3/0.5` and corresponding
`whole-concept-whole-*` scenarios. The main country-fractional scenario owns
ECI/PCI. Explicit model `scenario_id` is respected. Missing model ownership uses
the recorded main scenario, preserving `fractional-concept-0.3` for old exports
and `current` for legacy cubes. Old data is never reinterpreted as concept-whole.
Other scenarios do not silently reuse
this model. Statuses such as disconnected/degenerate remain null/gaps; unstable
valid models retain scores with warnings.

## HTTP contract

- `GET /api/meta`: readiness, run scope/status, dictionaries, scenarios, year
  range, coverage, downloads, and metric definitions.
- `GET /api/series`: `scheme=topics|concepts`, `level=field|subfield|0|1`,
  `discipline=<ID>`, `countries=CN,US`,
  `metric=publications|citations|share|rca|eci|pci`,
  `basis=publications|citations`, `scenario=<ID>`, `start=2000`, `end=2025`.
- `GET /api/series.csv`: identical selection, actual numeric values and explicit
  status/partial columns; undefined values have empty numeric cells.
- `GET /download?file=<allowlisted artifact>`: original files only; traversal,
  arbitrary names, directory listing, and external symlink targets are rejected.

Series responses also expose `counting`: country/concept modes, threshold,
whether the Concepts threshold applies, and whether publication counts are
integer under the recorded weights. Series points contain `year`, numeric-or-null
`value`, `status`, and `partial`.
For ECI/PCI, points additionally contain `rank` (null when invalid) and
`rank_universe`. They rank all valid active model countries/disciplines by original
score, with competition ties (`1,1,3`), independent of selected countries. CSV
preserves original `value` and appends rank/universe after corpus.
Counts missing from sparse statistics are zero for valid labels. Shares require
a positive known-country discipline denominator. RCA additionally requires a
positive country portfolio. UNKNOWN cells do not enter these denominators.

ECI is country-wide over all disciplines at the selected layer, independent of
the selected discipline. PCI is a single global discipline curve. Querying only
certain countries never recomputes global margins or shrinks the citing corpus.

UI defaults to 2000–2025 and marks 2026 partial for this project's June snapshot.
It defaults to CN, US, GB, DE, FR, JP, IN, CA and KR when available, as common
comparison countries rather than an official ranking. Explicit country query
parameters take precedence. Country search accepts Chinese, English and ISO
codes; selected chips, bulk results, clear and reset avoid scrolling the whole
list. Small choice sets use one-click buttons. Quantitative series show annual
markers and tooltips sort current values descending with nulls last.
ECI/PCI default to a global Bump ranking view with a one-click original score
view. Country ECI always uses the layer's complete discipline structure and does
not change when a single subject is selected. Rank views preserve gaps and display
global rank/universe alongside the original score.
Whole-count shares use country assignment totals, not unique global works.
Concept-whole means each distinct qualifying same-level concept label receives
one full work. Country fractions still apply independently, so the default
country-fractional curves can contain fractional publications. Country-whole
plus concept-whole publication counts are integers. Topics use the primary topic
mapping and do not apply the Concepts score threshold; the UI says this explicitly
and suppresses redundant threshold-only Topics choices.
Pilot runs are prominently identified as real subset statistics, not world
totals or national rankings.

## Validation

`go test ./internal/web` exercises real Parquet fixtures, formal and legacy
formats, sparse zeros/undefined denominators, UNKNOWN boundaries, active-score
index mapping, ECI/PCI attribution, model gaps, scenario isolation, partial years,
actual CSV downloads, embedded assets, malformed parameters, and traversal safety.
Hand-counted multi-country/multi-concept fixtures check hit-whole versus country
fractions and whole integer counts, alongside legacy dataset interpretation.
Browser QA derives scenario IDs from metadata and can require the new mode with
`QA_EXPECT_CONCEPT_MODE=whole`; new-mode runs additionally verify real Biology
whole publication/citation integers and visible counting explanations.
Visual browser verification belongs to the service integration task.
`OA_WEB_BROWSER_QA=1 go test ./internal/web -run TestLocalCatalogBrowserControls -v`
additionally runs installed Chrome against two hand-sized localhost fixture
directories to verify catalog isolation/preserved filters, real-parent selection,
search/actions, one-click weighting choices, actual Bump/score views, tooltip
null ordering, CSV/download separation and mobile overflow. Fixture screenshots
are explicitly not formal results. Formal QA accepts `QA_CORPUS=core|all` and must
run after those completed production datasets have been independently validated.

Quantitative chart updates intentionally disable ECharts interpolation. The
canvas is flushed in the same update task as the comparison table, so a freshly
updated table cannot be paired with an intermediate curve from the prior query.
CSS page entrance effects remain independent of the statistical data. Browser
QA verifies actual rendered polyline coordinates against API values as well as
the table's full-precision values, including after a mobile viewport resize.
