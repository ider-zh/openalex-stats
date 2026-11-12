"""One shared, column-pruned original-author identity read for metadata + PMC."""
import argparse,pathlib,json,os,hashlib,time,concurrent.futures
import duckdb
a=argparse.ArgumentParser();a.add_argument('--targets',nargs='+',required=True);a.add_argument('--benchmark',type=int);a.add_argument('--out');a.add_argument('--reuse-manifest');args=a.parse_args()
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');out=pathlib.Path(args.out) if args.out else b/'shared/author-identities';out.mkdir(parents=True,exist_ok=True)
if args.reuse_manifest:
 oldmanifest=pathlib.Path(args.reuse_manifest);old=json.loads(oldmanifest.read_text());assert old['status']=='complete_shared_original_author_identities'
 assert oldmanifest.parent.resolve()!=out.resolve(),'Reuse requires a new immutable output version'
 for r in old['parts']:
  dest=out/f"part-{r['source_index']:05d}.parquet";receipt=dest.with_suffix('.json')
  if not dest.exists() and not receipt.exists():
   assert hashlib.sha256(pathlib.Path(r['path']).read_bytes()).hexdigest()==r['sha256']
   os.link(r['path'],dest);receipt.write_text(json.dumps(r,indent=2)+'\n')
source=json.loads((b/'shared/identifiers/manifest.json').read_text());assert source['rows']==277418468
by_index={p['source_index']:p for p in source['parts']};c=duckdb.connect();c.execute('set threads=8');c.execute("set memory_limit='32GB'")
c.read_parquet(args.targets,union_by_name=True).create_view('combined')
cols='id,ordinal,year,type,is_xpac,source_index,raw_row_index'
c.execute('create temp table targets as select distinct '+cols+' from combined')
n,u=c.execute('select count(*),count(distinct id) from targets').fetchone();assert n==u,('conflicting target identities',n,u)
target_dir=out/'targets';target_dir.mkdir(exist_ok=True)
parts=[]
for idx,rows in c.execute('select source_index,count(*) from targets group by source_index order by source_index').fetchall():
 p=target_dir/f'part-{idx:05d}.parquet';c.sql(f'select * from targets where source_index={idx} order by raw_row_index').write_parquet(str(p),compression='zstd');parts.append(dict(source_index=idx,rows=rows,target=str(p),target_sha256=hashlib.sha256(p.read_bytes()).hexdigest(),**{k:by_index[idx][k] for k in ['raw_path','cache_path']}))
c.close();target_manifest=dict(status='shared_original_author_target_union',input_targets=args.targets,rows=n,parts=parts)
(out/'target-manifest.json').write_text(json.dumps(target_manifest,indent=2)+'\n')
q="""select t.*,coalesce(list_transform(r.authorships,a->struct_pack(
 name:=coalesce(a.author.display_name,''),raw_author_name:=coalesce(a.raw_author_name,''),
 orcid:=coalesce(a.author.orcid,''),raw_orcid:=coalesce(a.raw_orcid,''))),[]) as authors,
 true as authors_loaded,
 r.id='https://openalex.org/W'||t.id::varchar and coalesce(r.publication_year,0)=t.year
 and coalesce(r.type,'')=t.type and coalesce(r.is_xpac,false)=t.is_xpac as source_id_year_type_xpac_verified
 from read_parquet($raw,file_row_number=true) r join read_parquet($target) t
 on r.file_row_number=t.raw_row_index"""
def run(p):
 idx=p['source_index'];path=out/f'part-{idx:05d}.parquet';receipt=out/f'part-{idx:05d}.json'
 if path.exists() and receipt.exists():
  old=json.loads(receipt.read_text())
  if old['target_sha256']==p['target_sha256'] and old['sha256']==hashlib.sha256(path.read_bytes()).hexdigest():return old
 # An expanded source target set reuses all original rows already decoded.
 oldpath=None
 if path.exists():
  oldpath=out/f'part-{idx:05d}-previous.parquet';os.replace(path,oldpath)
 c=duckdb.connect();c.execute('set threads=2');c.execute("set memory_limit='6GB'");t=time.time()
 if oldpath is not None:
  missing=out/f'part-{idx:05d}-missing.parquet'
  c.sql('select t.* from read_parquet(?) t anti join read_parquet(?) old using(id)',params=[p['target'],str(oldpath)]).write_parquet(str(missing),compression='zstd');target=missing
 else:target=p['target']
 new=out/f'part-{idx:05d}.parquet.partial'
 if oldpath is None:c.sql(q,params={'raw':p['raw_path'],'target':str(target)}).write_parquet(str(new),compression='zstd')
 else:
  c.sql(q,params={'raw':p['raw_path'],'target':str(target)}).create_view('new_rows');c.sql('select * from read_parquet(?) union all select * from new_rows',params=[str(oldpath)]).write_parquet(str(new),compression='zstd')
 rows,bad=c.execute('select count(*),count(*) filter(where source_id_year_type_xpac_verified is distinct from true) from read_parquet(?)',[str(new)]).fetchone();assert rows==p['rows'] and bad==0,(idx,rows,p['rows'],bad)
 c.close();os.replace(new,path);os.chmod(path,0o600);r=dict(p,path=str(path),sha256=hashlib.sha256(path.read_bytes()).hexdigest(),bytes=path.stat().st_size,rows=rows,seconds=time.time()-t,source_identity_failures=bad)
 tmp=receipt.with_suffix('.json.partial');tmp.write_text(json.dumps(r,indent=2)+'\n');os.replace(tmp,receipt);return r
if args.benchmark is not None:
 r=run(next(p for p in parts if p['source_index']==args.benchmark));print(json.dumps(r),flush=True)
else:
 start=time.time();results=[]
 with concurrent.futures.ThreadPoolExecutor(max_workers=4) as ex:
  fs=[ex.submit(run,p) for p in parts]
  for f in concurrent.futures.as_completed(fs):
   results.append(f.result())
   if len(results)%50==0:print(json.dumps(dict(done=len(results),parts=len(parts),rows=sum(r['rows'] for r in results),seconds=time.time()-start)),flush=True)
 assert sum(r['rows'] for r in results)==n
 m=dict(status='complete_shared_original_author_identities',rows=n,parts=sorted(results,key=lambda r:r['source_index']),source_mutated=False,seconds=time.time()-start,target_manifest_sha256=hashlib.sha256((out/'target-manifest.json').read_bytes()).hexdigest(),schema='id,ordinal,year,type,is_xpac,source_index,raw_row_index,authors[{name,raw_author_name,orcid,raw_orcid}],authors_loaded,source_id_year_type_xpac_verified')
 tmp=out/'manifest.json.partial';tmp.write_text(json.dumps(m,indent=2)+'\n');os.replace(tmp,out/'manifest.json');print('COMPLETE',n,'seconds',time.time()-start,flush=True)
