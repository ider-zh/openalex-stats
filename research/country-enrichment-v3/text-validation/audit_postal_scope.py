"""Read-only cross-check for postal-route country/location inconsistencies.

No rule or patch is changed. Defer review is narrower than replacing TR with CY:
the study identifies the affiliation's geographic location, not its mail transit.
"""
import hashlib,json,pathlib,time
import duckdb
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/text-validation')
patch=b/'validated-text-patch.parquet'
c=duckdb.connect();c.execute('set threads=8');c.execute("set memory_limit='24GB'")
c.read_parquet(str(patch)).create_view('passed')
start=time.time()
q="""with candidates as materialized (
 select id,evidence_json from read_parquet('/mnt/nvme0/public/openalex-stats/country-enrichment-io2/patch/*.parquet')
 where newly_known and not conflict and len(original_countries)=0 and len(institution_added)=0 and list_contains(text_added,'TR')
 and id in(select id from passed where list_contains(proposed_countries,'TR'))
), aff as (
 select p.id,json_extract_string(e.value,'$.raw_affiliation') as raw_affiliation
 from candidates p,json_each(p.evidence_json) e
 where json_extract_string(e.value,'$.country')='TR'
 and json_extract_string(e.value,'$.source')='raw_exact_tail_country'
)
select distinct id,raw_affiliation from aff
where regexp_matches(lower(raw_affiliation),'mersin[ ,./-]*10')
and regexp_matches(lower(raw_affiliation),'cyprus|kyrenia|girne|lefkosa|lefkoşa|famagusta|guzelyurt|güzelyurt|nicosia')
order by id,raw_affiliation"""
rows=[dict(id=r[0],raw_affiliation=r[1]) for r in c.execute(q).fetchall()]
out=b/'postal-location-scope-readonly.json'
assert not out.exists()
v=dict(status='read_only_scope_audit_not_applied',scope='passed text patch; TR tail derived from Mersin10 with explicit Cyprus-campus geographic context',rows=rows,works=len({r['id'] for r in rows}),patch_sha256=hashlib.sha256(patch.read_bytes()).hexdigest(),script_sha256=hashlib.sha256(pathlib.Path(__file__).read_bytes()).hexdigest(),seconds=time.time()-start,production_applied=False,automatic_country_replacement=False,notes='Potential geographic-location versus postal-routing mismatch; does not claim other TR-affiliated authors are absent. Review all evidence before any conservative whole-Work quarantine.')
out.write_text(json.dumps(v,ensure_ascii=False,indent=2)+'\n');print(json.dumps({k:v[k] for k in ['status','works','seconds','production_applied']}))
