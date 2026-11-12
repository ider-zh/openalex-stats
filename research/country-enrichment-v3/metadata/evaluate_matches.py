"""Mechanical source payload adapter to immutable Go policy engine.

Outputs validation candidates, not a published/gated country patch.
"""
import argparse,duckdb,pathlib,json,subprocess,hashlib,collections,os
a=argparse.ArgumentParser();a.add_argument('--matches',required=True);a.add_argument('--authors',required=True);a.add_argument('--out',required=True);a.add_argument('--routes',nargs='+',choices=['crossref','pubmed_baseline'],default=['crossref','pubmed_baseline']);args=a.parse_args()
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');md=b/'metadata';matches=pathlib.Path(args.matches);out=pathlib.Path(args.out);out.mkdir(parents=True,exist_ok=True);os.chmod(out,0o700)
engine=md/'policy-engine';assert hashlib.sha256(engine.read_bytes()).hexdigest()=='9f17ef782ffc894317c04e26403871ce25f41179f01c0ed6eccd56411389bb10'
policy_sha=hashlib.sha256((md/'policy-v2.json').read_bytes()).hexdigest();assert policy_sha=='2480962940e1ad708ca5f71638d40dc590ff0de72a9f634cd857db1751b8901e'
auth_path=pathlib.Path(args.authors)
if auth_path.name=='manifest.json':
 manifest=json.loads(auth_path.read_text());assert manifest['status']=='complete_shared_original_author_identities'
 paths=[p['path'] for p in manifest['parts']]
 ac=duckdb.connect();ac.execute('set threads=8')
 cur=ac.execute('select * from read_parquet(?)',[paths]);names=[x[0] for x in cur.description]
 authors={r['id']:r for r in (dict(zip(names,row)) for row in cur.fetchall())};ac.close()
 assert len(authors)==manifest['rows']
elif auth_path.is_dir():
 authors={int(p.stem.removeprefix('W')):json.loads(p.read_text()) for p in auth_path.glob('W*.json')}
else:
 d=json.loads(auth_path.read_text());authors={r['id']:r for r in (d.get('works') or d.get('rows'))}
c=duckdb.connect();c.execute('set threads=8')
def load(path):
 cur=c.execute('select * from read_parquet(?)',[str(path)]);names=[x[0] for x in cur.description];return [dict(zip(names,r)) for r in cur.fetchall()]
allinputs=[];metadata={};route_rows=collections.Counter()
for route,file in [('crossref','crossref-matches.parquet'),('pubmed_baseline','pubmed-matches.parquet')]:
 if route not in args.routes:continue
 for r in load(matches/file):
  wid=r['id'];assert wid in authors,('original authors not yet read',wid)
  packet=authors[wid];assert packet.get('authors_loaded') and packet.get('source_id_year_type_xpac_verified')
  assert packet['year']==r['year'] and packet['type']==r['type'] and packet['is_xpac']==r['is_xpac']
  original=dict(id=wid,title=r['title'],doi=r['doi'] or '',pmid=r['pmid'] or '',year=r['year'],authors=packet['authors'])
  metadata[route,wid]={k:r[k] for k in ['id','ordinal','year','type','is_xpac','source_index','raw_row_index','raw_path','cache_path','title','doi','pmid']}
  if route=='crossref':
   src=dict(doi=r['source_doi'],relationship='contributor-affiliation',role='author',author=dict(given=r['given'],family=r['family'],orcids=[r['orcid']],affiliations=[r['organisation']]))
   evidence=dict(source_file='/mnt/nvme0/public/openalex-stats/analysis-country-pilot-20261112/metadata/dumps/country-pilot-crossref-organisation-assertions-Mar-2025.jsonl.gz',source_sha256='be7c9fb2c5bfdee6eb052eadace6c18efeb860abededc73fde5962ff5ecae992',source_line=r['source_line'],source_line_sha256=r['source_line_sha256'],source_assertion_json=r['private_assertion_json'])
  else:
   record=json.loads(r['private_record_json']);sourceauthors=[]
   for aa in record.get('Authors') or []:
    sourceauthors.append(dict(given=aa.get('Given',''),family=aa.get('Family',''),orcids=[x['Value'] for x in aa.get('Identifiers') or [] if x.get('Source','').lower()=='orcid'],affiliations=[x['Text'] for x in aa.get('Affiliations') or []]))
   src=dict(pmid=record['PMID'],doi=record.get('DOI',''),title=record['Title'],vernacular=record.get('Vernacular',''),years=record.get('Years') or [],authors=sourceauthors,legacy_affiliation=record.get('LegacyAffiliation',''))
   evidence=dict(source_file=r['source_file'],source_sha256=r['source_sha256'],record_ordinal=r['record_ordinal'],record_sha256=r['record_sha256'],private_record_json=r['private_record_json'])
  allinputs.append(dict(route='pubmed' if route=='pubmed_baseline' else route,original=original,source=src,evidence=evidence));route_rows[route]+=1
