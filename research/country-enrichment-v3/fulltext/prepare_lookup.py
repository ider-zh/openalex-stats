"""Vectorized dedup of complete official IDs; no Work country prediction."""
import duckdb,pathlib,json,time,hashlib
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/fulltext/source-index')
c=duckdb.connect();c.execute('set threads=24');c.execute("set memory_limit='96GB'")
start=time.time();records=[]
for key in ['doi','pmid']:
 out=b/('pmc-'+key+'-lookup.parquet')
 q=f"""copy(select {key},min(pmcid) as pmcid,count(distinct pmcid) as pmcid_count
 from read_parquet('{b/'pmc-source-idmap.parquet'}') where {key}!='' group by {key})
 to '{out}' (format parquet,compression zstd)"""
 c.execute(q)
 rows=c.execute(f"select count(*),count(*)filter(where pmcid_count>1) from read_parquet('{out}')").fetchone()
 records.append(dict(kind=key,path=str(out),identifiers=rows[0],ambiguous_identifiers=rows[1],sha256=hashlib.sha256(out.read_bytes()).hexdigest()))
 print(records[-1],flush=True)
(b/'lookup-acceptance.json').write_text(json.dumps(dict(status='ready_source_lookup_only',records=records,elapsed_seconds=time.time()-start,production_applied=False),indent=2)+'\n')
