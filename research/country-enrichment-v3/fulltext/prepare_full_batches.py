"""Materialize bounded full application batches only after both independent gates.

This is orchestration, not a change to frozen identity/country rules. Application
keeps development papers but tags them; three already quarantined bad Work input
identities can never enter the proposed application patch.
"""
import pathlib,json,hashlib,duckdb,argparse,time
p=argparse.ArgumentParser();p.add_argument('--recovery-ledger',required=True);p.add_argument('--overall-gate',required=True);p.add_argument('--net-gate',required=True);p.add_argument('--batch-size',type=int,default=10000);a=p.parse_args()
assert 1<=a.batch_size<=10000
root=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');out=root/'fulltext/full';out.mkdir(exist_ok=True)
assert not (out/'application-manifest.json').exists(),'Immutable full application manifest already exists'
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
gates=[]
for path,route in [(a.overall_gate,'pmc_jats'),(a.net_gate,'pmc_jats/net_new')]:
 gate=json.load(open(path));assert gate['status']=='passed' and gate['route']==route
 assert gate['audited_clusters']>=128 and gate['full_paper_complete'] is None
 assert sha(gate['independent_gate_path'])==gate['independent_gate_sha256']
 gates.append(dict(route=route,path=path,sha256=sha(path),independent_gate_sha256=gate['independent_gate_sha256']))
ledger=json.load(open(a.recovery_ledger));assert ledger['status']=='complete_passed_recovery_scope_for_fulltext_net_cohort'
c=duckdb.connect();c.execute('set threads=16');c.execute("set memory_limit='96GB'");c.execute('create temp table recovered(id ubigint)')
for source in ledger['passed_sources']:
 assert source['gate_status']=='passed' and sha(source['gate_path'])==source['gate_sha256'] and sha(source['patch_path'])==source['patch_sha256']
 c.execute('insert into recovered select cast(id as ubigint) from read_parquet(?)',[source['patch_path']])
eligible=root/'fulltext/pmc-source-eligible.parquet'
cur=c.execute('select * from read_parquet(?) where id not in(select id from recovered) order by pmcid,id',[str(eligible)])
cols=[d[0] for d in cur.description];rows=[dict(zip(cols,r)) for r in cur.fetchall()]
parts={r['source_index']:r for r in json.loads((root/'shared/unknown/manifest.json').read_text())['parts']}
devwork=set(json.loads((root/'validation/development-exclusions.json').read_text())['ids']);devkeys=json.loads((root/'validation/development-paper-identities-r2.json').read_text());devdoi=set(devkeys['dois']);devpmid=set(devkeys['pmids']);devpmcid=set(devkeys['pmcids'])
quarantine={19239315,26184156,34102155};quarantine_rows=[];batches=[];pending=[];development_count=0
rule_sha=sha(root/'fulltext/frozen-policy.json')
def flush(items):
 if not items:return
 idx=len(batches);folder=out/('batch-%05d'%idx);folder.mkdir(exist_ok=True);works=[];pmc=[]
 for r in items:
  source=parts[r['source_index']];dev=r['id'] in devwork or r['doi'] in devdoi or r['pmid'] in devpmid or r['pmcid'] in devpmcid
  w=dict(id=r['id'],work_id='https://openalex.org/W'+str(r['id']),ordinal=r['ordinal'],year=r['year'],type=r['type'],is_xpac=r['is_xpac'],title=r['title'],doi=r['doi'],pmid=r['pmid'],pmcid=r['pmcid'],source_index=r['source_index'],raw_row_index=r['raw_row_index'],raw_path=source['raw_path'],cache_path=source['cache_path'],locations=[],baseline_countries=[],stratum='full_pmc_net_application',inclusion_probability=1,design_weight=1,development_member=dev)
  works.append(w);pmc.append(dict(id=r['id'],pmcid=r['pmcid'],doi_match=r['doi_pmcid']==r['pmcid'],pmid_match=r['pmid_pmcid']==r['pmcid'],inventory_metadata_keys=r['metadata_keys']))
 raw=json.dumps(works,ensure_ascii=False,sort_keys=True,separators=(',',':')).encode();sample_sha=hashlib.sha256(raw).hexdigest()
 sample=folder/'metadata.json';sample.write_text(json.dumps(dict(works=works,sample_sha256=sample_sha,precision_only=False,application_gates=gates,policy_sha256=rule_sha),ensure_ascii=False,indent=2)+'\n')
 plan=folder/'xml-plan.json';plan.write_text(json.dumps(dict(status='frozen_full_application_batch',mode='full_net_application_after_dual_independent_gates',sample_sha256=sample_sha,sample_size=len(works),pmc=pmc,pdf=[],pdf_strata=[]),ensure_ascii=False,indent=2)+'\n')
 batches.append(dict(batch_index=idx,path=str(folder),works=len(works),metadata_path=str(sample),metadata_sha256=sha(sample),plan_path=str(plan),plan_sha256=sha(plan),development_members=sum(w['development_member'] for w in works)))
last_pmcid=None
for r in rows:
 if r['id'] in quarantine:
  quarantine_rows.append(dict(id=r['id'],source_route='pmc_jats',status='quarantined_known_wrong_original_work_identity',proposed_countries=[],original_countries=[],policy_outcomes_unmodified=True,reason='Root-specified pre-existing metadata identity quarantine; never eligible for main application patch'));continue
 if pending and len(pending)>=a.batch_size and r['pmcid']!=last_pmcid:flush(pending);pending=[]
 pending.append(r);last_pmcid=r['pmcid']
flush(pending)
(out/'application-quarantine.json').write_text(json.dumps(dict(quarantined_work_ids=sorted(quarantine),present_in_runtime_scope=quarantine_rows,automatic_rules_changed=False),indent=2)+'\n')
manifest=dict(status='full_runtime_batches_frozen_after_dual_passed_gates_not_yet_executed',created_at_utc=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),source_eligible_rows=c.execute('select count(*) from read_parquet(?)',[str(eligible)]).fetchone()[0],net_runtime_rows=len(rows),quarantined_rows=len(quarantine_rows),scheduled_rows=sum(b['works'] for b in batches),batches=batches,recovery_ledger_path=a.recovery_ledger,recovery_ledger_sha256=sha(a.recovery_ledger),application_gates=gates,source_frame_sha256=sha(eligible),policy_sha256=rule_sha,development_policy='Included only through unchanged automatic frozen engine; tagged; no manual override',country_set_complete='unknown',production_applied=False)
(out/'application-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n');print('Full PMC batches frozen',len(batches),'runtime',len(rows),'scheduled',manifest['scheduled_rows'],flush=True)
