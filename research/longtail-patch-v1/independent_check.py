"""Independent DuckDB input extraction and NumPy fixed-point/math audit.

Use the existing DuckDB Python for `extract` and NumPy Python for `audit`.
Neither mode invokes the Go filtering/solver implementation or changes data.
"""
import gzip
import hashlib
import json
import math
import struct
import sys
from pathlib import Path

BUNDLES=Path('/mnt/nvme0/public/openalex-stats/complexity-profiles-v2-20261113')
RAW_BUNDLES=Path('/mnt/nvme0/public/openalex-stats/complexity-profiles-v1-20261113')
OUT=Path('/mnt/nvme0/public/openalex-stats/analysis-longtail-profiles-v1-20261113/independent-v2')
RESULTS=Path('/mnt/nvme0/public/openalex-stats/results')
SOURCES={'original-core':'core-concept-hits-v1','original-all':'all-concept-hits-v1',
         'enriched-core':'core-country-validated-v3-r3-20261113','enriched-all':'all-country-validated-v3-r3-20261113'}
YEARS=[2000,2012,2020,2025]
SCORE_PROFILES={'p100-c1000-x0-d1-v1','p1000-c1000-x5-d3-v1','p5000-c1000-x20-d3-v1'}

def read(p):
    return json.loads(Path(p).read_text(encoding='utf8'))

def sha(p):
    h=hashlib.sha256()
    with Path(p).open('rb') as stream:
        while b:=stream.read(8*1024*1024): h.update(b)
    return h.hexdigest()

def key(row):
    return (int(row['year']),row['scheme'],str(row['level']),row['metric'])

def extract():
    import duckdb
    OUT.mkdir(parents=True,exist_ok=True)
    frozen=[];sources=[]
    for dataset,source in SOURCES.items():
        folder=RESULTS/source;manifest=read(BUNDLES/dataset/'manifest.json');dictionary=read(folder/'dictionary.json')
        assert sha(folder/'statistics.parquet')==manifest['source']['statistics_sha256']
        assert sha(folder/'dictionary.json')==manifest['source']['dictionary_sha256']
        countries=[x['id'] for x in dictionary['countries'] if x['id']!='UNKNOWN'];ci={x:i for i,x in enumerate(countries)}
        labels={(l['scheme'],str(l['level'])):[x['id'] for x in l['labels'] if x['id']!='UNKNOWN'] for l in dictionary['layers']}
        matrices={};scales={y:[[0]*len(countries),[0]*len(countries)] for y in YEARS}
        for y in YEARS:
            for (scheme,level),subjects in labels.items():
                for metric in ['publications','citations']:
                    matrices[y,scheme,level,metric]={'dataset':dataset,'year':y,'scheme':scheme,'level':level,'metric':metric,
                        'countries':countries,'disciplines':subjects,'values':[[0.0]*len(subjects) for _ in countries]}
        di={layer:{name:i for i,name in enumerate(names)} for layer,names in labels.items()}
        db=duckdb.connect();db.execute('set threads=8');db.execute("set memory_limit='16GB'")
        years=','.join(map(str,YEARS));main=manifest['scenario_id']
        for country,y,scheme,level,subject,p,c in db.execute(f"select country,year,scheme,level,discipline_id,publications,citations from read_parquet(?) where country!='UNKNOWN' and discipline_id!='UNKNOWN' and scenario_id=? and year in ({years})",[str(folder/'statistics.parquet'),main]).fetchall():
            i,j=ci[country],di[scheme,str(level)][subject]
            matrices[y,scheme,str(level),'publications']['values'][i][j]=p
            matrices[y,scheme,str(level),'citations']['values'][i][j]=c
        whole=next(x['id'] for x in read(folder/'scenarios.json') if x['country_mode']=='whole' and x['concept_mode']=='whole' and x['concept_threshold']==.3)
        for country,y,p,c in db.execute(f"select country,year,sum(publications),sum(citations) from read_parquet(?) where country!='UNKNOWN' and scenario_id=? and scheme='topics' and level='field' and year in ({years}) group by country,year",[str(folder/'statistics.parquet'),whole]).fetchall():
            assert abs(p-round(p))<1e-6 and abs(c-round(c))<1e-6
            scales[y][0][ci[country]]=round(p);scales[y][1][ci[country]]=round(c)
        for coords,item in matrices.items():
            item['whole_publications'],item['whole_citations']=scales[item['year']]
            frozen.append(item)
        sources.append({'dataset':dataset,'manifest_sha256':sha(BUNDLES/dataset/'manifest.json'),'statistics_sha256':manifest['source']['statistics_sha256']})
        db.close();print('Extracted',dataset,len(matrices),flush=True)
    target=OUT/'frozen-128-inputs.json.gz'
    with gzip.open(target,'wt',encoding='utf8') as stream: json.dump(frozen,stream,separators=(',',':'))
    (OUT/'frozen-input-manifest.json').write_text(json.dumps({'matrices':len(frozen),'years':YEARS,'sources':sources,'input_sha256':sha(target)},indent=2)+'\n')

