"""Read-only audit, run with: Get-Content -Raw ... | ssh ider@192.168.1.229 python3 -

Only projected Parquet columns are read. Does not write on the remote server.
"""
import datetime
import json
import pathlib
import subprocess
import time

ROOT = pathlib.Path('/mnt/hg02/openalex-snapshot/data/parquet')
DUCKDB = '/home/ider/.local/bin/duckdb'
PATH = str(ROOT / 'works/*/*.parquet')
SETTINGS = "SET threads=4; SET memory_limit='8GB'; SET enable_progress_bar=false; "

def query(sql, timeout=600):
    started = time.monotonic()
    r = subprocess.run([DUCKDB, '-json', '-c', SETTINGS + sql],
                       capture_output=True, text=True, timeout=timeout)
    if r.returncode:
        raise RuntimeError(r.stderr)
    return {'elapsed_seconds': round(time.monotonic() - started, 2),
            'sql': sql, 'rows': json.loads(r.stdout)}

manifest = json.loads((ROOT/'manifest.json').read_text())
result = {'audit_utc': datetime.datetime.now(datetime.timezone.utc).isoformat(),
          'snapshot_date': manifest['date'], 'snapshot_path': str(ROOT),
          'scope': 'full works corpus; source breakdown restricted to is_xpac=false',
          'settings': {'threads':4,'memory_limit':'8GB'}}
result['schema'] = query("DESCRIBE SELECT * FROM read_parquet('"+str(ROOT/'works/updated_date=2016-06-24/part_0000.parquet')+"');",30)
result['type_xpac'] = query("SELECT type, is_xpac, count(*) AS works, count_if(is_paratext IS TRUE) AS paratext_true, count_if(is_retracted IS TRUE) AS retracted_true FROM read_parquet('"+PATH+"') GROUP BY type,is_xpac ORDER BY type,is_xpac;")
print(json.dumps({'progress':'type_xpac_complete','elapsed_seconds':result['type_xpac']['elapsed_seconds']},ensure_ascii=False),file=__import__('sys').stderr,flush=True)
result['core_type_source'] = query("SELECT type, primary_location.source.type AS source_type, count(*) AS works, count_if(is_paratext IS TRUE) AS paratext_true, count_if(is_retracted IS TRUE) AS retracted_true FROM read_parquet('"+PATH+"') WHERE is_xpac IS FALSE GROUP BY type,primary_location.source.type ORDER BY type,source_type;")
print(json.dumps({'progress':'core_type_source_complete','elapsed_seconds':result['core_type_source']['elapsed_seconds']},ensure_ascii=False),file=__import__('sys').stderr,flush=True)
# Stratified source/type examples use five selected partitions, not a random sample.
files = next(e['files'] for e in manifest['entities'] if e['entity']=='works')
dates = sorted({f['url'].split('updated_date=')[1].split('/')[0] for f in files})
chosen = []
for index in [0,len(dates)//4,len(dates)//2,3*len(dates)//4,len(dates)-1]:
    candidates = [f for f in files if '/updated_date='+dates[index]+'/' in f['url']]
    chosen.append(str(ROOT/min(candidates,key=lambda f:f['meta']['content_length'])['url'].split('/data/parquet/',1)[1]))
file_list = '['+','.join("'"+p+"'" for p in chosen)+']'
result['example_sampling'] = {'method':'five update-date strata, smallest Parquet file in each; illustrative, not representative','files':chosen}
result['examples'] = query("SELECT id,display_name,type,primary_location.source.type AS source_type,primary_location.source.display_name AS source_name,primary_location.raw_type AS raw_type,primary_location.version AS version,is_paratext,is_retracted,is_xpac FROM read_parquet("+file_list+") WHERE is_xpac IS FALSE AND (type IN ('letter','editorial','book-chapter','other','preprint','erratum','retraction') OR (type='article' AND (primary_location.source.type='conference' OR is_paratext IS TRUE))) QUALIFY row_number() OVER (PARTITION BY type,primary_location.source.type ORDER BY id)<=3;",120)
print(json.dumps(result,ensure_ascii=False,indent=2))
