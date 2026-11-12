import pathlib,json,hashlib,tarfile,sys
root=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');out=root/'fulltext/pdf-probability';out.mkdir(exist_ok=True)
archive=pathlib.Path(sys.argv[1])
with tarfile.open(archive) as t:
 for m in t.getmembers():assert (out/m.name).resolve().is_relative_to(out.resolve()) and not (m.issym() or m.islnk())
 t.extractall(out,filter='data')
planpath=root/'fulltext/pdf-frame-prep/pdf-plan.json';plan=json.loads(planpath.read_text());sha=hashlib.sha256(planpath.read_bytes()).hexdigest()
m=json.loads((out/'batch-manifest.json').read_text());assert m['parent_plan_sha256']==sha
picks={r['id']:r for r in plan['pdf']};works={w['id']:w for w in json.loads((root/'validation/sample-metadata.json').read_text())['works']}
(out/'pdf-results').mkdir(exist_ok=True);parse=[]
for r in m['records']:
 assert r['plan_sha256']==sha and r['id'] in picks
 pick=picks[r['id']];assert pick['pi2']==r['pi2']
 if r['status']=='pdf_reached':
  p=out/'pdf'/('W'+str(r['id'])+'.pdf');assert hashlib.sha256(p.read_bytes()).hexdigest()==r['sha256'];parse.append(pick);continue
 w=works[r['id']]
 o=dict(id=r['id'],work_id=w['work_id'],stratum=w['stratum'],design_weight=w['design_weight'],pi1=w['inclusion_probability'],pi2=pick['pi2'],route='oa_pdf_grobid_header',status='proxy_'+r['status'],source=dict(url=r.get('final_url',r['url']),http_status=r.get('http_status',0),bytes=r.get('bytes',0),sha256=r.get('sha256',''),seconds=r['seconds']),identity_pass=False,candidate_countries=[],source_plan_sha256=sha,extractor_version='country-fulltext-v2-2026-pmc-bool-s3',transport_record=r)
 (out/'pdf-results'/('W'+str(r['id'])+'.json')).write_text(json.dumps(o,ensure_ascii=False))
plan.update(pdf=parse,parent_plan_sha256=sha,status='execution subset only; original frozen IDs/pi2 retained')
(out/'parse-batch-plan.json').write_text(json.dumps(plan,ensure_ascii=False))
(out/'pdf-author-requests.json').write_text(json.dumps(dict(ids=[r['id'] for r in parse])))
print('imported records',len(m['records']),'PDFs to parse',len(parse))
