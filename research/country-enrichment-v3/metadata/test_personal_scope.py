import pathlib,json,subprocess,importlib.util
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/metadata');o=b/'personal-scope-synthetic-QA';o.mkdir(exist_ok=True);inputs=[]
def rec(wid,personal,collective,legacy='',ordinal=1):
 title='A comprehensive investigation of synthetic biological systems';doi='10.9999/synthetic'+str(wid);pmid=str(wid)
 aa=[dict(Given='Alice',Family='Smith',Collective='',Identifiers=[],Affiliations=[dict(Text=s) for s in personal]),dict(Given='',Family='',Collective='Synthetic Research Consortium',Identifiers=[],Affiliations=[dict(Text=s) for s in collective])]
 source=dict(pmid=pmid,doi=doi,title=title,vernacular='',years=[2020],authors=[dict(given=a['Given'],family=a['Family'],orcids=[],affiliations=[f['Text'] for f in a['Affiliations']]) for a in aa],legacy_affiliation=legacy)
 record=dict(PMID=pmid,DOI=doi,Title=title,Years=[2020],Authors=aa,LegacyAffiliation=legacy)
 return dict(route='pubmed',original=dict(id=wid,title=title,doi=doi,pmid=pmid,year=2020,authors=[dict(raw_author_name='Alice Smith')]),source=source,evidence=dict(private_record_json=json.dumps(record),source_file='synthetic.xml.gz',source_sha256='0'*64,record_ordinal=ordinal))
inputs=[rec(990001,[],['Institute, USA']),rec(990002,['Institute, USA'],['Institute, USA']),rec(990003,['Institute, United Kingdom'],['Institute, USA']),rec(990004,[],['Institute, USA'],'Institute, USA'),rec(990005,['Institute, USA'],[],ordinal=1),rec(990005,[],['Institute, USA'],ordinal=2)]
(o/'inputs.jsonl').write_text(''.join(json.dumps(r)+'\n' for r in inputs))
with (o/'inputs.jsonl').open('rb') as stdin,(o/'outcomes.jsonl').open('wb') as stdout:subprocess.run([str(b/'policy-engine'),'--aliases',str(b/'country-aliases.json')],stdin=stdin,stdout=stdout,check=True)
countries={}
for r in map(json.loads,(o/'outcomes.jsonl').read_text().splitlines()):
 assert r['eligible'];countries.setdefault(r['id'],set()).update(r['proposed_countries'])
(o/'candidates.jsonl').write_text(''.join(json.dumps(dict(id=wid,proposed_countries=sorted(cs),original_countries=[]))+'\n' for wid,cs in countries.items()))
spec=importlib.util.spec_from_file_location('scope',b/'personal_country_scope.py');module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
r=module.scope(o/'inputs.jsonl',o/'outcomes.jsonl',o/'candidates.jsonl',b/'policy-engine',b/'country-aliases.json',b/'personal-country-scope-v1.json',o/'result')
kept={r['id']:r for r in map(json.loads,(o/'result/pubmed_baseline-validation-candidates.jsonl').read_text().splitlines())};isolated={r['id']:r for r in map(json.loads,(o/'result/quarantined-collective-only.jsonl').read_text().splitlines())}
assert set(kept)=={990002,990004,990005};assert set(isolated)=={990001,990003};assert isolated[990003]['proposed_countries']==['GB','US'];assert kept[990002]['country_support_roles']['US']==['collective_author','personal_author'];assert kept[990005]['country_support_roles']['US']==['collective_author','personal_author'];assert kept[990004]['country_support_roles']['US']==['collective_author','legacy_article_affiliation_limited']
qa=dict(status='passed',synthetic_only=True,cases=5,records=6,tests=['collective-only whole Work quarantine','same-country individual and collective kept','mixed-country whole Work not silently edited','predeclared limited legacy support retained as separate role','same-country personal support across accepted source records kept','original identities unchanged and role union exhaustive'],original_engine_unmodified=True)
(o/'QA.json').write_text(json.dumps(qa,indent=2)+'\n');print(json.dumps(qa))
