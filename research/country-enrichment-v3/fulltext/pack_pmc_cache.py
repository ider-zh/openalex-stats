"""Package only a completed, checked transport cache for file bridge to 229."""
import pathlib,json,hashlib,tarfile,sys,subprocess,time
source=pathlib.Path(sys.argv[1]);archive=pathlib.Path(sys.argv[2]);complete=json.loads((source/'transport-complete.json').read_text(encoding='utf-8'))
assert complete['status']=='complete_for_frozen_plan'
if len(sys.argv)>3 and sys.argv[3]=='--native':
 # The transport has already computed hashes/official MD5 and written objects
 # atomically. Native tar avoids Python's repeated NTFS opens; the destination
 # import independently rechecks every digest/MD5 before Go reads the cache.
 start=time.monotonic();subprocess.run(['tar','-czf',str(archive.resolve()),'-C',str(source.resolve()),'pmc-metadata','xml','transport','transport-complete.json','fetch-events.jsonl','transport-runs.jsonl'],check=True)
 print('Native PMC archive ready',complete['tasks'],archive.stat().st_size,hashlib.sha256(archive.read_bytes()).hexdigest(),'seconds',time.monotonic()-start,flush=True);sys.exit(0)
records=[]
for p in (source/'transport').glob('*.json'):
 r=json.loads(p.read_text(encoding='utf-8'));assert r['plan_sha256']==complete['plan_sha256'];records.append(r)
 for key in ['metadata','xml']:
  item=r.get(key)
  if item and item['status']=='reached':
   f=pathlib.Path(item['path']);assert hashlib.sha256(f.read_bytes()).hexdigest()==item['sha256']
assert len(records)==complete['tasks']
with tarfile.open(archive,'w:gz') as tar:
 for d in ['pmc-metadata','xml','transport']:tar.add(source/d,arcname=d)
 for n in ['transport-complete.json','fetch-events.jsonl','transport-runs.jsonl','concurrency3-stop-identity.json','concurrency8-stop-identity.json']:
  if (source/n).exists():tar.add(source/n,arcname=n)
print('PMC archive ready',len(records),archive.stat().st_size,hashlib.sha256(archive.read_bytes()).hexdigest())