def audit():
    import numpy as np
    with gzip.open(OUT/'frozen-128-inputs.json.gz','rt',encoding='utf8') as stream: inputs=json.load(stream)
    groups={}
    for item in inputs: groups.setdefault(item['dataset'],{})[key(item)]=item
    stats={'status':'running','checker_sha256':sha(__file__),'inputs':len(inputs),'fixed_points':0,'score_pairs':0,'rank_models':0,
           'direction_chains':0,'exact_global_sign_model_pairs':0,'max_spectral_error':0.0,'max_reflection_error':0.0}
    def header(kind,item):
        b=('OA-COMPLEXITY-'+kind+'-1\n').encode()+struct.pack('<QQ',len(item['countries']),len(item['disciplines']))
        return b+b''.join(struct.pack('<Q',len(s.encode()))+s.encode() for labels in [item['countries'],item['disciplines']] for s in labels)
    def digest(kind,x,item):
        data=x.astype('u1' if kind=='NETWORK' else '<f8').tobytes(order='C')
        return hashlib.sha256(header(kind,item)+data).hexdigest()
    def network(x):
        rows,cols,total=x.sum(1),x.sum(0),x.sum()
        rca=np.divide(np.divide(x,rows[:,None],out=np.zeros_like(x),where=rows[:,None]>0),
                      np.divide(cols,total,out=np.zeros_like(cols),where=total>0)[None,:],out=np.zeros_like(x),where=(rows[:,None]>0)&(cols[None,:]>0)&(total>0))
        rca[np.abs(rca-1)<=1e-12]=1
        return rca>=1
    def filtered(item,profile):
        original=np.asarray(item['values'],dtype=float);x=original.copy()
        exclude=np.asarray(item['whole_publications'])<profile['country_min_publications']
        if item['metric']=='citations': exclude|=np.asarray(item['whole_citations'])<profile['country_min_citation_events']
        x[exclude]=0;x[x<profile['cell_min_support']]=0
        passes=0
        while True:
            passes+=1;m=network(x);remove=(x.sum(1)>0)&(m.sum(1)<profile['min_diversity'])
            if not remove.any():return original,x,m,passes
            x[remove]=0
            assert passes<=len(x)
    def z(v):
        shifted=np.asarray(v)-v[0];centered=shifted-shifted.mean();return centered/np.sqrt(np.mean(centered*centered))
    def competition(v):return [1+sum(w>x for w in v) for x in v]
    for dataset,selected in groups.items():
        root=BUNDLES/dataset;manifest=read(root/'manifest.json');raw_manifest=read(RAW_BUNDLES/dataset/'manifest.json')
        assert manifest['direction_policy']=='baseline-pooled-common-year-sign-v1'
        old_entries={x['profile']['id']:x for x in raw_manifest['profiles']}
        baseline=next(x for x in manifest['profiles'] if x['profile']['id']=='baseline-v1')
        base_models={method:{key(m):m['result'] for m in read(root/baseline['models'][method]['path'])} for method in ['spectral-v1','java-reflections-v1']}
        for entry in manifest['profiles']:
            profile=entry['profile'];pid=profile['id'];diags={key(x):x['diagnostics'] for x in read(root/entry['diagnostics']['path'])}
            expected={}
            for coords,item in selected.items():
                old,x,m,passes=filtered(item,profile);d=diags[coords]
                assert digest('MATRIX',old,item)==d['source_matrix_sha256'],(dataset,pid,coords,'sourceX')
                assert digest('MATRIX',x,item)==d['final_matrix_sha256'],(dataset,pid,coords,'finalX')
                assert digest('NETWORK',m,item)==d['final_network_sha256'],(dataset,pid,coords,'M')
                assert passes==len(d['rounds']),(dataset,pid,coords,'closure')
                assert abs(x.sum()-d['final_mass'])<=1e-10*max(1,x.sum())
                ac=np.flatnonzero(x.sum(1));ad=np.flatnonzero(x.sum(0));expected[coords]=(m,ac,ad)
                stats['fixed_points']+=1
            for method in ['spectral-v1','java-reflections-v1']:
                models={key(x):x['result'] for x in read(root/entry['models'][method]['path'])}
                raw_models={key(x):x['result'] for x in read(RAW_BUNDLES/dataset/old_entries[pid]['models'][method]['path'])}
                direction=read(root/entry['alignment']['path'])
                chains={(c['method'],c['scheme'],str(c['level']),c['metric']):c for c in direction['chains']}
                for coords,r in models.items():
                    prior=raw_models[coords]
                    assert r['status']==prior['status'] and r['active_countries']==prior['active_countries'] and r['active_disciplines']==prior['active_disciplines']
                    factor=1 if pid=='baseline-v1' else chains[method,coords[1],coords[2],coords[3]]['factor']
                    for field in ['eci','pci']:
                        assert (r.get(field) or [])==[factor*v for v in (prior.get(field) or [])],(dataset,pid,coords,method,'not one exact global factor')
                    stats['exact_global_sign_model_pairs']+=1
                if pid!='baseline-v1':
                    for chain_key,c in chains.items():
                        if chain_key[0]!=method:continue
                        cov=var_a=var_b=0.0;pairs=0
                        for coords,prior in raw_models.items():
                            if coords[0] not in range(2000,2026) or coords[1:]!=chain_key[1:]:continue
                            reference=base_models[method][coords]
                            if prior['status']!='ok' or reference['status']!='ok':continue
                            a={prior['countries'][i]:v for i,v in zip(prior['active_countries'],prior['eci'])}
                            b={reference['countries'][i]:v for i,v in zip(reference['active_countries'],reference['eci'])}
                            common=sorted(a.keys()&b.keys())
                            if len(common)<2:continue
                            aa=np.asarray([a[k] for k in common]);bb=np.asarray([b[k] for k in common]);aa-=aa.mean();bb-=bb.mean()
                            cov+=float(aa@bb);var_a+=float(aa@aa);var_b+=float(bb@bb);pairs+=len(common)
                        rho=cov/math.sqrt(var_a*var_b) if var_a>0 and var_b>0 else None
                        if rho is not None:
                            assert abs(rho-c['raw_pooled_rho'])<1e-12
                            assert c['factor']==(-1 if cov<0 else 1)
                            assert abs(c['aligned_pooled_rho']-abs(rho))<1e-12
                            assert c['weak_reference']==(abs(rho)<.2)
                        assert c['common_pair_n']==pairs
                        stats['direction_chains']+=1
                ranks={key(x):x for x in read(root/entry['ranks'][method]['path'])}
                for coords,r in models.items():
                    rank=ranks[coords]
                    if r['status']=='ok':
                        for field,rf in [('eci','country_ranks'),('pci','discipline_ranks')]:
                            values=r[field];assert competition(values)==rank[rf]
                            assert abs(np.mean(values))<1e-6 and abs(np.mean(np.asarray(values)**2)-1)<1e-6,(dataset,pid,coords,method,'z-score')
                    else:
                        assert not r.get('eci') and not r.get('pci') and not rank['country_ranks'] and not rank['discipline_ranks']
                    stats['rank_models']+=1
                if pid not in SCORE_PROFILES:continue
                for coords,(m,ac,ad) in expected.items():
                    r=models[coords];assert ac.tolist()==r['active_countries'] and ad.tolist()==r['active_disciplines']
                    if r['status']!='ok':continue
                    mm=m[np.ix_(ac,ad)].astype(float);kc,kd=mm.sum(1),mm.sum(0)
                    if method=='spectral-v1':
                        symmetric=(mm/kd)@mm.T/np.sqrt(kc[:,None]*kc[None,:]);e,v=np.linalg.eigh(symmetric)
                        c=v[:,-2]/np.sqrt(kc);p=mm.T@c/kd;c,p=z(c),z(p)
                    else:
                        c,p=kc.copy(),kd.copy()
                        for iteration in range(1,18):
                            c,p=mm@p/kc,mm.T@c/kd
                            if iteration==16:target=c.copy()
                        c,p=z(target),z(p)
                    sign=1 if np.dot(c,r['eci'])>=0 else -1
                    error=max(float(np.max(np.abs(sign*c-r['eci']))),float(np.max(np.abs(sign*p-r['pci']))))
                    field='max_spectral_error' if method=='spectral-v1' else 'max_reflection_error';stats[field]=max(stats[field],error)
                    assert error<1e-6,(dataset,pid,coords,method,error)
                    stats['score_pairs']+=1
            print('Audited',dataset,pid,flush=True)
    stats['status']='passed_independent_numpy_filters_network_hashes_scores_and_ranks'
    stats['scope']='All19 profiles/4 datasets ranks+population z; independent fixed points on128 matrices (4years×4layers×2bases×4data); eigenscores and16/17 reflections for3 frozen profiles, jointly sign-matched for numerical comparison. Full pooled-baseline direction independently reproduced; each new27-year ECI/PCI chain exactly one common factor relative to preserved v1 raw models. No audit alters any production output.'
    (OUT/'numpy-audit.json').write_text(json.dumps(stats,indent=2)+'\n')
    print(json.dumps(stats))

if __name__=='__main__':
    if sys.argv[1]=='extract':extract()
    elif sys.argv[1]=='audit':audit()
    else:raise SystemExit('extract|audit')
