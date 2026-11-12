import pathlib,json,tarfile,hashlib,sys
root=pathlib.Path('bin/fulltext-v3-pdf-probability');planpath=pathlib.Path('research/country-enrichment-v3/fulltext/probability-pdf-plan.json')
plan=json.loads(planpath.read_text());sha=hashlib.sha256(planpath.read_bytes()).hexdigest();picks={r['id']:r for r in plan['pdf']}
name=sys.argv[1];batch=root/name;batch.mkdir(exist_ok=True)
records=[]
for p in (root/'transport').glob('W*.json'):
 try:r=json.loads(p.read_text())
 except json.JSONDecodeError:continue
 assert r['plan_sha256']==sha and r['id'] in picks and r['pi2']==picks[r['id']]['pi2']
 records.append(r)
with tarfile.open(batch/'bundle.tgz','w:gz') as tar:
 for r in records:
  if r['status']=='pdf_reached':
   p=root/'pdf'/('W'+str(r['id'])+'.pdf');assert hashlib.sha256(p.read_bytes()).hexdigest()==r['sha256'];tar.add(p,arcname='pdf/'+p.name)
 manifest=dict(parent_plan_sha256=sha,records=records,execution_subset_only=True)
 (batch/'manifest.json').write_text(json.dumps(manifest,ensure_ascii=False))
 tar.add(batch/'manifest.json',arcname='batch-manifest.json')
print('batch',name,'records',len(records),'PDF',sum(r['status']=='pdf_reached' for r in records),'bytes',(batch/'bundle.tgz').stat().st_size)
