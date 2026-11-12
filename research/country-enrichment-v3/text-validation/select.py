"""Freeze a fresh candidate-precision sample from the unchanged v1 text rule."""
import duckdb,json,pathlib,hashlib,time
B=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112')
O=B/'text-validation';O.mkdir(exist_ok=True)
seed='country-enrichment-v3-text-precision-holdout-20261112-v1'
rules=pathlib.Path('/mnt/nvme0/public/openalex-stats/country-enrichment-io2/rules.json')
ex=B/'validation/development-exclusions.json'
policy=dict(route='v1_strict_text_only_nonconflicting_original_unknown',seed=seed,n=256,
 selection='bottom SHA256(seed + newline + decimal WorkID), excluding all frozen development IDs',
 eligibility='newly_known AND NOT conflict AND original_countries=[] AND institution_added=[] AND text_added nonempty',
 countries='text_added only; no institution-only, known-country extensions or v1b',
 frozen_rules_sha256=hashlib.sha256(rules.read_bytes()).hexdigest(),
 exclusions_sha256=hashlib.sha256(ex.read_bytes()).hexdigest(),
 gate=dict(min_candidates=128,point_precision_min=.99,wilson_two_sided_95_lower_min=.97,unverified_counts_as_failure=True),
 independent_review='Re-read original authorship strings and identity; source-concordance, not author-confirmed global truth',
 no_same_holdout_retuning=True,production_applied=False)
p=O/'frozen-policy.json'
if p.exists():assert json.loads(p.read_text())==policy
else:p.write_text(json.dumps(policy,indent=2)+'\n')
print('policy_sha',hashlib.sha256(p.read_bytes()).hexdigest(),flush=True)
c=duckdb.connect();c.execute('set threads=24');c.execute("set memory_limit='64GB'")
c.execute('create temp table excluded as select unnest(?) as id',[json.loads(ex.read_text())['ids']])
glob='/mnt/nvme0/public/openalex-stats/country-enrichment-io2/patch/*.parquet'
filt='newly_known and not conflict and len(original_countries)=0 and len(institution_added)=0 and len(text_added)>0 and id not in(select id from excluded)'
N=c.execute('select count(*) from read_parquet(?) where '+filt,[glob]).fetchone()[0]
c.execute("create temp table wanted as select id,sha256(?||chr(10)||cast(id as varchar)) as h from read_parquet(?) where "+filt+' order by h,id limit 256',[seed,glob])
cur=c.execute('select p.*,w.h as validation_hash from read_parquet(?) p join wanted w using(id) order by w.h,p.id',[glob])
cols=[x[0] for x in cur.description];rows=[dict(zip(cols,r)) for r in cur.fetchall()]
assert len(rows)==256 and len({r['id'] for r in rows})==256
cur=c.execute('select * from read_parquet(?) where id in(select id from wanted)',[str(B/'shared/unknown/part-*.parquet')]);idx={r[0]:dict(zip([x[0] for x in cur.description],r)) for r in cur.fetchall()}
assert len(idx)==256
manifest=json.loads((B/'shared/unknown/manifest.json').read_text());parts={r['source_index']:r for r in manifest['parts']}
for r in rows:
 i=idx[r['id']];r['position']=dict(i,raw_path=parts[i['source_index']]['raw_path']);assert r['year']==i['year'] and r['type']==i['type'] and r['is_xpac']==i['is_xpac']
result=dict(status='frozen_blind_precision_sample',population=N,n=256,seed=seed,policy_sha256=hashlib.sha256(p.read_bytes()).hexdigest(),works=rows)
(O/'sample.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
print('population',N,'sample',len(rows),'sha',hashlib.sha256((O/'sample.json').read_bytes()).hexdigest(),flush=True)
