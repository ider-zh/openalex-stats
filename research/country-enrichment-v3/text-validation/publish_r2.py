"""Preserve first stage, conservatively defer reviewed postal-location cases.

No replacement countries, changed extractor, new precision claim, or removed
holdout failures. Prior text recoveries stay excluded from the frozen external
net frame even where this final batch chooses to defer application.
"""
import hashlib,json,pathlib,duckdb
root=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112')
b=root/'text-validation';out=root/'text-final-r2';out.mkdir(exist_ok=False)
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
old=b/'validated-text-patch.parquet';assert sha(old)=='4d58bb4cf77acc084a2084359248b973d11d392304c77ba6b90b13e22b5ae3e7'
scope=b/'postal-location-scope-readonly.json';review=root/'validation/postal-location-scope-independent-review.json'
s=json.load(open(scope));r=json.load(open(review));assert r['status']=='independent_readonly_scope_review' and r['unique_work_n']==78 and r['scope_source_sha256']==sha(scope) and r['patch_sha256']==sha(old)
assert not r['original_text256_intersection'] and all(not v['intersection'] for v in r['sample_intersections'].values())
ids=sorted({x['id'] for x in s['rows']});assert len(ids)==78
defer=dict(status='final_batch_whole_work_deferral',ids=ids,reason='Explicit Cyprus-campus affiliation plus Mersin10 mail routing. Conservative scope deferral, not automatic country replacement nor proof all other TR authors are absent.',original_production_unchanged=True,source_scope_path=str(scope),source_scope_sha256=sha(scope),independent_review_path=str(review),independent_review_sha256=sha(review),original_holdout_denominators_preserved=True,frozen_external_net_ledger_preserved=True,applies_to_all_source_routes_in_current_batch=True)
dp=out/'batch-deferred-work-ids.json';dp.write_text(json.dumps(defer,indent=2)+'\n')
c=duckdb.connect();c.execute('set threads=8');c.read_parquet(str(old)).create_view('original');c.execute('create table deferred as select unnest(?) id',[ids])
assert c.execute("select count(*) from original join deferred using(id) where list_contains(proposed_countries,'TR') and len(original_countries)=0").fetchone()[0]==78
target=out/'validated-text-patch.parquet';c.sql('select * from original anti join deferred using(id) order by id').write_parquet(str(target),compression='zstd')
n,u=c.execute('select count(*),count(distinct id) from read_parquet(?)',[str(target)]).fetchone();assert n==u==2667190
for q in ['select id,year,is_xpac,proposed_countries from read_parquet(?) except select id,year,is_xpac,proposed_countries from original','select id,year,is_xpac,proposed_countries from original anti join deferred using(id) except select id,year,is_xpac,proposed_countries from read_parquet(?)']:
 assert c.execute('select count(*) from ('+q+')',[str(target)]).fetchone()[0]==0
gate=json.load(open(b/'passed-gate.json'));gate.update(application_scope_version='v3-text-final-r2',original_passed_gate_path=str(b/'passed-gate.json'),original_passed_gate_sha256=sha(b/'passed-gate.json'),additional_conservative_batch_deferral_path=str(dp),additional_conservative_batch_deferral_sha256=sha(dp),additional_deferred_works=78,audit_scope='Original255/256 before known-error quarantine retained. New application scope conservatively defers78 reviewed postal cases without claiming new conditional holdout accuracy or altering failed external gates.',user_review_complete=False)
gp=out/'passed-gate.json';gp.write_text(json.dumps(gate,indent=2)+'\n')
manifest=dict(status='passed_validated_overlay',version='v3-text-final-r2',rows=n,patch_sha256=sha(target),gates=[dict(route='v1_strict_text_only',path=str(gp),sha256=sha(gp))],original_version_preserved=True,production_applied=False,previous_text_patch_path=str(old),previous_text_patch_sha256=sha(old),batch_defer_path=str(dp),batch_defer_sha256=sha(dp),excluded_since_stage1=78,external_routes_applied=[],note='Only the independent-precision-passed text route is admitted. External route failures and unverified evidence are reported separately; machine candidate counts do not become recoveries.')
(out/'overlay-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
(out/'independent-copy-conservation.json').write_text(json.dumps(dict(status='passed_exact_SQL_retained_row_copy',before=2667268,deferred=78,after=n,new_or_modified_retained_rows=0,production_applied=False),indent=2)+'\n')
print(json.dumps(manifest))
