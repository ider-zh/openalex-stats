"""Package frozen automatic positives and complete operational denominator.
This does not grant the route gate or apply countries to production.
"""
import argparse,json,pathlib,hashlib
p=argparse.ArgumentParser();p.add_argument('--sample',required=True);p.add_argument('--results',required=True);p.add_argument('--authors',required=True);p.add_argument('--out',required=True);p.add_argument('--complete-mapped-frame',action='store_true');a=p.parse_args()
sample=json.load(open(a.sample));works={r['id']:r for r in sample['works']};out=pathlib.Path(a.out);out.mkdir(parents=True,exist_ok=True)
rows=[];candidates=[]
for f in sorted(pathlib.Path(a.results).glob('W*.json')):
 r=json.load(open(f));w=works[r['id']]
 r.update(original_work=w,original_countries=[],source_route=r['route'],rule_sha256=r['policy_sha256'],country_set_complete='unknown',proposed_countries=r.get('candidate_countries',[]),gate_sha=None,evidence_path=str(f.resolve()))
 author=pathlib.Path(a.authors)/('W'+str(r['id'])+'.json')
 if author.exists():r['original_authors_path']=str(author)
 for k in ['year','type','is_xpac','source_index','raw_row_index','ordinal','development_member']:
  if k in w:r[k]=w[k]
 rows.append(r)
 if r['status']=='automatic_country_candidate' and r['identity_pass'] and r['proposed_countries']:candidates.append(r)
if a.complete_mapped_frame:
 seen={r['id'] for r in rows}
 for wid,w in works.items():
  if wid not in seen:rows.append(dict(id=wid,work_id=w['work_id'],route='pmc_jats',source_route='pmc_jats',status='not_eligible_for_frozen_identifier_inventory_strategy',identity_pass=False,proposed_countries=[],original_countries=[],original_work=w,source_scope='Complete official identifier-map lookup; not a claim that no full text exists anywhere',production_applied=False))
for name,data in [('all-outcomes.jsonl',rows),('automatic-candidates.jsonl',candidates)]:
 with open(out/name,'w') as stream:
  for r in data:stream.write(json.dumps(r,ensure_ascii=False,separators=(',',':'))+'\n')
(out/'export-acceptance.json').write_text(json.dumps(dict(status='candidate_stream_for_independent_gate_only',total_operational_outcomes=len(rows),automatic_positive_candidates=len(candidates),sample_sha256=sample.get('sample_sha256'),candidate_stream_sha256=hashlib.sha256((out/'automatic-candidates.jsonl').read_bytes()).hexdigest(),production_applied=False,gate_granted=False),indent=2)+'\n')
print('operational outcomes',len(rows),'automatic candidates',len(candidates))
