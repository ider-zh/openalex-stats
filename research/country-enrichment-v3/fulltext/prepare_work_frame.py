"""Prepare source-eligible UNKNOWN frame and frozen source-precision cohort only.
No country patches are produced before independent route gate.
"""
import pathlib,json,hashlib,duckdb,time
root=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112')
out=root/'fulltext';src=out/'source-index';manifest=root/'shared/identifiers/manifest.json'
if not manifest.exists():raise SystemExit('SOURCE_NOT_COMPLETE: shared identifiers manifest not published')
source=json.loads(manifest.read_text());unknown=json.loads((root/'shared/unknown/manifest.json').read_text())
parts=unknown['parts'];paths=[];partmap={}
for p in parts:
 f=root/'shared/identifiers'/('part-%05d.parquet'%p['source_index']);r=json.loads(f.with_suffix('.json').read_text())
 assert r['rows']==p['rows'] and r['raw_identity_failures']==0 and r['input_part_sha256']==p['sha256']
 paths.append(str(f));partmap[p['source_index']]=p
assert sum(p['rows'] for p in parts)==277418468 and len(paths)==2446
policy=(out/'frozen-policy.json').read_bytes();cfg=json.loads(policy);seed=cfg['validation_cohort']['first_stage_seed'];cap=cfg['validation_cohort']['first_stage_cap']
c=duckdb.connect();c.execute('set threads=32');c.execute("set memory_limit='192GB'")
c.execute("set temp_directory='"+str(out/'temp')+"'")
c.read_parquet(paths).create_view('identifiers')
c.execute("create view dm as select * from read_parquet('"+str(src/'pmc-doi-lookup.parquet')+"')")
c.execute("create view pm as select * from read_parquet('"+str(src/'pmc-pmid-lookup.parquet')+"')")
c.execute("create view inv as select * from read_parquet('"+str(src/'pmc-oa-inventory.parquet')+"')")
start=time.time();eligible=out/'pmc-source-eligible.parquet'
if not eligible.exists():
 c.execute("""create temp table matched as select i.*,dm.pmcid as doi_pmcid,dm.pmcid_count as doi_pmcid_count,
 pm.pmcid as pmid_pmcid,pm.pmcid_count as pmid_pmcid_count,
 case when coalesce(dm.pmcid_count,0)>1 or coalesce(pm.pmcid_count,0)>1 then 'ambiguous_identifier'
 when dm.pmcid is not null and pm.pmcid is not null and dm.pmcid!=pm.pmcid then 'doi_pmid_disagreement'
 else 'unambiguous' end as mapping_status,
 coalesce(dm.pmcid,pm.pmcid) as pmcid
 from identifiers i left join dm on i.doi=dm.doi
 left join pm on i.pmid=pm.pmid
 where dm.pmcid is not null or pm.pmcid is not null""")
 statuses=c.execute('select mapping_status,count(*) from matched group by mapping_status').fetchall()
 c.execute("copy(select m.*,inv.metadata_keys from matched m join inv using(pmcid) where mapping_status='unambiguous') to '"+str(eligible)+".tmp' (format parquet,compression zstd)")
 (out/'pmc-source-mapping-statuses.json').write_text(json.dumps(statuses))
 (out/'pmc-source-eligible.parquet.tmp').replace(eligible)
N=c.execute('select count(*) from read_parquet(?)',[str(eligible)]).fetchone()[0]
dev=json.loads((root/'validation/development-exclusions.json').read_text())['ids'];c.execute('create temp table dev as select unnest(?) as id',[dev])
paper_keys_path=root/'validation/development-paper-identities.json'
paper_keys=json.loads(paper_keys_path.read_text())
for table,key in [('dev_dois','dois'),('dev_pmids','pmids'),('dev_pmcids','pmcids')]:
 c.execute('create temp table '+table+' as select unnest(?) as identifier',[paper_keys[key]])
