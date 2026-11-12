import pathlib,json,time,duckdb
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112')
r=json.load(open(b/'shared/author-identities/part-01300.json'))
o=b/'metadata/author-go-benchmark';c=duckdb.connect();c.execute('set threads=2')
c.execute("set enable_profiling='json'")
c.execute("set profiling_output='"+str(o/'duck-profile.json')+"'")
q='''select t.id,coalesce(list_transform(r.authorships,a->struct_pack(name:=coalesce(a.author.display_name,''),raw_author_name:=coalesce(a.raw_author_name,''),orcid:=coalesce(a.author.orcid,''),raw_orcid:=coalesce(a.raw_orcid,''))),[]) authors from read_parquet($raw,file_row_number=true) r join read_parquet($target) t on r.file_row_number=t.raw_row_index'''
def readbytes():return int(dict(x.split(':') for x in pathlib.Path('/proc/self/io').read_text().splitlines())['rchar'])
io0=readbytes();s=time.time()
c.sql(q,params={'raw':r['raw_path'],'target':r['target']}).write_parquet(str(o/'duck.parquet'))
elapsed=time.time()-s;io1=readbytes();c.close();p=json.load(open(o/'duck-profile.json'))
result=dict(seconds=elapsed,process_syscall_read_bytes=io1-io0,profile_total_bytes_read=p['total_bytes_read'],rows=290)
print(result);(o/'duck-timing.json').write_text(json.dumps(result,indent=2))
