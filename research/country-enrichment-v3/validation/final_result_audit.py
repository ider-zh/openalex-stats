"""Read-only independent final side-version statistics/model conservation audit."""
import hashlib
import json
import sys
from pathlib import Path

import duckdb

base, new, methods, output = map(Path, sys.argv[1:])
def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()
def read(path):
    return json.loads(path.read_text())
checks = []
def check(name, good, value=None):
    checks.append({"check": name, "passed": bool(good), "value": value})
    assert good, (name, value)

q = duckdb.connect()
q.execute("set threads=8")
q.execute("set memory_limit='20GB'")
for tag, folder in [("b", base), ("n", new)]:
    q.read_parquet(str(folder / "statistics.parquet")).create_view(tag)
keys = "scenario_id,country,year,scheme,level,discipline_id"
for tag in ["b", "n"]:
    dup = q.execute(f"select count(*) from (select {keys},count(*) z from {tag} group by all having z<>1)").fetchone()[0]
    check(tag + "_unique_statistic_keys", dup == 0, dup)
bad = q.execute("select count(*) from n where not isfinite(publications) or not isfinite(citations) or publications<0 or citations<0 or publications is null or citations is null").fetchone()[0]
check("finite_nonnegative_statistics", bad == 0, bad)
whole = q.execute("select count(*) from n where country_mode='whole' and (publications<>trunc(publications) or citations<>trunc(citations))").fetchone()[0]
check("whole_country_integrality", whole == 0, whole)
q.execute(f"create table change as select {keys},coalesce(b.publications,0) bp,coalesce(n.publications,0) np,coalesce(b.citations,0) bc,coalesce(n.citations,0) nc from b full outer join n using({keys})")
decrease = q.execute("select count(*) from change where country<>'UNKNOWN' and (np+1e-7<bp or nc+1e-7<bc)").fetchone()[0]
increase = q.execute("select count(*) from change where country='UNKNOWN' and (np>bp+1e-7 or nc>bc+1e-7)").fetchone()[0]
check("known_country_count_nondecrease", decrease == 0, decrease)
check("UNKNOWN_count_nonincrease", increase == 0, increase)
mass = q.execute("""select count(*) from (
 select scenario_id,year,scheme,level,sum(bp) a,sum(np) z,sum(bc) c,sum(nc) d from change
 where scenario_id like 'fractional-%' group by all
 having abs(a-z)>greatest(1e-6,abs(a)*1e-12) or abs(c-d)>greatest(1e-6,abs(c)*1e-12))""").fetchone()[0]
check("all_fractional_year_layer_mass_conservation", mass == 0, mass)
for fn in ["dictionary.json", "scenarios.json"]:
    check(fn + "_immutable", digest(base / fn) == digest(new / fn))
old_run, new_run = read(base / "run.json"), read(new / "run.json")
check("fixed_selected_corpus", old_run["selected_works"] == new_run["selected_works"])
check("fixed_reference_graph", old_run["graph"] == new_run["graph"])
coverage_fields = ["publication_works", "citation_events", "unclassified_publications", "unclassified_citations", "publication_classification_coverage", "citation_classification_coverage"]
before = {(r["scenario_id"], r["scheme"], r["level"]): r for r in read(base / "coverage.json")}
after = {(r["scenario_id"], r["scheme"], r["level"]): r for r in read(new / "coverage.json")}
check("coverage_key_sets_unchanged", before.keys() == after.keys())
check("raw_and_classification_totals_unchanged", all(before[k][f] == after[k][f] for k in before for f in coverage_fields))
normalization = []
for method, folder in [("spectral", new), ("java_reflections", methods)]:
    for kind, fn in [("ECI", "country_eci.parquet"), ("PCI", "discipline_pci.parquet")]:
        q.read_parquet(str(folder / fn)).create_view("scores", replace=True)
        bad = q.execute("select count(*) from scores where valid and (score is null or not isfinite(score))").fetchone()[0]
        check(method + kind + "_valid_scores_finite", bad == 0, bad)
        dup = q.execute("select count(*) from (select scenario_id,year,scheme,level,metric,id,count(*) z from scores group by all having z<>1)").fetchone()[0]
        check(method + kind + "_unique_scores", dup == 0, dup)
        result = q.execute("""select max(abs(a)),max(abs(v-1)),count(*) from (
         select scenario_id,year,scheme,level,metric,avg(score) a,avg(score*score) v from scores where valid group by all)""").fetchone()
        check(method + kind + "_population_standardization", result[0] is not None and result[0] <= 1e-6 and result[1] <= 1e-6, list(result))
        normalization.append({"method": method, "kind": kind, "max_abs_mean": result[0], "max_abs_second_moment_minus1": result[1], "valid_models": result[2]})
coverage = []
for end in [2025, 2026]:
    r = q.execute("""select sum(bp),sum(np),sum(bp) filter(where country='UNKNOWN'),sum(np) filter(where country='UNKNOWN'),sum(bc),sum(nc),sum(bc) filter(where country='UNKNOWN'),sum(nc) filter(where country='UNKNOWN') from change where scenario_id='fractional-concept-whole-0.3' and scheme='topics' and level='field' and year between 2000 and ?""", [end]).fetchone()
    p, pn, ub, un, c, cn, cb, cc = r
    coverage.append({"start": 2000, "end": end, "publication_works": p, "known_before": p - ub, "known_after": pn - un,
                     "recovered_publications": ub - un, "coverage_before": (p - ub) / p, "coverage_after": (pn - un) / pn,
                     "citation_events": c, "recovered_citation_events": cb - cc, "citation_coverage_before": (c - cb) / c, "citation_coverage_after": (cn - cc) / cn})
report = {"status": "passed_independent_final_statistics_and_model_audit", "base": str(base), "new": str(new), "methods": str(methods),
          "checks": checks, "normalization": normalization, "coverage": coverage, "external_sources_admitted": 0,
          "statistics_sha256": digest(new / "statistics.parquet"), "production_applied": False}
output.write_text(json.dumps(report, indent=2) + "\n")
print(json.dumps({k: r for k, r in report.items() if k not in ["checks", "normalization"]}))
