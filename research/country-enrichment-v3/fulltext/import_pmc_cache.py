"""Verify a native-tar bridge and every object before local Go parsing.

Unavailable objects retain their original operational denominator. A parse-only
execution plan contains reached source versions, with the frozen full input plan
retained separately. There is no retry of blocked/failed remote retrieval here.
"""
import pathlib,json,hashlib,tarfile,argparse,urllib.parse
p=argparse.ArgumentParser();p.add_argument('--archive',required=True);p.add_argument('--archive-sha256',required=True);p.add_argument('--plan',required=True);p.add_argument('--sample',required=True);p.add_argument('--out',required=True);a=p.parse_args()
archive=pathlib.Path(a.archive);out=pathlib.Path(a.out);out.mkdir(parents=True,exist_ok=True)
assert hashlib.sha256(archive.read_bytes()).hexdigest()==a.archive_sha256
planbytes=pathlib.Path(a.plan).read_bytes();plan=json.loads(planbytes);plan_sha=hashlib.sha256(planbytes).hexdigest()
with tarfile.open(archive) as t:
 for m in t.getmembers():
  target=(out/m.name).resolve();assert target.is_relative_to(out.resolve()) and not (m.issym() or m.islnk())
 t.extractall(out,filter='data')
complete=json.loads((out/'transport-complete.json').read_text());assert complete['plan_sha256']==plan_sha and complete['status']=='complete_for_frozen_plan'
expected={}
for w in plan['pmc']:
 for key in w['inventory_metadata_keys']:expected.setdefault(key,set()).add(w['id'])
seen={};verified={};count=0
for f in (out/'transport').glob('*.json'):
 r=json.loads(f.read_text());key=r['key'];assert key in expected and set(r['ids'])==expected[key] and r['plan_sha256']==plan_sha and key not in seen;seen[key]=r
 reached=True
 for name,folder in [('metadata','pmc-metadata'),('xml','xml')]:
  item=r.get(name)
  if not item or item['status']!='reached':reached=False;continue
  filename=item['path'].replace('\\','/').rsplit('/',1)[-1];b=(out/folder/filename).read_bytes()
  assert hashlib.sha256(b).hexdigest()==item['sha256']
  md5=urllib.parse.parse_qs(urllib.parse.urlparse(item['url']).query).get('md5')
  if md5:assert hashlib.md5(b).hexdigest()==md5[0].lower()
  count+=1
 verified[key]=reached
assert set(seen)==set(expected) and len(seen)==complete['tasks']
works={w['id']:w for w in json.loads(pathlib.Path(a.sample).read_text())['works']};execution=[];failed=[]
(out/'xml-results').mkdir(exist_ok=True)
for w in plan['pmc']:
 available=[k for k in w['inventory_metadata_keys'] if verified[k]]
 if available:
  execution.append(dict(w,inventory_metadata_keys=available));continue
 original=works[w['id']];failed.append(w['id'])
 result=dict(id=w['id'],work_id=original['work_id'],stratum=original['stratum'],design_weight=original['design_weight'],pi1=original['inclusion_probability'],pi2=1,route='pmc_jats',status='source_unavailable_for_frozen_inventory_versions',identity_pass=False,candidate_countries=[],source={},source_plan_sha256=plan_sha,extractor_version='country-fulltext-xml-v3-trans-title-uk',error='All fixed inventory source versions unavailable in bounded transport; no replacement, retry, or new source lookup')
 target=out/'xml-results'/('W'+str(w['id'])+'.json');assert not target.exists() or json.loads(target.read_text())==result;target.write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n')
executionplan=dict(plan,pmc=execution,parent_plan_sha256=plan_sha,status='parse-only reached sources; original fixed membership preserved in complete operational outcomes')
(out/'execution-plan.json').write_text(json.dumps(executionplan,ensure_ascii=False,indent=2)+'\n')
(out/'cache-import-acceptance.json').write_text(json.dumps(dict(status='archive_and_all_reached_objects_verified_before_parse',archive_path=str(archive),archive_sha256=a.archive_sha256,original_plan_sha256=plan_sha,plan_works=len(plan['pmc']),metadata_tasks=len(expected),verified_object_references=count,parse_works=len(execution),fully_unavailable_work_ids=failed,operational_denominator_preserved=True,source_availability_only_no_automatic_country_rule_change=True),indent=2)+'\n')
print('PMC bridge verified',len(expected),'source versions, parse',len(execution),'unavailable',len(failed),flush=True)
