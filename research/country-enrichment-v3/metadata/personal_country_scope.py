"""Gold-blind collective-country quarantine, using the unchanged Go engine.

The original engine outcomes/country sets are never rewritten. Views clear only
affiliation evidence, preserving all names/ORCIDs and therefore identity inputs.
"""
import argparse,copy,hashlib,itertools,json,os,pathlib,subprocess

ENGINE_SHA='9f17ef782ffc894317c04e26403871ce25f41179f01c0ed6eccd56411389bb10'
POLICY_SHA='2480962940e1ad708ca5f71638d40dc590ff0de72a9f634cd857db1751b8901e'
ROLES=['personal_author','collective_author','legacy_article_affiliation_limited']

def sha(path):
 with pathlib.Path(path).open('rb') as f:return hashlib.file_digest(f,'sha256').hexdigest()
def dump(f,r):f.write(json.dumps(r,ensure_ascii=False)+'\n')
def scope(inputs,outcomes,candidates,engine,aliases,append,out):
 out=pathlib.Path(out);out.mkdir(parents=True,exist_ok=True);os.chmod(out,0o700)
 assert sha(engine)==ENGINE_SHA
 append_sha=sha(append);policy=json.loads(pathlib.Path(append).read_text());assert policy['original_policy_sha256']==POLICY_SHA
 work_roles={};record_proofs={};pending={};eligible_records=0
 views=out/'private-role-view-inputs.jsonl'
 def merge(wid,proof):
  wr=work_roles.setdefault(wid,{})
  for country,roles in proof['country_support_roles'].items():wr.setdefault(country,set()).update(roles)
  record_proofs.setdefault(wid,[]).append(proof)
 with pathlib.Path(inputs).open() as fi,pathlib.Path(outcomes).open() as fo,views.open('w') as fv:
  for line,pair in enumerate(itertools.zip_longest(fi,fo),1):
   assert pair[0] is not None and pair[1] is not None,'Input/outcome stream length mismatch'
   i,r=map(json.loads,pair);assert i['route']=='pubmed' and r['route']=='pubmed' and i['original']['id']==r['id']
   if not r['eligible']:continue
   eligible_records+=1;wid=r['id'];record=json.loads(i['evidence']['private_record_json']);raw_authors=record.get('Authors') or [];authors=i['source']['authors'];assert len(raw_authors)==len(authors)
   flags=[bool((a.get('Collective') or '').strip()) for a in raw_authors]
   collective_aff=any(flag and bool(a['affiliations']) for flag,a in zip(flags,authors));legacy=bool(i['source'].get('legacy_affiliation'))
   proof=dict(source_input_line=line,source_file=i['evidence']['source_file'],source_sha256=i['evidence']['source_sha256'],record_ordinal=i['evidence']['record_ordinal'],country_support_roles={})
   countries=set(r['proposed_countries'])
   if not collective_aff and not legacy:
    proof['country_support_roles']={c:['personal_author'] for c in sorted(countries)};merge(wid,proof);continue
   active=['personal_author']+(['collective_author'] if collective_aff else [])+(['legacy_article_affiliation_limited'] if legacy else [])
   pending[line]=dict(id=wid,countries=countries,identity=r['identity'],proof=proof,roles=active,received=set())
   for role in active:
    view=copy.deepcopy(i);view['evidence']=dict(source_input_line=line,role=role)
    for a,is_collective in zip(view['source']['authors'],flags):
     if role=='legacy_article_affiliation_limited' or (role=='personal_author' and is_collective) or (role=='collective_author' and not is_collective):a['affiliations']=[]
    if role!='legacy_article_affiliation_limited':view['source']['legacy_affiliation']=''
    dump(fv,view)
 os.chmod(views,0o600);results=out/'private-role-view-outcomes.jsonl'
 with views.open('rb') as stdin,results.open('wb') as stdout:subprocess.run([str(engine),'--aliases',str(aliases)],stdin=stdin,stdout=stdout,check=True)
 os.chmod(results,0o600)
 with results.open() as f:
  for line in f:
   r=json.loads(line);key=r['evidence']['source_input_line'];role=r['evidence']['role'];p=pending[key]
   assert r['id']==p['id'] and r['identity']==p['identity'],'Role view changed an identity input/decision'
   assert role in p['roles'] and role not in p['received'];p['received'].add(role)
   for c in r['proposed_countries']:p['proof']['country_support_roles'].setdefault(c,[]).append(role)
 for p in pending.values():
  assert p['received']==set(p['roles']) and set(p['proof']['country_support_roles'])==p['countries'],'Role views do not exhaust original frozen country evidence'
  for roles in p['proof']['country_support_roles'].values():roles.sort()
  merge(p['id'],p['proof'])
 personal=out/'pubmed_baseline-validation-candidates.jsonl';quarantine=out/'quarantined-collective-only.jsonl';supports=out/'country-support-roles.jsonl';n=kept=isolated=0
 with pathlib.Path(candidates).open() as fc,personal.open('w') as fp,quarantine.open('w') as fq,supports.open('w') as fs:
  for line in fc:
   r=json.loads(line);wid=r['id'];n+=1;roles=work_roles[wid];assert set(roles)==set(r['proposed_countries'])
   roles={c:sorted(rs) for c,rs in sorted(roles.items())};only=[c for c,rs in roles.items() if rs==['collective_author']]
   r.update(country_support_roles=roles,personal_scope_append_sha256=append_sha,collective_only_quarantine_countries=only)
   dump(fs,dict(id=wid,country_support_roles=roles,accepted_source_records=record_proofs[wid]))
   if only:r['scope_status']='quarantined_whole_work_collective_only_country';dump(fq,r);isolated+=1
   else:r['scope_status']='personal_country_scope_pending_independent_gate';dump(fp,r);kept+=1
 for p in [personal,quarantine,supports]:os.chmod(p,0o600)
 summary=dict(status='frozen_personal_country_scope_pending_independent_gate',original_policy_sha256=POLICY_SHA,engine_sha256=ENGINE_SHA,append_sha256=append_sha,code_sha256=sha(__file__),original_candidate_sha256=sha(candidates),input_records_eligible=eligible_records,records_requiring_role_views=len(pending),original_candidate_works=n,personal_domain_works=kept,whole_work_collective_only_quarantine=isolated,personal_candidate_sha256=sha(personal),quarantine_sha256=sha(quarantine),support_roles_sha256=sha(supports),original_countries_changed=False,original_outcomes_rewritten=False,gold_seen=False,production_applied=False)
 (out/'manifest.json').write_text(json.dumps(summary,indent=2)+'\n');print(json.dumps(summary),flush=True);return summary

if __name__=='__main__':
 p=argparse.ArgumentParser()
 for arg in ['inputs','outcomes','candidates','engine','aliases','append','out']:p.add_argument('--'+arg,required=True)
 a=p.parse_args();scope(a.inputs,a.outcomes,a.candidates,a.engine,a.aliases,a.append,a.out)
