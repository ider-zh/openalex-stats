"""Read-only bundle transport/rank/baseline audit and bounded mirror packaging.

Run on 229; this verifies already-computed bundles and never edits their files.
"""
import hashlib
import argparse
import json
import math
import struct
import tarfile
import time
from pathlib import Path

import duckdb

parser=argparse.ArgumentParser()
parser.add_argument('--version',choices=['v1','v2'],default='v1')
parser.add_argument('--bundle',choices=['original-core','original-all','enriched-core','enriched-all'])
args=parser.parse_args()
BASE = Path('/mnt/nvme0/public/openalex-stats/complexity-profiles-'+args.version+'-20261113')
OUT = Path('/mnt/nvme0/public/openalex-stats/analysis-longtail-profiles-v1-20261113/package-audit-'+args.version)
OUT.mkdir(exist_ok=args.bundle is not None)
REPORT_NAME='compute-audit'+('-v2'if args.version=='v2'else'')+('-'+args.bundle if args.bundle else'')+'.json'
DEFAULT = 'p1000-c1000-x5-d3-v1'
q = duckdb.connect()
q.execute('set threads=8')

def sha(path):
    h=hashlib.sha256()
    with path.open('rb') as stream:
        for b in iter(lambda:stream.read(8*1024*1024),b''):h.update(b)
    return h.hexdigest()

def read(path):
    return json.loads(path.read_text(),parse_int=float)

def exact(a,b):
    if isinstance(a,(int,float)) and isinstance(b,(int,float)):
        return struct.pack('<d',a)==struct.pack('<d',b)
    if type(a)!=type(b):return False
    if isinstance(a,list):return len(a)==len(b) and all(exact(x,y) for x,y in zip(a,b))
    if isinstance(a,dict):return a.keys()==b.keys() and all(exact(a[k],b[k]) for k in a)
    return a==b

def modelkey(m):return m['year'],m['scheme'],m['level'],m['metric']

