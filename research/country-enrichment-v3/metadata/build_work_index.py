"""Dense original-Parquet MAP/column projection for shared UNKNOWN target index.

This is source preparation, not enrichment or holdout-driven policy fitting.
"""
import concurrent.futures,hashlib,json,os,pathlib,time,sys
import duckdb

base=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112')
input_manifest=base/'shared/unknown/manifest.json'
out=pathlib.Path(os.environ.get('METADATA_INDEX_OUTPUT',str(base/'shared/identifiers')));out.mkdir(parents=True,exist_ok=True)
m=json.loads(input_manifest.read_text()); msha=hashlib.sha256(input_manifest.read_bytes()).hexdigest()
q="""select t.id,t.ordinal,t.year,t.type,t.is_xpac,t.source_index,t.raw_row_index,
 r.id as original_id,coalesce(r.doi,map_extract_value(r.ids,'doi'),'') as raw_doi,
 coalesce(map_extract_value(r.ids,'pmid'),'') as raw_pmid,
 coalesce(r.display_name,'') as title,coalesce(r.open_access.is_oa,false) as is_oa,
 r.id='https://openalex.org/W'||t.id::varchar and coalesce(r.publication_year,0)=t.year
 and coalesce(r.type,'')=t.type and coalesce(r.is_xpac,false)=t.is_xpac as raw_identity_pass
 from read_parquet($raw,file_row_number=true) r
 join read_parquet($target) t on r.file_row_number=t.raw_row_index"""
def run(part):
 idx=part['source_index'];p=out/f'part-{idx:05d}.parquet';receipt=out/f'part-{idx:05d}.json'
 if p.exists() and receipt.exists():
  old=json.loads(receipt.read_text())
  if old['input_manifest_sha256']==msha and old['rows']==part['rows'] and old['sha256']==hashlib.sha256(p.read_bytes()).hexdigest():return old
 assert hashlib.sha256(pathlib.Path(part['path']).read_bytes()).hexdigest()==part['sha256']
 c=duckdb.connect();c.execute('set threads=2');c.execute("set memory_limit='6GB'")
 c.execute(f"set temp_directory='{base}/metadata/index-temp-{idx:05d}'")
 start=time.time();temp=p.with_suffix('.parquet.partial')
 rel=c.sql(q,params={'raw':part['raw_path'],'target':part['path']})
 rel.create_view('projected')
 # Bare numeric PMID or its canonical NLM URL only. Preserve originals for audit.
 query="""select * exclude(original_id),
 lower(trim(regexp_replace(raw_doi,'(?i)^(https?://(dx\\.)?doi\\.org/|doi:)',''))) as doi,
 case when regexp_matches(raw_pmid,'^[1-9][0-9]*$') then raw_pmid
 when regexp_matches(raw_pmid,'^https?://pubmed\\.ncbi\\.nlm\\.nih\\.gov/[1-9][0-9]*/?$')
 then regexp_extract(raw_pmid,'([0-9]+)/?$',1) else '' end as pmid
 from projected"""
 c.sql(query).write_parquet(str(temp),compression='zstd')
 rows,bad,core,xpac=c.execute('select count(*),count(*) filter(where raw_identity_pass is distinct from true),count(*) filter(where not is_xpac),count(*) filter(where is_xpac) from read_parquet(?)',[str(temp)]).fetchone()
 assert rows==part['rows'] and bad==0 and core==part['core'] and xpac==part['xpac'],(idx,rows,bad,part)
 c.close();os.replace(temp,p)
 r=dict(source_index=idx,path=str(p),rows=rows,core=core,xpac=xpac,bytes=p.stat().st_size,sha256=hashlib.sha256(p.read_bytes()).hexdigest(),input_manifest_sha256=msha,input_part_sha256=part['sha256'],raw_path=part['raw_path'],cache_path=part['cache_path'],elapsed_seconds=time.time()-start,raw_identity_failures=0)
 tmp=receipt.with_suffix('.json.partial');tmp.write_text(json.dumps(r,indent=2)+'\n');os.replace(tmp,receipt);return r
parts=m['parts'];start=time.time();results=[]
if len(sys.argv)>1 and sys.argv[1]=='benchmark':
 for part in [parts[0],parts[1300],parts[-1]]:
  r=run(part);print(json.dumps(r),flush=True)
else:
 with concurrent.futures.ThreadPoolExecutor(max_workers=int(os.environ.get('METADATA_INDEX_WORKERS','4'))) as ex:
  fs=[ex.submit(run,p) for p in parts]
  for f in concurrent.futures.as_completed(fs):
   results.append(f.result())
   if len(results)%50==0:print(json.dumps(dict(done=len(results),parts=len(parts),rows=sum(x['rows'] for x in results),seconds=time.time()-start)),flush=True)
 assert sum(r['rows'] for r in results)==277418468 and len(results)==2446
 manifest=dict(status='complete_shared_original_identifier_projection',parts=sorted(results,key=lambda r:r['source_index']),rows=sum(r['rows'] for r in results),core=sum(r['core'] for r in results),xpac=sum(r['xpac'] for r in results),input_manifest_sha256=msha,source_mutated=False,metadata_api_calls=0,elapsed_seconds=time.time()-start,workers=int(os.environ.get('METADATA_INDEX_WORKERS','4')),threads_per_worker=2,
 schema='id,ordinal,year,type,is_xpac,source_index,raw_row_index,raw_doi,raw_pmid,title,is_oa,raw_identity_pass,doi,pmid')
 tmp=out/'manifest.json.partial';tmp.write_text(json.dumps(manifest,indent=2)+'\n');os.replace(tmp,out/'manifest.json')
 print('COMPLETE',manifest['rows'],'seconds',time.time()-start,flush=True)
