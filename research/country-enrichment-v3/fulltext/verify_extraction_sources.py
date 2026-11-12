"""Check every raw extraction still refers to the exact retained source bytes."""
import pathlib,json,hashlib,argparse
p=argparse.ArgumentParser();p.add_argument('--results',required=True);p.add_argument('--out',required=True);a=p.parse_args()
records=[];sources=0
for f in pathlib.Path(a.results).glob('W*.json'):
 r=json.loads(f.read_text());rec=dict(id=r['id'],outcome_sha256=hashlib.sha256(f.read_bytes()).hexdigest())
 for k in ['source','metadata']:
  x=r.get(k)
  if x and x.get('sha256') and x.get('path'):
   source=pathlib.Path(x['path']);actual=hashlib.sha256(source.read_bytes()).hexdigest();assert actual==x['sha256'],(r['id'],k,'Source evidence changed!')
   rec[k+'_sha256']=actual;sources+=1
 records.append(rec)
assert records
pathlib.Path(a.out).write_text(json.dumps(dict(status='all_retained_source_bytes_match_raw_extraction_digest',outcomes=len(records),verified_source_objects=sources,records=records),indent=2)+'\n')
print('Source evidence digest audit passed',len(records),'outcomes',sources,'references')
