"""Two-file illustrative sample. Run via SSH Python stdin; no remote writes."""
import json,pathlib,subprocess,time,datetime
root=pathlib.Path('/mnt/hg02/openalex-snapshot/data/parquet')
m=json.loads((root/'manifest.json').read_text())
fs=next(e['files'] for e in m['entities'] if e['entity']=='works')
dates=sorted({f['url'].split('updated_date=')[1].split('/')[0] for f in fs})
chosen=[]
for date in [dates[0],dates[-1]]:
    candidates=[f for f in fs if '/updated_date='+date+'/' in f['url']]
    f=max(candidates,key=lambda f:f['meta']['record_count'])
    chosen.append({'path':str(root/f['url'].split('/data/parquet/',1)[1]),'manifest_rows':f['meta']['record_count'],'manifest_bytes':f['meta']['content_length']})
paths='['+','.join("'"+f['path']+"'" for f in chosen)+']'
settings="SET threads=2; SET memory_limit='4GB'; SET enable_progress_bar=false; "
def q(sql):
    t=time.monotonic()
    r=subprocess.run(['/home/ider/.local/bin/duckdb','-json','-c',settings+sql],capture_output=True,text=True,timeout=180)
    if r.returncode:raise RuntimeError(r.stderr)
    return {'elapsed_seconds':round(time.monotonic()-t,2),'sql':sql,'rows':json.loads(r.stdout)}
o={'snapshot_date':m['date'],'audit_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'method':'two illustrative files: highest manifest record count in earliest and latest update-date partitions; not representative','files':chosen}
o['coverage']=q("SELECT filename,count(*) AS works,count_if(is_xpac IS FALSE) AS core_works,count_if(primary_location.raw_type IS NOT NULL) AS raw_type_present FROM read_parquet("+paths+",filename=true) GROUP BY filename;")
o['raw_type_groups']=q("SELECT type,primary_location.source.type AS source_type,primary_location.raw_type AS raw_type,count(*) AS works FROM read_parquet("+paths+") WHERE is_xpac IS FALSE GROUP BY type,source_type,raw_type ORDER BY works DESC;")
o['examples']=q("SELECT id,display_name,publication_year,type,primary_location.source.type AS source_type,primary_location.source.display_name AS source_name,primary_location.raw_type AS raw_type,primary_location.version AS version,is_paratext,is_retracted FROM read_parquet("+paths+") WHERE is_xpac IS FALSE AND (type IN ('review','book-chapter','letter','editorial','other') OR (type='article' AND (primary_location.source.type='conference' OR lower(coalesce(primary_location.raw_type,'')) SIMILAR TO '.*(abstract|review|letter|editorial|proceeding).*' OR lower(display_name) SIMILAR TO '.*(book review|meeting abstract|conference abstract).*'))) QUALIFY row_number() OVER(PARTITION BY type,primary_location.source.type,primary_location.raw_type ORDER BY id)<=2;")
print(json.dumps(o,ensure_ascii=False,indent=2))