c.execute("create view eligible as select * from read_parquet('"+str(eligible)+"') where id not in (select id from dev) and coalesce(doi,'') not in (select identifier from dev_dois) and coalesce(pmid,'') not in (select identifier from dev_pmids) and pmcid not in (select identifier from dev_pmcids)")
Ndev=c.execute('select count(*) from eligible').fetchone()[0]
cohort=out/'validation-cohort.parquet'
c.execute("copy(select *,sha256('"+seed+"'||chr(10)||cast(id as varchar)) as validation_hash from eligible order by validation_hash limit "+str(cap)+") to '"+str(cohort)+"' (format parquet,compression zstd)")
cur=c.execute('select * from read_parquet(?) order by validation_hash',[str(cohort)]);rows=[dict(zip([d[0] for d in cur.description],r)) for r in cur.fetchall()]
works=[];pmc=[];metadata_parts={}
for r in rows:
 p=partmap[r['source_index']];w=dict(id=r['id'],work_id='https://openalex.org/W'+str(r['id']),ordinal=r['ordinal'],year=r['year'],type=r['type'],is_xpac=r['is_xpac'],title=r['title'],doi=r['doi'],pmid=r['pmid'],pmcid=r['pmcid'],raw_path=p['raw_path'],cache_path=p['cache_path'],source_index=r['source_index'],raw_row_index=r['raw_row_index'],locations=[],baseline_countries=[],stratum='source_precision_cohort',inclusion_probability=min(cap,Ndev)/Ndev,design_weight=Ndev/min(cap,Ndev),validation_hash=r['validation_hash'])
 works.append(w);metadata_parts.setdefault(r['source_index'],[]).append(w)
 pmc.append(dict(id=r['id'],pmcid=r['pmcid'],doi_match=r['doi_pmcid']==r['pmcid'],pmid_match=r['pmid_pmcid']==r['pmcid'],inventory_metadata_keys=r['metadata_keys']))
raw=json.dumps(dict(works=works,source_eligible_N=Ndev,precision_only=True,policy_sha256=hashlib.sha256(policy).hexdigest()),ensure_ascii=False,sort_keys=True,separators=(',',':')).encode();sha=hashlib.sha256(raw).hexdigest()
sample=dict(works=works,sample_sha256=sha,source_eligible_N=Ndev,precision_only=True,policy_sha256=hashlib.sha256(policy).hexdigest())
(out/'validation-cohort-metadata.json').write_text(json.dumps(sample,ensure_ascii=False,indent=2)+'\n')
md=out/'validation-metadata-parts';md.mkdir(exist_ok=True)
for idx,ws in metadata_parts.items():(md/('part-%05d.json'%idx)).write_text(json.dumps(dict(sample_sha256=sha,works=ws),ensure_ascii=False)+'\n')
plan=dict(status='frozen',mode='new_blind_source_eligible_validation_only',sample_sha256=sha,sample_size=len(works),seed=seed,pmc=pmc,pdf=[],pdf_strata=[])
(out/'validation-xml-plan.json').write_text(json.dumps(plan,ensure_ascii=False,indent=2)+'\n')
(out/'work-frame-acceptance.json').write_text(json.dumps(dict(status='source_discovery_and_blind_validation_cohort_ready_not_full_country_predictions',baseline_unknown_N=277418468,source_pmc_oa_eligible_N=N,nondevelopment_eligible_N=Ndev,cohort_count=len(works),cohort_seed=seed,cohort_sha256=sha,cohort_pi=min(cap,Ndev)/Ndev,shared_identifier_manifest_sha256=hashlib.sha256(manifest.read_bytes()).hexdigest(),policy_sha256=hashlib.sha256(policy).hexdigest(),development_paper_keys_sha256=hashlib.sha256(paper_keys_path.read_bytes()).hexdigest(),protocol_r2_sha256=hashlib.sha256((root/'validation/protocol-r2.json').read_bytes()).hexdigest(),elapsed_seconds=time.time()-start,production_applied=False),indent=2)+'\n')
print('PMC source eligible',N,'nondevelopment',Ndev,'cohort',len(works),'seconds',time.time()-start,flush=True)
