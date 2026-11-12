"""Independent DuckDB comparison of immutable baseline and enriched artifacts.

Count conservation, identity of Q/reference graph, all six scenarios, and matched
Java normalization baseline are checked separately from country/rank effects.
"""
import argparse
import csv
import hashlib
import json
from pathlib import Path

import duckdb


def digest(path):
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for b in iter(lambda: f.read(1 << 20), b""):
            h.update(b)
    return h.hexdigest()


def records(cursor):
    names = [x[0] for x in cursor.description]
    return [dict(zip(names, row)) for row in cursor.fetchall()]


def export(path, rows):
    with open(path, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0]))
        w.writeheader()
        w.writerows(rows)


def main():
    ap = argparse.ArgumentParser()
    for k in ("base", "new", "base_methods", "new_methods", "old_methods", "out"):
        ap.add_argument("--" + k.replace("_", "-"), required=True)
    a = ap.parse_args()
    out = Path(a.out)
    out.mkdir(exist_ok=False, parents=True)
    c = duckdb.connect()
    c.execute("set threads=8")
    c.execute("set memory_limit='24GB'")
    checks = {}
    def check(name, ok):
        checks[name] = bool(ok)
        if not ok:
            raise AssertionError(name)
    for tag, root in (("b", a.base), ("n", a.new)):
        c.read_parquet(str(Path(root) / "statistics.parquet")).create_view(tag)
    keys = "scenario_id,country,year,scheme,level,discipline_id"
    c.execute(f"""create table changes as select {keys},
      coalesce(b.publications,0) bp,coalesce(n.publications,0) np,
      coalesce(b.citations,0) bc,coalesce(n.citations,0) nc
      from b full outer join n using ({keys})""")
    check("known_country_counts_never_decrease", c.execute("select count(*)=0 from changes where country <> 'UNKNOWN' and (np+1e-7<bp or nc+1e-7<bc)").fetchone()[0])
    check("unknown_counts_never_increase", c.execute("select count(*)=0 from changes where country='UNKNOWN' and (np>bp+1e-7 or nc>bc+1e-7)").fetchone()[0])
    check("whole_country_cells_integral", c.execute("select count(*)=0 from n where country_mode='whole' and (publications<>trunc(publications) or citations<>trunc(citations))").fetchone()[0])
    check("fractional_mass_conserved_every_year_layer", c.execute("""select count(*)=0 from (
      select scenario_id,year,scheme,level,sum(bp) bp,sum(np) np,sum(bc) bc,sum(nc) nc
      from changes where scenario_id like 'fractional-%' group by all
      having abs(bp-np)>greatest(1e-6,abs(bp)*1e-12) or abs(bc-nc)>greatest(1e-6,abs(bc)*1e-12))""").fetchone()[0])
    runs = [json.load(open(Path(p)/"run.json")) for p in (a.base,a.new)]
    check("selected_corpus_unchanged", runs[0]["selected_works"] == runs[1]["selected_works"])
    check("reference_graph_unchanged", runs[0]["graph"] == runs[1]["graph"])
    for fn in ("dictionary.json", "scenarios.json"):
        check(fn + "_unchanged", digest(Path(a.base)/fn)==digest(Path(a.new)/fn))
    covs=[json.load(open(Path(p)/"coverage.json")) for p in (a.base,a.new)]
    invariant=("publication_works","citation_events","unclassified_publications","unclassified_citations","publication_classification_coverage","citation_classification_coverage")
    bk={(v["scenario_id"],v["scheme"],v["level"]):v for v in covs[0]}
    check("all_classification_and_raw_totals_unchanged", all(all(v[k]==bk[(v["scenario_id"],v["scheme"],v["level"])][k] for k in invariant) for v in covs[1]))
    coverage=[]
    for end in (2025,2026):
        vals=c.execute("""select sum(bp),sum(np),sum(bp) filter(where country='UNKNOWN'),sum(np) filter(where country='UNKNOWN'),sum(bc),sum(nc),sum(bc) filter(where country='UNKNOWN'),sum(nc) filter(where country='UNKNOWN') from changes where scenario_id='fractional-concept-whole-0.3' and scheme='topics' and level='field' and year between 2000 and ?""",[end]).fetchone()
        p, pn, ub, un, ct, ctn, cb, cn=vals
        coverage.append(dict(start=2000,end=end,publication_works=p,known_before=p-ub,known_after=pn-un,recovered_publications=ub-un,coverage_before=(p-ub)/p,coverage_after=(pn-un)/pn,delta_percentage_points=((pn-un)/pn-(p-ub)/p)*100,citation_events=ct,known_country_citations_before=ct-cb,known_country_citations_after=ctn-cn,recovered_citation_events=cb-cn,citation_coverage_before=(ct-cb)/ct,citation_coverage_after=(ctn-cn)/ctn))
    country_sql="""select country,sum(bp) as "before",sum(np) as "after",sum(np-bp) added,sum(bc) citations_before,sum(nc) citations_after,sum(nc-bc) citations_added from changes where scenario_id='whole-concept-whole-0.3' and scheme='topics' and level='field' and year between 2000 and 2025 group by country order by added desc"""
    countries=records(c.execute(country_sql))
    export(out/"country-whole-counts-2000-2025.csv",countries)
    export(out/"china-2012-2018-discipline-count-changes.csv", records(c.execute("select * from changes where country='CN' and year between 2012 and 2018 and scenario_id='whole-concept-whole-0.3'")))
    score_reports={}
    for kind,fn in (("country","country_eci.parquet"),("discipline","discipline_pci.parquet")):
        for method,base,new in (("spectral",a.base,a.new),("java_matched",a.base_methods,a.new_methods),("java_numeric_only",a.old_methods,a.base_methods)):
            k=f"{kind}_{method}"
            for tag,p in (("sb",base),("sn",new)):
                c.read_parquet(str(Path(p)/fn)).create_view(tag+"_source", replace=True)
                c.execute(f"create or replace view {tag} as select *, rank() over(partition by scenario_id,year,scheme,level,metric order by score desc) as rank from {tag}_source where valid")
            sk="scenario_id,year,scheme,level,metric,id"
            q=f"""select {sk},sb.score as "before",sn.score as "after",sn.score-sb.score score_delta,sb.rank before_rank,sn.rank after_rank,sb.rank-sn.rank rank_improved,sb.unstable before_unstable,sn.unstable after_unstable from sb full outer join sn using ({sk})"""
            df=records(c.execute(q))
            export(out/(k+"-changes.csv"),df)
            valid=[r for r in df if r["before"] is not None and r["after"] is not None]
            score_reports[k]={"rows":len(df),"validity_set_changed":len(df)-len(valid),"max_abs_score_delta":max(abs(r["score_delta"]) for r in valid),"rank_changed_rows":sum(r["rank_improved"] !=0 for r in valid),"max_rank_change":max(abs(r["rank_improved"]) for r in valid),"top_rank_changes_2025":sorted([r for r in valid if r["year"]==2025],key=lambda r:abs(r["rank_improved"]),reverse=True)[:12]}
    report={"status":"passed_independent_statistics_comparison","checks":checks,"coverage":coverage,"country_top_additions":countries[:15],"score_comparisons":score_reports,"base":a.base,"new":a.new,"java_matched_baseline":a.base_methods,"production_applied":False,"notes":["Topics primary-field counts include UNKNOWN discipline, so coverage denominators count unique works; Concepts hit totals cannot substitute for unique works.","Global fractional mass is conserved; whole-country assignment sums may rise for international works.","Java matched contrast isolates country effects from stable population-standardization repair.","Rank changes in unstable models are diagnostics, not robust policy conclusions."]}
    (out/"review.json").write_text(json.dumps(report,ensure_ascii=False,indent=2)+"\n")
    print(json.dumps({"out":str(out),"checks":checks,"coverage":coverage},ensure_ascii=False))


if __name__ == "__main__":
    main()
