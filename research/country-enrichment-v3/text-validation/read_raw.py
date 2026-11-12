"""Independent raw authorship projection for the frozen text candidate sample."""
import pathlib,json,duckdb,collections,concurrent.futures,time,re,hashlib
B=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/text-validation')
s=json.loads((B/'sample.json').read_text());groups=collections.defaultdict(list)
for r in s['works']:groups[r['position']['raw_path']].append(r)
email=re.compile(r'[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}',re.I)
def run(item):
 path,rows=item;c=duckdb.connect();c.execute('set threads=2');c.execute("set memory_limit='2GB'")
 result=c.execute("""select file_row_number,id,publication_year,type,is_xpac,
 list_transform(authorships,a->struct_pack(
 raw_affiliation_strings:=a.raw_affiliation_strings,
 alternate_raw_affiliations:=list_transform(a.affiliations,f->f.raw_affiliation_string),
 countries:=a.countries))
 from read_parquet(?,file_row_number=true)
 where file_row_number in (select unnest(?))""",[path,[r['position']['raw_row_index'] for r in rows]]).fetchall();c.close()
 lookup={r['position']['raw_row_index']:r for r in rows};out=[]
 for row,id,year,typ,xpac,authors in result:
  r=lookup[row];assert id=='https://openalex.org/W'+str(r['id']) and (year or 0)==r['year'] and typ==r['type'] and bool(xpac)==r['is_xpac']
  assert not any(a['countries'] for a in (authors or []))
  for a in authors or []:
   for k in ['raw_affiliation_strings','alternate_raw_affiliations']:a[k]=[email.sub('[email removed]',x) for x in (a.get(k) or []) if x]
  out.append(dict(id=r['id'],year=r['year'],is_xpac=r['is_xpac'],proposed_countries=r['text_added'],raw_path=path,raw_row_index=row,authors=authors or [],identity_verified=True))
 assert len(out)==len(rows)
 return out
t=time.time();results=[]
with concurrent.futures.ThreadPoolExecutor(max_workers=12) as ex:
 for rs in ex.map(run,groups.items()):results.extend(rs)
assert len(results)==256
(B/'raw-review.json').write_text(json.dumps(dict(status='raw_identity_and_baseline_empty_verified',sample_sha256=hashlib.sha256((B/'sample.json').read_bytes()).hexdigest(),elapsed_seconds=time.time()-t,works=sorted(results,key=lambda r:r['id'])),ensure_ascii=False,indent=2)+'\n')
print('raw_records',len(results),'parts',len(groups),'seconds',round(time.time()-t,2))
