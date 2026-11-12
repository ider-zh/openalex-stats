"""Read-only postal-routing marker audit; no automatic country/defer decision."""
import pathlib,json,re,os,collections
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');md=b/'metadata';postal=re.compile(r'mersin[\s,.-]*10\b',re.I);campus=re.compile(r'cyprus|nicosia|lefko[sş]a|lefkosia|famagusta|gazi\s*m[aăağ]g?usa|kyrenia|girne|lefke|near\s+east\s+university|eastern\s+mediterranean\s+university',re.I);email=re.compile(r'[A-Z0-9._%+\-]+@[A-Z0-9.\-]+\.[A-Z]{2,}',re.I);rows=[]
for route,path in [('crossref',md/'full-crossref-policy/crossref-validation-candidates.jsonl'),('pubmed_baseline',md/'full-pubmed-policy/personal-scope/pubmed_baseline-validation-candidates.jsonl')]:
 for line in path.open():
  r=json.loads(line)
  if 'TR' not in r['proposed_countries']:continue
  matches=[]
  for er in r['evidence_records']:
   e=er['evidence']
   if route=='crossref':
    s=json.loads(e['source_assertion_json']);affs=[dict(text=s.get('organisation-string',''),source_file=e['source_file'],source_line=e['source_line'])]
   else:
    s=json.loads(e['private_record_json']);affs=[dict(text=f['Text'],collective_name=a.get('Collective',''),source_file=e['source_file'],record_ordinal=e['record_ordinal']) for a in s.get('Authors') or [] for f in a.get('Affiliations') or []]
    if s.get('LegacyAffiliation'):affs.append(dict(text=s['LegacyAffiliation'],legacy_limited=True,source_file=e['source_file'],record_ordinal=e['record_ordinal']))
   for a in affs:
    if postal.search(a['text']) and campus.search(a['text']):a['text']=email.sub('[email removed]',a['text']);matches.append(a)
  if matches:rows.append(dict(id=r['id'],route=route,source_evidence=matches,marker_only_no_country_judgement=True,candidate_and_prediction_unchanged=True))
p=md/'postal-marker-cross-source-readonly.json';p.write_text(json.dumps(dict(status='read_only_marker_hits_for_root_review_not_an_automatic_defer',rule='Mersin10 postal marker and explicit northern-Cyprus campus/city marker in same affiliation; proposed TR only',rows=rows,unique_work_count=len({r['id'] for r in rows}),route_rows=dict(collections.Counter(r['route'] for r in rows))),ensure_ascii=False,indent=2)+'\n');os.chmod(p,0o600);print('marker work IDs',len({r['id'] for r in rows}),'route rows',dict(collections.Counter(r['route'] for r in rows)))