report=dict(status='auditing',default_profile_id=DEFAULT,bundles=[])
for name in ['original-core','original-all','enriched-core','enriched-all']:
    if args.bundle and name!=args.bundle:continue
    started=time.monotonic();root=BASE/name;m=json.load(open(root/'manifest.json'))
    assert len(m['profiles'])==19 and len({p['profile']['id'] for p in m['profiles']})==19
    assert m['default_profile_id']==DEFAULT
    if args.version=='v2':assert m['direction_policy']=='baseline-pooled-common-year-sign-v1'
    artifacts={}
    for profile in m['profiles']:
        entries=[*profile['models'].values(),*profile['ranks'].values(),profile['diagnostics'],profile['baseline_comparisons']]
        if args.version=='v2':
            entries.append(profile['alignment'])
            assert profile['direction_policy']==m['direction_policy']
        for downloads in profile['downloads'].values():entries.extend(downloads.values())
        for a in entries:
            p=root/a['path'];assert p.resolve().is_relative_to(root)
            if a['path'] in artifacts:assert artifacts[a['path']]==a;continue
            assert p.is_file() and p.stat().st_size==a['bytes'] and sha(p)==a['sha256'],p
            artifacts[a['path']]=a
        for method in ['spectral-v1','java-reflections-v1']:
            models=json.load(open(root/profile['models'][method]['path']))
            ranks=json.load(open(root/profile['ranks'][method]['path']))
            assert len(models)==len(ranks)==216
            for model,rank in zip(models,ranks):
                assert modelkey(model)==modelkey(rank)
                r=model['result'];assert r['rca'] is None and r['advantage'] is None and r['components'] is None
                for side,scores,active in [('country','eci','active_countries'),('discipline','pci','active_disciplines')]:
                    want=[1+sum(v>x for v in r[scores]) for x in r[scores]] if r['status']=='ok' else []
                    assert rank[side+'_ranks']==want
                    assert rank[side+'_universe']==(len(r[active]) if r['status']=='ok' else 0)
            for filename in ['country_eci.parquet','discipline_pci.parquet']:
                p=str(root/profile['downloads'][method][filename]['path'])
                q.read_parquet(p).create_view('scores',replace=True)
                bad=q.execute("""select count(*) from scores where profile_id<>? or profile_run_id<>? or source_run_id<>? or method<>? or scenario_id<>'fractional-concept-whole-0.3' or (valid and (not active or model_status<>'ok' or global_rank is null)) or (not valid and global_rank is not null)""",[profile['profile']['id'],profile['profile_run_id'],m['source_run_id'],method]).fetchone()[0]
                assert bad==0
                bad=q.execute("""select count(*) from (select global_rank,global_universe,rank()over(partition by scenario_id,year,scheme,level,metric order by score desc)expected,count(*)over(partition by scenario_id,year,scheme,level,metric)n from scores where valid) where global_rank<>expected or global_universe<>n""").fetchone()[0]
                assert bad==0
    # Published baseline results are compared field-for-field and Float64-bit-
    # for-bit after exactly the disclosed heavy-network-array pruning.
    baseline=next(p for p in m['profiles'] if p['profile']['id']=='baseline-v1')
    source_roots={kind:Path(next(a['path'] for a in m['source']['inputs'] if a['path']==kind+'/completion.json')) for kind in ['base','methods']}
    origbase=Path('/mnt/nvme0/public/openalex-stats/results')/({'original-core':'core-concept-hits-v1','original-all':'all-concept-hits-v1','enriched-core':'core-country-validated-v3-r3-20261113','enriched-all':'all-country-validated-v3-r3-20261113'}[name])
    origmethod=Path('/mnt/nvme0/public/openalex-stats/results')/({'original-core':'core-java-reflections-v1','original-all':'all-java-reflections-v1','enriched-core':'core-country-java-v3-r3-20261113','enriched-all':'all-country-java-v3-r3-20261113'}[name])
    for method,source in [('spectral-v1',origbase/'eci.json'),('java-reflections-v1',origmethod/'java-reflections-v1.json')]:
        old=read(source);new=read(root/baseline['models'][method]['path']);lookup={modelkey(x):x for x in old}
        assert len(old)==len(new)==216
        for model in new:
            previous=lookup[modelkey(model)]['result']
            previous['rca']=previous['advantage']=previous['components']=None
            assert exact(previous,model['result']),(name,method,modelkey(model))
        del old,new,lookup
    default=next(p for p in m['profiles'] if p['profile']['id']==DEFAULT)
    if args.version=='v2':
        previous=Path(str(BASE).replace('profiles-v2-','profiles-v1-'))/name
        old_manifest=json.load(open(previous/'manifest.json'));old_profiles={p['profile']['id']:p for p in old_manifest['profiles']}
        for profile in m['profiles']:
            old_profile=old_profiles[profile['profile']['id']]
            assert profile['diagnostics']['sha256']==old_profile['diagnostics']['sha256'],'filter/X/M diagnostics changed across direction-only release'
            alignment=json.load(open(root/profile['alignment']['path']))
            assert alignment['direction_policy']==m['direction_policy']
            if profile['profile']['id']=='baseline-v1':
                assert alignment['chains']==[]and alignment['model_source']=='copied_publication'
                assert profile['models']==old_profile['models'],'baseline actual model bytes changed'
            else:
                assert len(alignment['chains'])==16
                for chain in alignment['chains']:
                    assert chain['factor']in[-1,1]and chain['year_range']==[2000,2025]
    diagnostics=json.load(open(root/default['diagnostics']['path']))
    cases=[]
    for row in diagnostics:
        if row['year']==2025 and ((row['scheme']=='concepts'and row['level']=='0'and row['metric']=='citations')or(row['scheme']=='topics'and row['level']=='field'and row['metric']=='publications')):
            d=row['diagnostics'];focus=[c for c in d['countries'] if c['country']in['AG','EH','SG','CN','US','KP']]
            case={k:row[k] for k in ['year','scheme','level','metric']};case.update(retained_assignment_fraction=d['retained_mass_fraction'],country_scale_removed=d['country_scale_removed'],diversity_removed=d['diversity_removed'],rounds=len(d['rounds']),focus=focus,methods={})
            for method in ['spectral-v1','java-reflections-v1']:
                models=json.load(open(root/default['models'][method]['path']));model=next(x for x in models if modelkey(x)==modelkey(row));r=model['result']
                rankdata=json.load(open(root/default['ranks'][method]['path']));ranks=next(x for x in rankdata if modelkey(x)==modelkey(row))
                entries=[]
                for country in ['AG','EH','SG','CN','US','KP']:
                    label=r['countries'].index(country);entry=dict(country=country,active=label in r['active_countries'],status=r['status'])
                    if r['status']=='ok'and label in r['active_countries']:
                        i=r['active_countries'].index(label);entry.update(score=r['eci'][i],rank=ranks['country_ranks'][i],universe=ranks['country_universe'])
                    entries.append(entry)
                case['methods'][method]=entries
            cases.append(case)
    archive=OUT/(name+'.tar.gz')
    with tarfile.open(archive,'w:gz',compresslevel=1)as tar:
        for p in sorted(root.rglob('*')):
            if p.is_file():tar.add(p,arcname=str(p.relative_to(root)),recursive=False)
    item=dict(name=name,root=str(root),source_run_id=m['source_run_id'],source_completion_sha256=m['source_completion_sha256'],engine_binary_sha256=m['engine_binary_sha256'],profiles=19,models_per_method_per_profile=216,unique_artifacts_verified=len(artifacts),baseline_exact_model_checks=432,rank_json_and_parquet_checks='all profiles/methods/country+discipline passed',default_cases=cases,manifest_sha256=sha(root/'manifest.json'),archive_path=str(archive),archive_bytes=archive.stat().st_size,archive_sha256=sha(archive),unpacked_bytes=sum(p.stat().st_size for p in root.rglob('*')if p.is_file()))
    report['bundles'].append(item)
    if args.version=='v2':item.update(direction_policy=m['direction_policy'],filter_diagnostics_unchanged_v1=True,baseline_model_bytes_unchanged_v1=True)
    (OUT/REPORT_NAME).write_text(json.dumps(report,indent=2)+'\n')
    print(json.dumps(dict(name=name,status='passed',profiles=19,artifacts=len(artifacts),transport_bytes=item['archive_bytes'],seconds=round(time.monotonic()-started,2))),flush=True)
report['status']='passed_artifacts_baseline_exact_scores_and_independent_ranks'
(OUT/REPORT_NAME).write_text(json.dumps(report,indent=2)+'\n')
print('passed',len(report['bundles']),'bundles',flush=True)
