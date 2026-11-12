"""Freeze the append-only fresh net PMC validation domain before source outcomes.

Only explicit independently passed recoveries may be excluded. This script emits
source eligibility and a blind sample, never country predictions or live patches.
"""
import pathlib,json,hashlib,duckdb,time,argparse
p=argparse.ArgumentParser();p.add_argument('--recovery-ledger',required=True);a=p.parse_args()
root=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112')
out=root/'fulltext';dest=out/'net-validation';dest.mkdir(exist_ok=True)
assert not (dest/'cohort-acceptance.json').exists(),'Frozen net cohort already exists; do not overwrite'
ledgerpath=pathlib.Path(a.recovery_ledger);ledgerbytes=ledgerpath.read_bytes();ledger=json.loads(ledgerbytes)
assert ledger['status']=='complete_passed_recovery_scope_for_fulltext_net_cohort'
rulespath=out/'frozen-policy.json';protocolpath=root/'validation/protocol-r2.json'
assert hashlib.sha256(rulespath.read_bytes()).hexdigest()=='09312f219af3980328199a6cad05386a71ad8b957e505cb847ae6eefae1c28a1'
assert hashlib.sha256(protocolpath.read_bytes()).hexdigest()=='29b273128655f6a5dcaa3f455496e172ee29f6ad55b359068e20ea810b2410e5'
protocol3path=root/'validation/protocol-r3.json'
assert hashlib.sha256(protocol3path.read_bytes()).hexdigest()=='3413756a7e94e2d95fda7a54348061227320ed86ec1844b19c3e0d5c5546c6a2'
seed='openalex-country-enrichment-v3-20261112-pmc-net-eligible-validation-01';cap=10000
c=duckdb.connect();c.execute('set threads=24');c.execute("set memory_limit='96GB'")
c.execute('create temp table recovered(id ubigint)');verified=[]
for item in ledger['passed_sources']:
 patch=pathlib.Path(item['patch_path']);gate=pathlib.Path(item['gate_path'])
 assert hashlib.sha256(patch.read_bytes()).hexdigest()==item['patch_sha256']
 assert hashlib.sha256(gate.read_bytes()).hexdigest()==item['gate_sha256']
 # The root ledger explicitly binds the passed gate; do not infer pass from file existence.
 assert item['gate_status']=='passed' and item['source_name']
 c.execute('insert into recovered select cast(id as ubigint) from read_parquet(?)',[str(patch)])
 verified.append(item)
devwork=json.loads((root/'validation/development-exclusions.json').read_text())['ids']
devpath=root/'validation/development-paper-identities-r2.json';probpath=root/'validation/current-probability-paper-identities.json'
dev=json.loads(devpath.read_text());prob=json.loads(probpath.read_text())
originalpath=out/'validation-cohort-metadata.json';original=json.loads(originalpath.read_text())['works']
c.execute('create temp table excluded_work as select unnest(?)::ubigint id',[sorted(set(devwork+prob['work_ids']+[r['id'] for r in original]))])
keycounts={}
for table,key,field in [('excluded_doi','dois','doi'),('excluded_pmid','pmids','pmid'),('excluded_pmcid','pmcids','pmcid')]:
 keys=sorted(set(dev[key]+prob[key]+[str(r[field]) for r in original if r.get(field)]));keycounts[key]=len(keys)
 c.execute('create temp table '+table+' as select unnest(?) identifier',[keys])
eligible=out/'pmc-source-eligible.parquet'
c.execute("create view eligible as select * from read_parquet('"+str(eligible)+"') where id not in(select id from excluded_work) and id not in(select id from recovered) and coalesce(doi,'') not in(select identifier from excluded_doi) and coalesce(pmid,'') not in(select identifier from excluded_pmid) and pmcid not in(select identifier from excluded_pmcid)")
N=c.execute('select count(*) from eligible').fetchone()[0];n=min(N,cap)
c.execute("copy(select *,sha256('"+seed+"'||chr(10)||cast(id as varchar)) validation_hash from eligible order by validation_hash limit "+str(cap)+") to '"+str(dest/'cohort.parquet')+"' (format parquet,compression zstd)")
cur=c.execute('select * from read_parquet(?) order by validation_hash',[str(dest/'cohort.parquet')]);rows=[dict(zip([d[0] for d in cur.description],r)) for r in cur.fetchall()]
parts={p['source_index']:p for p in json.loads((root/'shared/unknown/manifest.json').read_text())['parts']}
works=[];pmc=[]
for r in rows:
 source=parts[r['source_index']]
 w=dict(id=r['id'],work_id='https://openalex.org/W'+str(r['id']),ordinal=r['ordinal'],year=r['year'],type=r['type'],is_xpac=r['is_xpac'],title=r['title'],doi=r['doi'],pmid=r['pmid'],pmcid=r['pmcid'],raw_path=source['raw_path'],cache_path=source['cache_path'],source_index=r['source_index'],raw_row_index=r['raw_row_index'],locations=[],baseline_countries=[],stratum='net_source_precision_cohort',inclusion_probability=n/N if N else 0,design_weight=N/n if n else 0,validation_hash=r['validation_hash'])
 works.append(w);pmc.append(dict(id=r['id'],pmcid=r['pmcid'],doi_match=r['doi_pmcid']==r['pmcid'],pmid_match=r['pmid_pmcid']==r['pmcid'],inventory_metadata_keys=r['metadata_keys']))
raw=json.dumps(dict(works=works,source_eligible_N=N,precision_only=True),ensure_ascii=False,sort_keys=True,separators=(',',':')).encode();sha=hashlib.sha256(raw).hexdigest()
(dest/'cohort-metadata.json').write_text(json.dumps(dict(works=works,sample_sha256=sha,source_eligible_N=N,precision_only=True),ensure_ascii=False,indent=2)+'\n')
(dest/'xml-plan.json').write_text(json.dumps(dict(status='frozen',mode='fresh_net_domain_precision_validation_only',sample_sha256=sha,sample_size=n,seed=seed,pmc=pmc,pdf=[],pdf_strata=[]),ensure_ascii=False,indent=2)+'\n')
(dest/'cohort-acceptance.json').write_text(json.dumps(dict(status='frozen_fresh_net_cohort_before_source_outcomes',eligible_N=N,cap=cap,n=n,pi=n/N if N else 0,seed=seed,sample_sha256=sha,recovery_ledger_sha256=hashlib.sha256(ledgerbytes).hexdigest(),passed_sources=verified,development_paper_keys_sha256=hashlib.sha256(devpath.read_bytes()).hexdigest(),current_probability_paper_keys_sha256=hashlib.sha256(probpath.read_bytes()).hexdigest(),original3000_sample_file_sha256=hashlib.sha256(originalpath.read_bytes()).hexdigest(),policy_sha256=hashlib.sha256(rulespath.read_bytes()).hexdigest(),protocol_r2_sha256=hashlib.sha256(protocolpath.read_bytes()).hexdigest(),protocol_r3_sha256=hashlib.sha256(protocol3path.read_bytes()).hexdigest(),paper_exclusion_counts=keycounts,production_applied=False),indent=2)+'\n')
print('Fresh PMC net cohort',n,'of',N,flush=True)
