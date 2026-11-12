"""Exact identifier discovery only. Frozen Go policy makes all admission decisions."""
import argparse,duckdb,pathlib,json,hashlib,time,os
a=argparse.ArgumentParser();a.add_argument('--input',required=True);a.add_argument('--out',required=True);a.add_argument('--mode',choices=['sample','full'],required=True);a.add_argument('--allow-partial-pubmed',action='store_true');args=a.parse_args()
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');out=pathlib.Path(args.out);out.mkdir(parents=True,exist_ok=True);os.chmod(out,0o700)
c=duckdb.connect();c.execute('set threads=24');c.execute("set memory_limit='160GB'");c.execute(f"set temp_directory='{out}/temp'")
t=time.time();inp=pathlib.Path(args.input)
if args.mode=='sample':
 c.execute('create temp table work as select w.* from (select unnest(works) w from read_json_auto(?,maximum_object_size=134217728))',[str(inp)])
else:
 m=json.loads(inp.read_text());assert m['status']=='complete_shared_original_identifier_projection' and m['rows']==277418468
 paths=[x['path'] for x in m['parts']];sources={x['source_index']:x for x in m['parts']}
 c.read_parquet(paths).create_view('work_base')
 c.execute('create temp table source_locations as select unnest(?) as s',[[dict(source_index=i,raw_path=x['raw_path'],cache_path=x['cache_path']) for i,x in sources.items()]])
 c.execute('create temp view work as select w.*,l.raw_path,l.cache_path from work_base w join (select s.* from source_locations) l using(source_index)')

cross_dir=b/'metadata/crossref-pool-data'; cm=json.loads((cross_dir/'manifest.json').read_text()); assert cm['status']=='complete_frozen_crossref_source_pool'
cross=[x['Path'] for x in cm['parts']]
c.read_parquet(cross).create_view('cross_source')
cross_q="""select w.id,w.ordinal,w.year,w.type,w.is_xpac,w.source_index,w.raw_row_index,w.raw_path,w.cache_path,w.title,w.doi,w.pmid,
 p.doi as source_doi,p.given,p.family,p.orcid,p.organisation,p.countries as source_countries,p.source_line,p.source_line_sha256,p.seq_num,p.private_assertion_json
 from work w join cross_source p on w.doi=p.doi where w.doi<>''"""
c.sql(cross_q).write_parquet(str(out/'crossref-matches.parquet'),compression='zstd')
print('crossref_source_matches',c.execute('select count(*),count(distinct id) from read_parquet(?)',[str(out/'crossref-matches.parquet')]).fetchone(),flush=True)

pool=b/'metadata/pubmed-pool-data';files=sorted(pool.glob('pubmed26n*.parquet'))
complete=(pool/'manifest.json').exists() and json.loads((pool/'manifest.json').read_text()).get('status')=='complete_frozen_baseline_source_preparation'
if not complete and not args.allow_partial_pubmed:raise RuntimeError('Full declared PubMed baseline pool not complete; do not classify absent source as a negative')
c.read_parquet([str(f) for f in files]).create_view('pubmed_source')
# Two hash joins rather than a quadratic OR join. Dedup the same record matching both IDs.
sel="""select w.id,w.ordinal,w.year,w.type,w.is_xpac,w.source_index,w.raw_row_index,w.raw_path,w.cache_path,w.title,w.doi,w.pmid,
 p.pmid as source_pmid,p.doi as source_doi,p.source_file,p.source_sha256,p.record_ordinal,p.record_sha256,p.private_record_json,p.countries as source_countries,p.title as source_title,p.vernacular,p.years
 from work w join pubmed_source p on MATCH where IDENT_NONEMPTY and len(p.countries)>0"""
one=sel.replace('MATCH','w.pmid=p.pmid').replace('IDENT_NONEMPTY',"w.pmid<>''")
two=sel.replace('MATCH','w.doi=p.doi').replace('IDENT_NONEMPTY',"w.doi<>''")
c.sql('select distinct * from (('+one+') union all ('+two+'))').write_parquet(str(out/'pubmed-matches.parquet'),compression='zstd')
print('pubmed_source_matches',c.execute('select count(*),count(distinct id) from read_parquet(?)',[str(out/'pubmed-matches.parquet')]).fetchone(),flush=True)
target="""select distinct id,ordinal,year,type,is_xpac,source_index,raw_row_index,raw_path,cache_path,title,doi,pmid
 from (select id,ordinal,year,type,is_xpac,source_index,raw_row_index,raw_path,cache_path,title,doi,pmid from read_parquet(?)
 union all select id,ordinal,year,type,is_xpac,source_index,raw_row_index,raw_path,cache_path,title,doi,pmid from read_parquet(?))"""
rel=c.sql(target,params=[str(out/'crossref-matches.parquet'),str(out/'pubmed-matches.parquet')]);rel.write_parquet(str(out/'author-targets.parquet'),compression='zstd')
ids=[x[0] for x in c.execute('select id from read_parquet(?) order by id',[str(out/'author-targets.parquet')]).fetchall()]
(out/'author-requests.json').write_text(json.dumps({'ids':ids,'purpose':'Source-identifier potential matches; no admission until frozen policy author identity evaluation'})+'\n')
summary=dict(status='source_match_discovery_only_not_admitted',mode=args.mode,input=str(inp),input_sha256=hashlib.sha256(inp.read_bytes()).hexdigest(),pubmed_source_complete=complete,pubmed_source_files=len(files),crossref_source_complete=True,author_target_count=len(ids),seconds=time.time()-t,production_applied=False)
(out/'manifest.json').write_text(json.dumps(summary,indent=2)+'\n');print(json.dumps(summary),flush=True)
