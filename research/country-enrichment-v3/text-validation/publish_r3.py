"""Final independent-reviewed conservative scope; immutable first-stage retained."""
import hashlib,json,pathlib,duckdb
root=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112')
b=root/'text-validation';out=root/'text-final-r3';out.mkdir(exist_ok=False)
def sha(p):return hashlib.sha256(p.read_bytes()).hexdigest()
old=b/'validated-text-patch.parquet';assert sha(old)=='4d58bb4cf77acc084a2084359248b973d11d392304c77ba6b90b13e22b5ae3e7'
review=root/'validation/final142-defer-independent-review.json';r=json.load(open(review))
assert r['status']=='independent_scope_union_review_passed' and r['union_unique_work_n']==142
assert r['original_patch_unknown_and_proposed_country_checks']==[142,142,142]
assert all(not x for x in r['pairwise_intersections'].values())
assert not r['sample_intersections']['text256'] and not r['sample_intersections']['pubmed-net-cluster-sample.json']
assert r['sample_intersections']['crossref-overall-cluster-sample.json']==[4285021191]
postal=b/'postal-location-scope-readonly.json';canada=root/'fulltext/text-known-canada-precise-independent-scope.json'
assert sha(postal)==r['source_postal_scope_sha256'] and sha(canada)==r['source_canada_cyprus_scope_sha256']
p=json.load(open(postal));a=json.load(open(canada))
ids=sorted({x['id'] for x in p['rows']}|set(a['precise_scope_ids'])|set(a['cyprus_near_match_work_ids']))
assert ids==sorted(r['ids']) and len(ids)==142
defer=dict(status='final_batch_whole_work_deferral',ids=ids,groups=r['group_n'],reason='Reviewed ambiguous/erroneous geographic evidence:78 explicit Cyprus postal-routing cases,50 precise Toronto/Lakehead address cases,14 explicit NorthernCyprusCampus cases. Whole-Work conservative deferral, not automatic country replacement or proof no other US/TR author exists.',independent_review_path=str(review),independent_review_sha256=sha(review),source_scope_artifacts=[dict(path=str(x),sha256=sha(x)) for x in [postal,canada]],original_version_preserved=True,production_applied=False,original_holdout_denominators_preserved=True,external_net_exclusion_membership_preserved=True,all_current_batch_routes_defer_these_ids=True)
dp=out/'batch-deferred-work-ids.json';dp.write_text(json.dumps(defer,indent=2)+'\n')
c=duckdb.connect();c.execute('set threads=8');c.read_parquet(str(old)).create_view('original');c.execute('create table deferred as select unnest(?) id',[ids])
assert c.execute('select count(*) from original join deferred using(id) where len(original_countries)=0').fetchone()[0]==142
target=out/'validated-text-patch.parquet';c.sql('select * from original anti join deferred using(id) order by id').write_parquet(str(target),compression='zstd')
n,u=c.execute('select count(*),count(distinct id) from read_parquet(?)',[str(target)]).fetchone();assert n==u==2667126
for q in ['select id,year,is_xpac,proposed_countries from read_parquet(?) except select id,year,is_xpac,proposed_countries from original','select id,year,is_xpac,proposed_countries from original anti join deferred using(id) except select id,year,is_xpac,proposed_countries from read_parquet(?)']:
 assert c.execute('select count(*) from ('+q+')',[str(target)]).fetchone()[0]==0
gate=json.load(open(b/'passed-gate.json'));gate.update(application_scope_version='v3-text-final-r3',original_passed_gate_path=str(b/'passed-gate.json'),original_passed_gate_sha256=sha(b/'passed-gate.json'),additional_conservative_batch_deferral_path=str(dp),additional_conservative_batch_deferral_sha256=sha(dp),additional_deferred_works=142,audit_scope='Original255/256 and Wilson interval before quarantine retained. Conservative reviewed application deferral142 is not a changed extractor/new conditional accuracy claim; all external failures and denominators retained.',user_review_complete=False)
gp=out/'passed-gate.json';gp.write_text(json.dumps(gate,indent=2)+'\n')
manifest=dict(status='passed_validated_overlay',version='v3-text-final-r3',rows=n,patch_sha256=sha(target),gates=[dict(route='v1_strict_text_only',path=str(gp),sha256=sha(gp))],original_version_preserved=True,production_applied=False,previous_text_patch_path=str(old),previous_text_patch_sha256=sha(old),batch_defer_path=str(dp),batch_defer_sha256=sha(dp),excluded_since_stage1=142,external_routes_applied=[],note='Only independently-precision-passed text route admitted; external failed/unverified candidate streams never become recovered data.')
(out/'overlay-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
(out/'exact-copy-conservation.json').write_text(json.dumps(dict(status='passed_exact_SQL_retained_row_copy',before=2667268,deferred=142,after=n,new_or_modified_retained_rows=0,source_independent_review_sha256=sha(review),production_applied=False),indent=2)+'\n')
print(json.dumps(manifest))
