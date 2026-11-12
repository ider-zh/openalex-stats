# Statistical methodology and quality interpretation

> Historical v7/v8 evidence. The 2026-11-12 user revision replaces same-level Concepts fractional weighting with one full assignment per distinct matching label; see [current counting policy](counting-policy.md) and [current release](concept-hits-release.md). Numerical acceptance statements below certify the preserved historical core run, not the new core/all results.

Authority: docs/goal-prompt.txt. Snapshot date2026-06-26; source manifest SHA10c1785b4a6f9757823fc6dd005a052716e2c510ea751659cab4bcfba4c35e2a. Run65644017413394b988bb5f1e81e7e4a055e2a119f4463650b8ce1f22b557d1d7. Exact settings are in the immutable remote methodology.json/run.json and research/formal-run-v4.json.

The optimized final served run is e1c0135626bc7dfe950894a9e666df5d181d8c0d673023655221f27921445844 (core-goal-v2). It uses the same contract and exact raw corpus/edge counts, established by full exported-cell equivalence and committed integer-shard equivalence; its own216-model/export/sign audit also passed. Final actual settings are in research/formal-run-v7.json and the immutable v2 methodology/run files. Baseline identifiers below remain versioned evidence, not the current service identity.

## Country, subjects and time

A Work's country set is the distinct union of nonempty authorships.countries values in the snapshot, with original country/territory codes preserved. It is not first-author nationality or corresponding-author attribution. A CN+US Work contributes1 to each country under whole counting, or0.5 each under fractional counting. Countries are deduplicated across authors; author and institution counts do not change these equal country weights. Missing country goes to UNKNOWN; no country is inferred from names or missing affiliations.

Topics use the primary_topic's Field or Subfield, one label at each level. Concepts use the Work's tags at Level0 or Level1, deduplicated by ID, taking the maximum valid repeated score. Retained same-level labels receive equal fractions; parents/children are not mixed. The main threshold is score>=.3. Six output scenarios combine country whole/fractional with threshold0/.3/.5; whole country counts still use fractional Concepts tags.

Publication year is the Work's year. Citation year is the source Work's year, while country/subject belongs to the target. The same induced corpus Q is used at both endpoints; selection in the chart does not restrict citing countries/subjects. Eligible old targets remain in Q. 2000–2025 are complete calendar-year windows, not a guarantee of complete historical source coverage;2026 ends at snapshot date and is partial.

## Raw corpus and exact reference accounting

| Quantity | Count |
|---|---:|
| Raw Works |510,372,821|
| Core before type filters |317,820,190|
| XPAC before type filters |192,552,631|
| Unknown XPAC flag |0|
| Eligible core Q, all years |269,194,661|
| Q with valid output year2000–2026 |201,267,475|
| Q with invalid/missing year |2,284,669|
| Q outside output window |65,642,517|
| Retained flagged retracted originals |96,020|
| Reference slots from eligible output-year sources |2,599,185,946|
| Same-Work loops excluded |953,374|
| Targets absent from snapshot |117,697,642|
| Targets in snapshot but outside Q |117,216,603|
| Retained citation edges |2,363,318,327|
| Retained edges targeting a later publication year |13,678,393|

Reference-slot conservation is exact:2,599,185,946=953,374+117,697,642+117,216,603+2,363,318,327. The full run observed zero invalid reference IDs or duplicate references after normalization; this does not mean all raw metadata is flawless. Missing targets represent unresolved reference coverage, not zero-citation eligible papers. Future-target edges are reported rather than silently discarded; they can reflect metadata/version/year inconsistencies.

The15-type blacklist and is_paratext flag are the only document-type exclusions. Other, preprint, letter, report and book-chapter remain unless blacklisted. This is a reproducible research-document count, not a manual certification of every retained article. No title/DOI/language/open-access criterion is silently added. Detailed type×core/XPAC inventory and mutually exclusive filtering reasons are in run.json. Filtering reasons use rule precedence and must not be naively interpreted as independent overlapping flags.

## Coverage and threshold sensitivity

Among201,267,475 output-year publications,103,275,459 (51.31%) have no country; known-country publication coverage is48.69%. Of2,363,318,327 citation edges,276,033,100 (11.68%) have unknown target country. UNKNOWN stays in base counts and fractional conservation but is excluded from country-complexity/RCA matrices. Country comparisons therefore reflect observed affiliation metadata; absence must not be read as national zero output.

Topics Field/Subfield classification covers93.96% of output publications and99.93% of citation events.

| Concepts threshold | Level | Publication classification coverage | Citation classification coverage |
|---|---|---:|---:|
|0|0|97.94%|99.998%|
|.3 (main)|0|87.81%|99.40%|
|.5|0|43.89%|69.51%|
|0|1|78.94%|98.91%|
|.3 (main)|1|71.59%|95.83%|
|.5|1|30.87%|50.19%|

These denominators count raw Works/edges, not the sum of duplicated whole-country assignments. All scenarios use the same Q and graph; thresholds redistribute labels or move unclassified rows to UNKNOWN. Raising the threshold is a major coverage choice, especially for Level1. The six-scenario statistics allow actual curves, shares and RCA to be compared; ECI is intentionally fixed to the main .3 fractional scenario.

## ECI, PCI and RCA

For each layer, year and input metric X: RCA(c,d)=(Xcd/Xc)/(Xd/X), where totals use known-country×known-discipline cells of that model. Whole-count assignment totals are not unique global Work counts. A zero denominator has undefined RCA/share rather than an invented numerical zero.

Mcd=1 when RCA>=1, with values within1e-12 of the threshold normalized to1 for floating-point roundoff. Country diversity and subject ubiquity normalize the operator T=Dc^-1 M Dd^-1 M^T. ECI is its second-largest-eigenvalue right eigenvector (the leading nonconstant mode), centered and scaled to population variance1; PCI is the standardized dual projection Dd^-1 M^T ECI. Production diagonalizes the symmetric similar operator, rather than wrongly treating T as symmetric. Active indices map back to fixed full dictionaries.

Initial ECI sign favors positive diversity correlation, with a deterministic label-order tie break. Subsequent valid years align on common countries; ECI and PCI signs change together. Annual z-scores are relative structure, not an absolute accumulated-knowledge scale. UNKNOWN is not a country or subject in these matrices. Empty, disconnected, repeated-eigenvalue or unstable cases have explicit diagnostics; no fabricated rankings are inserted.

Four layers × two inputs ×27 years=216 models. Independent verification rebuilt X and RCA/M and used a general nonsymmetric eigensolver, checked centered(T*ECI)=lambda2*ECI and the PCI projection. All216 passed. Independent full DuckDB count/group checks:2141 passed,0 failed. Evidence: research/full-audit-v5/audit.json and model-audit.json.

## Evidence boundaries

The full oracle recomputes membership and citation joins from normalized Parquet, so it independently checks production graph/aggregation, but shares the projection boundary. A separate bounded direct-raw projection audit compared402,439 real rows across two source partitions with an independent DuckDB interpretation; all23 differences were zero (research/projection-audit). Do not describe it as a complete independent rescan of all nested raw source columns.

The original100k regression independently reproduces48,319 eligible Works,388 excluded same-Work loops and193 retained edges under its all-corpus/threshold0 development policy. It is a regression scenario, not the formal core result. Evidence: research/regression-100k-v5.json and pilot-oracle-no-loops.sql/.json.