inp=out/'private-policy-inputs.jsonl';inp.write_text(''.join(json.dumps(x,ensure_ascii=False)+'\n' for x in allinputs));os.chmod(inp,0o600)
raw=out/'private-policy-record-outcomes.jsonl'
with inp.open('rb') as stdin,raw.open('wb') as stdout:subprocess.run([str(engine),'--aliases',str(md/'country-aliases.json')],stdin=stdin,stdout=stdout,check=True)
os.chmod(raw,0o600)
groups=collections.defaultdict(list)
for line in raw.read_text().splitlines():
 r=json.loads(line);route='pubmed_baseline' if r['route']=='pubmed' else r['route'];groups[route,r['id']].append(r)
counts={};outputs={}
for route in args.routes:
 candidates=[];deferred=[]
 for (rr,wid),records in groups.items():
  if rr!=route:continue
  accepted=[r for r in records if r['eligible']];keyset=set()
  if route=='pubmed_baseline':
   for r in accepted:
    src=json.loads(r['evidence']['private_record_json']);keyset.add((src['PMID'],src.get('DOI',''),src['Title']))
  countries=sorted(set(x for r in accepted for x in r['proposed_countries']))
  ambiguous_identity=len(keyset)>1
  m=metadata[route,wid];rec=dict(m,source_route=route,original_countries=[],proposed_countries=countries if not ambiguous_identity else [],rule_sha256=policy_sha,country_set_complete='false' if any(r['country_set_complete']=='false' for r in records) else 'unknown',gate_status='not_yet_validated',evidence_path=str(raw),evidence_records=accepted if countries else records,original_authors=authors[wid]['authors'],identity_conflict_multiple_source_records=ambiguous_identity)
  if countries and not ambiguous_identity:candidates.append(rec)
  else:deferred.append(rec)
 candidates.sort(key=lambda r:r['id']);deferred.sort(key=lambda r:r['id'])
 f=out/(route+'-validation-candidates.jsonl');f.write_text(''.join(json.dumps(r,ensure_ascii=False)+'\n' for r in candidates));os.chmod(f,0o600)
 f=out/(route+'-deferred.jsonl');f.write_text(''.join(json.dumps(r,ensure_ascii=False)+'\n' for r in deferred));os.chmod(f,0o600)
 counts[route]=dict(source_records=route_rows[route],works=len(candidates)+len(deferred),automatic_candidates=len(candidates),deferred=len(deferred));outputs[route]=str(out/(route+'-validation-candidates.jsonl'))
manifest=dict(status='frozen_policy_candidates_pending_independent_gate',counts=counts,outputs=outputs,policy_sha256=policy_sha,engine_sha256=hashlib.sha256(engine.read_bytes()).hexdigest(),matches_manifest_sha256=hashlib.sha256((matches/'manifest.json').read_bytes()).hexdigest(),production_applied=False,gate_passed=False)
(out/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n');print(json.dumps(manifest),flush=True)
