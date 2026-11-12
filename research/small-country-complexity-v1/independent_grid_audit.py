"""Independently check diagnostic ranks, tie-aware rho and retained mass."""
import csv
import hashlib
import json
import sys
from collections import defaultdict
from pathlib import Path

import numpy as np

folder = Path(sys.argv[1])
reports = json.loads((folder / "experiments.json").read_text())
fields = ["corpus", "year", "scheme", "level", "metric", "kind", "parameter", "method"]
def key(row):
    return tuple(str(row[k]) if k != "parameter" else str(float(row[k])) for k in fields)
lookup = {key(r): r for r in reports}
assert len(lookup) == 1152
groups = defaultdict(list)
with (folder / "all-country-diagnostic-ranks.csv").open(newline="", encoding="utf8") as stream:
    for row in csv.DictReader(stream):
        groups[key(row)].append(row)
assert groups.keys() == lookup.keys()
def avg_ranks(values):
    values = np.asarray(values)
    return np.asarray([1 + np.count_nonzero(values > value) + (np.count_nonzero(values == value) - 1) / 2 for value in values])
def competition(values):
    values = np.asarray(values)
    return np.asarray([1 + np.count_nonzero(values > value) for value in values])
max_error = 0
for case, rows in groups.items():
    report = lookup[case]
    active = [r for r in rows if r["matched_after_score"]]
    assert len(active) == report["active_countries"]
    vals = [float(r["matched_after_score"]) for r in active]
    ranks = competition(vals)
    for r, rank in zip(active, ranks):
        assert int(r["matched_after_rank"]) == rank
        assert float(r["matched_after_score"]) == float(r["raw_after_score"]) * float(r["comparison_only_multiplier"])
    common = [r for r in active if r["before_score"]]
    if len(common) >= 3:
        a = avg_ranks([float(r["before_score"]) for r in common])
        b = avg_ranks([float(r["matched_after_score"]) for r in common])
        if a.std() and b.std():
            rho = float(np.corrcoef(a, b)[0, 1])
            delta = abs(rho - report["common_country_spearman"])
            max_error = max(max_error, delta)
            assert delta < 1e-12
    metric = "fractional_citation_events" if report["metric"] == "citations" else "fractional_publications"
    total = sum(float(r[metric]) for r in rows)
    kept = sum(float(r[metric]) for r in active)
    assert abs(total - report["total_known_country_fractional_attribution_mass"]) <= max(1e-7, total * 1e-12)
    assert abs((kept / total if total else 0) - report["ranked_fraction"]) < 1e-12
    if report["kind"] == "display_only":
        assert all(r["before_score"] == r["raw_after_score"] for r in active)
        assert not common or report["common_country_spearman"] is None or abs(report["common_country_spearman"] - 1) < 1e-12
result = {"status": "passed_independent_csv_summary_audit", "cases": len(groups),
          "rows": sum(map(len, groups.values())), "common_country_spearman_max_abs_error": max_error,
          "checks": ["competition ties", "coupled sign multiplication", "common-country average-tie Spearman", "active count", "fractional attribution retention", "display-only scores unchanged"],
          "input_sha256": hashlib.sha256((folder / "all-country-diagnostic-ranks.csv").read_bytes()).hexdigest(),
          "production_changed": False}
(folder / "independent-grid-audit.json").write_text(json.dumps(result, indent=2) + "\n")
print(json.dumps(result))
