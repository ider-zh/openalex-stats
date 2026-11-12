"""Publish text-only full-corpus overlay only after independent gate and quarantine."""
import pathlib,json,hashlib,duckdb,re,time
B=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/text-validation')
gate=B/'raw-gate.json'; g=json.loads(gate.read_text())['routes'][0]
assert g['Status']=='passed_source_supported_country_precision' and g['Selected']==256 and g['Successes']==255
assert g['VisibleComplete']/g['VisibleCompletenessObserved']>=.95
c=duckdb.connect();c.execute('set threads=24');c.execute("set memory_limit='48GB'")
glob='/mnt/nvme0/public/openalex-stats/country-enrichment-io2/patch/*.parquet'
filt='newly_known and not conflict and len(original_countries)=0 and len(institution_added)=0 and len(text_added)>0'
query="""with candidates as(select id,year,evidence_json from read_parquet(?) where FILTER and list_contains(text_added,'DE'))
 select distinct p.id,p.year,json_extract_string(e.value,'$.raw_affiliation') as raw_affiliation
 from candidates p,json_each(p.evidence_json) e
 where json_extract_string(e.value,'$.country')='DE'
 and json_extract_string(e.value,'$.source')='raw_exact_tail_country'
 and contains(regexp_replace(lower(replace(json_extract_string(e.value,'$.raw_affiliation'),'ß','ss')),'[^a-z0-9]','','g'),'fhcampuswien')
 and contains(regexp_replace(lower(replace(json_extract_string(e.value,'$.raw_affiliation'),'ß','ss')),'[^a-z0-9]','','g'),'favoritenstrasse226')
 and contains(json_extract_string(e.value,'$.raw_affiliation'),'1100')""".replace('FILTER',filt)
q=[dict(zip(['id','year','raw_affiliation'],r)) for r in c.execute(query,[glob]).fetchall()]
assert 4237692947 in {r['id'] for r in q},'known failure must be isolated'
qp=B/'quarantine.json';qp.write_text(json.dumps(dict(error_class='FH Campus Wien exact main-campus address incorrectly labeledDE',basis='Independent 2016 official address is Austria',policy='Defer original erroneous country; do not replace with AT or retune original parser; retain original255/256 audit denominator',records=q),ensure_ascii=False,indent=2)+'\n')
c.execute('create temp table quarantine as select unnest(?) as id',[[r['id'] for r in q]])
wrapper=dict(status='passed',route='v1_strict_text_only',independent_gate_path=str(gate),independent_gate_sha256=hashlib.sha256(gate.read_bytes()).hexdigest(),
 precision=g['Precision'],wilson_lower=g['WilsonLower'],wilson_upper=g['WilsonUpper'],audited_candidates=256,supported_candidates=255,
 source_visible_complete=g['VisibleComplete'],source_visible_observed=g['VisibleCompletenessObserved'],full_paper_complete=None,
 mandatory_quarantine_path=str(qp),mandatory_quarantine_sha256=hashlib.sha256(qp.read_bytes()).hexdigest(),quarantined_works=len({r['id'] for r in q}),
 audit_scope='Precision measured before mandatory known-error-class quarantine; no changed-parser or post-quarantine holdout accuracy claim',production_applied=False,user_review_complete=False)
wp=B/'passed-gate.json';wp.write_text(json.dumps(wrapper,ensure_ascii=False,indent=2)+'\n')
target=B/'validated-text-patch.parquet'
if target.exists():raise RuntimeError('immutable patch already exists')
c.execute("copy(select id,year,is_xpac,text_added as proposed_countries,'v1_strict_text_only' as source_route,cast([] as varchar[]) as original_countries,'unknown' as country_set_complete from read_parquet('"+glob+"') where "+filt+" and id not in(select id from quarantine) order by id) to '"+str(target)+"' (format parquet,compression zstd)")
n=c.execute('select count(*),count(distinct id) from read_parquet(?)',[str(target)]).fetchone();assert n[0]==n[1]
ph=hashlib.sha256(target.read_bytes()).hexdigest();gh=hashlib.sha256(wp.read_bytes()).hexdigest()
manifest=dict(status='passed_validated_overlay',version='v3-text-only',rows=n[0],patch_sha256=ph,gates=[dict(route=wrapper['route'],path=str(wp),sha256=gh)],
 original_version_preserved=True,production_applied=False,quarantine=wrapper['mandatory_quarantine_path'])
(B/'overlay-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
print(json.dumps(dict(rows=n[0],quarantine_works=len({r['id'] for r in q}),patch_sha256=ph,gate_sha256=gh,records=q),ensure_ascii=False,indent=2),flush=True)
