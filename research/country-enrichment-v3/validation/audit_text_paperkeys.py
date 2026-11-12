import duckdb,json,pathlib,hashlib,time,math
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');v=b/'validation'; sample=json.loads((b/'text-validation/sample.json').read_text());dev=json.loads((v/'development-paper-identities.json').read_text());manifest=b/'shared/identifiers/manifest.json'
assert hashlib.sha256(manifest.read_bytes()).hexdigest()=='eb0109f0f560a1d32b4ccd3eaecb2e8fb166701a90924c9f322332f351fb5fb0'
c=duckdb.connect();c.execute('set threads=4');c.execute("set memory_limit='4GB'");c.execute('create temp table wanted(id UBIGINT)');c.executemany('insert into wanted values (?)',[(w['id'],) for w in sample['works']]);start=time.monotonic()
cur=c.execute('select p.* from read_parquet(?) p semi join wanted using(id)',[str(b/'shared/identifiers/part-*.parquet')]);cols=[x[0] for x in cur.description];rows=[dict(zip(cols,x)) for x in cur.fetchall()];assert len(rows)==256 and len({x['id'] for x in rows})==256
orig={w['id']:w for w in sample['works']};checks=['id','ordinal','year','type','is_xpac','source_index','raw_row_index']
for r in rows:
 p=orig[r['id']]['position']
 for k in checks:assert r[k]==p[k],(r['id'],k,r[k],p[k])
 assert r['raw_identity_pass'] is True
D=set(dev['dois']);P=set(dev['pmids']);overlap=[];missing=[]
for r in rows:
 keys=[]
 if r['doi'] and r['doi'].lower() in D:keys.append('doi:'+r['doi'].lower())
 if r['pmid'] and str(r['pmid']) in P:keys.append('pmid:'+str(r['pmid']))
 if keys:overlap.append({'id':r['id'],'keys':keys})
 if not r['doi'] and not r['pmid']:missing.append(r['id'])
lookup={'status':'completed_shared_index_identity_verified','shared_manifest_sha256':hashlib.sha256(manifest.read_bytes()).hexdigest(),'sample_sha256':hashlib.sha256((b/'text-validation/sample.json').read_bytes()).hexdigest(),'works':sorted(rows,key=lambda r:r['id'])};o=v/'text-256-paper-identifiers.json';o.write_text(json.dumps(lookup,ensure_ascii=False,indent=2)+'\n')
review=json.loads((b/'text-validation/independent-reviews.json').read_text());leaked={x['id'] for x in overlap};novel=[x for x in review['reviews'] if x['id'] not in leaked];n=len(novel);s=sum(x['identity_status']=='verified' and x['all_predicted_countries_source_supported'] is True for x in novel);z=1.959963984540054;ph=s/n;den=1+z*z/n;center=(ph+z*z/(2*n))/den;half=z*math.sqrt(ph*(1-ph)/n+z*z/(4*n*n))/den
result={'status':'known_paper_key_exposure_audit_completed','n_original':256,'n_identified_papers':256-len(missing),'n_without_doi_or_pmid':len(missing),'known_exposed_paper_count':len(overlap),'known_exposed_papers':overlap,'missing_keys_ids':missing,'novel_with_respect_to_available_keys_n':n,'novel_successes':s,'novel_precision':ph,'novel_wilson_lower':center-half,'novel_wilson_upper':center+half,'numeric_gate_unchanged_pass':n>=128 and ph>=.99 and center-half>=.97,'no_resampling_or_replacement':True,'all_256_physical_identity_fields_matched':True,'development_paper_keys_sha256':hashlib.sha256((v/'development-paper-identities.json').read_bytes()).hexdigest(),'lookup_sha256':hashlib.sha256(o.read_bytes()).hexdigest(),'limitation':'Available DOI/PMID keys detect known development-paper duplicates; identifiers missing from original records or unavailable prior QA source identities cannot be proven absent. PMCID is not in this full original index. Original raw-source precision denominator remains 256.','elapsed_seconds':time.monotonic()-start}
(v/'text-256-paper-leakage-audit.json').write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n');print(json.dumps({k:val for k,val in result.items() if k not in ['missing_keys_ids']},ensure_ascii=False))
