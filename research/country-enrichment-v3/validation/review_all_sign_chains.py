import pathlib,json,math,hashlib,gc
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/results');v=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/validation')
def corr(a,b):
 ks=a.keys()&b.keys();n=len(ks)
 if n<2:return None
 mx=sum(a[k] for k in ks)/n;my=sum(b[k] for k in ks)/n;vx=sum((a[k]-mx)**2 for k in ks);vy=sum((b[k]-my)**2 for k in ks)
 return sum((a[k]-mx)*(b[k]-my) for k in ks)/math.sqrt(vx*vy) if vx*vy>0 else None
def cov(a,b):
 ks=a.keys()&b.keys();n=len(ks)
 if n<2:return None
 mx=sum(a[k] for k in ks)/n;my=sum(b[k] for k in ks)/n;return sum((a[k]-mx)*(b[k]-my) for k in ks)
def load(d,f):
 p=b/d/f;raw=p.read_bytes();sha=hashlib.sha256(raw).hexdigest();xs=json.loads(raw);del raw;out={};previous={};bad=[];nearzero=[]
 for m in sorted(xs,key=lambda m:m['year']):
  key=(m['scenario_id'],m['scheme'],str(m['level']),m['metric']);r=m['result'];E={r['countries'][j]:x for j,x in zip(r.get('active_countries',[]),r.get('eci',[]))};P={r['disciplines'][j]:x for j,x in zip(r.get('active_disciplines',[]),r.get('pci',[]))};D={r['countries'][j]:sum(r['advantage'][j]) for j in r.get('active_countries',[])};valid=r['status']=='ok';cv=cov(E,D) if valid else None;prev=previous.get(key);replaydiff=None
  if valid:
   if cv is None or abs(cv)<1e-14:nearzero.append([*key,m['year']])
   restore=-1 if cv is not None and cv<0 else 1;fresh={k:restore*x for k,x in E.items()}
   if prev:
    c=cov(fresh,prev['E'])
    if c is not None and c<0:fresh={k:-x for k,x in fresh.items()}
   replaydiff=max(abs(fresh[k]-E[k]) for k in E)
   if replaydiff>1e-12:bad.append([*key,m['year'],replaydiff])
  z={'year':m['year'],'status':r['status'],'E':E,'P':P,'diversity':D,'diversity_correlation':corr(E,D) if valid else None,'diversity_covariance':cv,'temporal_correlation':corr(E,prev['E']) if valid and prev else None,'previous_valid_year':prev['year'] if prev else None,'replay_max_abs_difference':replaydiff,'eigen_gap':r.get('eigen_gap'),'unstable':r.get('unstable'),'sign_convention':r.get('sign_convention')};out[key+(m['year'],)]=z
  if valid:previous[key]=z
 del xs;gc.collect();return out,{'path':str(p),'sha256':sha,'models':len(out),'replay_mismatches':bad,'near_zero_annual_diversity_covariance':nearzero}
pairs=[('Core','spectral','core-concept-hits-v1','core-country-text-v3-20261112','eci.json'),('All','spectral','all-concept-hits-v1','all-country-text-v3-r1-20261112','eci.json'),('Core','java_reflections','core-baseline-java-stable-v3-20261112','core-country-text-java-v3-r1-20261112','java-reflections-v1.json'),('All','java_reflections','all-baseline-java-stable-v3-20261112','all-country-text-java-v3-r1-20261112','java-reflections-v1.json')];chains=[];sources=[]
for corpus,method,before,after,file in pairs:
 a,sa=load(before,file);z,sz=load(after,file);sources.extend([sa,sz]);keys=sorted({k[:4] for k in set(a)|set(z)})
 for key in keys:
  years=sorted({k[4] for k in set(a)|set(z) if k[:4]==key});both=[y for y in years if a.get(key+(y,),{}).get('status')=='ok' and z.get(key+(y,),{}).get('status')=='ok' and len(a[key+(y,)]['E'].keys()&z[key+(y,)]['E'].keys())>=2];anchor=both[0] if both else None;ac=corr(a[key+(anchor,)]['E'],z[key+(anchor,)]['E']) if anchor is not None else None;multiplier=(-1 if ac<0 else 1) if ac is not None else None;yearrows=[]
  for y in years:
   ar=a.get(key+(y,),{});zr=z.get(key+(y,),{});valid=ar.get('status')=='ok' and zr.get('status')=='ok';ec=corr(ar['E'],zr['E']) if valid else None;pc=corr(ar['P'],zr['P']) if valid else None;rec={'year':y,'before_status':ar.get('status'),'after_status':zr.get('status'),'raw_eci_correlation':ec,'coupled_eci_correlation':multiplier*ec if multiplier is not None and ec is not None else None,'raw_pci_correlation':pc,'coupled_pci_correlation':multiplier*pc if multiplier is not None and pc is not None else None,'before_diversity_correlation':ar.get('diversity_correlation'),'after_diversity_correlation':zr.get('diversity_correlation'),'before_temporal_correlation':ar.get('temporal_correlation'),'after_temporal_correlation':zr.get('temporal_correlation'),'before_eigen_gap':ar.get('eigen_gap'),'after_eigen_gap':zr.get('eigen_gap'),'before_unstable':ar.get('unstable'),'after_unstable':zr.get('unstable')};yearrows.append(rec)
  chains.append({'corpus':corpus,'method':method,'scenario_id':key[0],'scheme':key[1],'level':key[2],'metric':key[3],'anchor_year':anchor,'anchor_common_country_eci_correlation':ac,'diagnostic_coupled_multiplier_new_vs_before':multiplier,'pci_uses_same_eci_multiplier':True,'before_first_valid_year':min([k[4] for k,vv in a.items() if k[:4]==key and vv['status']=='ok'],default=None),'after_first_valid_year':min([k[4] for k,vv in z.items() if k[:4]==key and vv['status']=='ok'],default=None),'years':yearrows})
 print(corpus,method,'chains',len(keys),'loaded and independently replayed',flush=True)
 del a,z;gc.collect()
x={'status':'independent_core_all_spectral_java_sign_chain_review_complete','production_or_method_changed':False,'source_models':sources,'chains':chains,'rules':'Only diagnostic: fix sign once at earliest common valid year using shared-country ECI correlation; same multiplier entire yearly ECI and PCI chain; retain all original model values. Report raw/coupled diagnostics together. Low anchor or annual absolute correlations may signal structural rather than sign-only changes.'};p=v/'all-method-sign-chain-review.json';p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n');print(json.dumps({'chains':len(chains),'negative_anchor_chains':[{k:r[k] for k in ['corpus','method','scheme','level','metric','anchor_year','anchor_common_country_eci_correlation']} for r in chains if r['diagnostic_coupled_multiplier_new_vs_before']==-1],'replay_mismatches':sum(len(s['replay_mismatches']) for s in sources),'nearzero_annual_diversity':sum(len(s['near_zero_annual_diversity_covariance']) for s in sources)}))
