"""Convert source-verified shared projections into frozen-engine input JSON.

No author normalization, matching, countries or decision rules are added here.
"""
import pathlib,json,hashlib,duckdb,argparse
p=argparse.ArgumentParser();p.add_argument('--sample',required=True);p.add_argument('--out',required=True);a=p.parse_args()
root=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112')
shared=root/'shared/author-identities';manifest=shared/'manifest.json'
sample=json.load(open(a.sample));works={w['id']:w for w in sample['works']}
paths=[str(shared/('part-%05d.parquet'%i)) for i in sorted(set(w['source_index'] for w in works.values()))]
assert all(pathlib.Path(p).exists() for p in paths)
if not manifest.exists():
 # A complete, exact cohort-specific receipt can precede the larger joint
 # metadata/PMC projection's global manifest. Every required part must exist,
 # verify its digest and have zero original-identity failures; no partial sample.
 receipts=[]
 for path in paths:
  pp=pathlib.Path(path);receipt=json.loads(pp.with_suffix('.json').read_text())
  assert receipt['source_identity_failures']==0 and receipt['path']==str(pp)
  assert hashlib.sha256(pp.read_bytes()).hexdigest()==receipt['sha256']
  receipts.append(receipt)
 manifest=pathlib.Path(a.out)/'exact-sample-source-author-manifest.json';manifest.parent.mkdir(parents=True,exist_ok=True)
 manifest.write_text(json.dumps(dict(status='complete_verified_source_parts_for_exact_sample_not_global_index',sample_sha256=sample['sample_sha256'],sample_rows=len(works),parts=receipts),indent=2)+'\n')
else:
 meta=json.loads(manifest.read_text())
c=duckdb.connect();c.execute('set threads=8');c.read_parquet(paths).create_view('a')
c.execute('create temp table ids as select unnest(?)::ubigint id',[list(works)])
cur=c.execute('select a.* from a join ids using(id)');out=pathlib.Path(a.out);(out/'authors').mkdir(parents=True,exist_ok=True)
seen=set()
for row in cur.fetchall():
 r=dict(zip([d[0] for d in cur.description],row));wid=r['id'];w=works[wid]
 assert wid not in seen;seen.add(wid)
 assert r['authors_loaded'] and r['source_id_year_type_xpac_verified']
 for k in ['year','type','is_xpac','source_index','raw_row_index','ordinal']:assert r[k]==w[k],(wid,k)
 r.update(work_id=w['work_id'],raw_path=w['raw_path'],sample_sha256=sample['sample_sha256'],source_author_manifest_sha256=hashlib.sha256(manifest.read_bytes()).hexdigest())
 (out/'authors'/('W'+str(wid)+'.json')).write_text(json.dumps(r,ensure_ascii=False,indent=2)+'\n')
assert seen==set(works),(len(seen),len(works))
(out/'author-export-acceptance.json').write_text(json.dumps(dict(status='all_sample_original_authors_exported_source_identity_verified',sample_rows=len(works),exported=len(seen),source_manifest_sha256=hashlib.sha256(manifest.read_bytes()).hexdigest(),sample_sha256=sample['sample_sha256']),indent=2)+'\n')
print('Original authors exported',len(seen),flush=True)
