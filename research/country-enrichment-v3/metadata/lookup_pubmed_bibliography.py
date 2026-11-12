"""Exact DOI bibliography lookup for independent source review; no country decision."""
import pathlib,json,argparse,duckdb,hashlib
p=argparse.ArgumentParser();p.add_argument('--dois',required=True);p.add_argument('--out',required=True);a=p.parse_args()
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');pool=b/'metadata/pubmed-pool-data';out=pathlib.Path(a.out);out.mkdir(parents=True,exist_ok=True)
d=json.loads(pathlib.Path(a.dois).read_text());dois=d['dois'];assert all(isinstance(x,str) and x for x in dois)
receipts=[json.loads(x.read_text()) for x in sorted(pool.glob('pubmed26n*.parquet.json'))];files=[r['Path'] for r in receipts];assert len(files)==len(set(files))
c=duckdb.connect();c.execute('set threads=24');c.execute("set memory_limit='80GB'");c.read_parquet(files).create_view('source');c.execute('create temp table dois as select distinct unnest(?) doi',[dois]);c.sql('select p.doi,p.pmid,p.pmcid,p.title,p.vernacular,p.years,p.source_file,p.source_sha256,p.record_ordinal from source p join dois d on p.doi=d.doi').write_parquet(str(out/'bibliography.parquet'),compression='zstd')
cur=c.execute('select * from read_parquet(?) order by doi,pmid',[str(out/'bibliography.parquet')]);names=[x[0] for x in cur.description];data=[dict(zip(names,r)) for r in cur.fetchall()];rows=len(data)
(out/'bibliography.jsonl').write_text(''.join(json.dumps(r,ensure_ascii=False)+'\n' for r in data))
complete=(pool/'manifest.json').exists() and json.loads((pool/'manifest.json').read_text()).get('status')=='complete_frozen_baseline_source_preparation'
r=dict(status='bibliographic_source_discovery_only_not_gold',input_sha256=hashlib.sha256(pathlib.Path(a.dois).read_bytes()).hexdigest(),requested_dois=len(set(dois)),source_files=len(files),source_complete=complete,rows=rows,absence_interpretation='not a conflict or proof of nonexistence; remaining sources pending' if not complete else 'no matching DOI in frozen January baseline; not proof of worldwide nonexistence',country_interpreted=False,per_work_api_calls=0)
(out/'manifest.json').write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r))
