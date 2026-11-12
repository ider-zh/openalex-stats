import pathlib,json,math,hashlib
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/results');v=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/validation')
def corr_pairs(p):
 n=len(p)
 if n<2:return None
 mx=sum(x for x,y in p)/n;my=sum(y for x,y in p)/n;sxx=sum((x-mx)**2 for x,y in p);syy=sum((y-my)**2 for x,y in p);cov=sum((x-mx)*(y-my) for x,y in p)
 return cov/math.sqrt(sxx*syy) if sxx*syy>0 else None
def vec(r,key='eci'):
 labels=r['countries'] if key=='eci' else r['disciplines'];act=r['active_countries'] if key=='eci' else r['active_disciplines'];return {labels[i]:val for i,val in zip(act,r.get(key,[]))}
def cross(a,b):return corr_pairs([(a[k],b[k]) for k in a.keys()&b.keys()])
def diversity(r):return {r['countries'][i]:sum(r['advantage'][i]) for i in r['active_countries']}
def load(name):
 p=b/name/'eci.json';raw=p.read_bytes();xs=json.loads(raw);return { (m['scenario_id'],m['scheme'],str(m['level']),m['metric'],m['year']):m['result'] for m in xs },hashlib.sha256(raw).hexdigest()
a,sha=load('core-concept-hits-v1');z,shz=load('core-country-text-v3-20261112');keys=sorted(set(a)|set(z));rows=[]
for k in keys:
 ar=a.get(k,{});zr=z.get(k,{});av=vec(ar) if ar.get('status')=='ok' else {};zv=vec(zr) if zr.get('status')=='ok' else {};co=cross(av,zv);orientation=-1 if co is not None and co<0 else 1;common=av.keys()&zv.keys();rmse=math.sqrt(sum((zv[c]-av[c])**2 for c in common)/len(common)) if common else None;rmse_aligned=math.sqrt(sum((orientation*zv[c]-av[c])**2 for c in common)/len(common)) if common else None
 prevs=[]
 for dataset,models,r,val in [('before',a,ar,av),('after',z,zr,zv)]:
  earlier=[kk for kk in models if kk[:4]==k[:4] and kk[4]<k[4] and models[kk].get('status')=='ok'];pk=max(earlier,key=lambda kk:kk[4]) if earlier else None
  prevs.append({'dataset':dataset,'previous_valid_year':pk[4] if pk else None,'stored_previous_year_eci_correlation':cross(val,vec(models[pk])) if pk and val else None,'stored_eci_diversity_correlation':cross(val,diversity(r)) if val else None,'sign_convention':r.get('sign_convention'),'eigen_gap':r.get('eigen_gap'),'unstable':r.get('unstable'),'status':r.get('status')})
 row={'scenario_id':k[0],'scheme':k[1],'level':k[2],'metric':k[3],'year':k[4],'common_countries':len(common),'before_after_eci_correlation':co,'orientation_free_abs_correlation':abs(co) if co is not None else None,'raw_rmse':rmse,'sign_matched_rmse':rmse_aligned,'pci_correlation':cross(vec(ar,'pci'),vec(zr,'pci')) if av and zv else None,'temporal_and_diversity':prevs}
 if k[1:4]==('concepts','0','citations') and k[4]==2025:
  ranks=lambda d:{c:1+sum(x>val for x in d.values()) for c,val in d.items()};ra=ranks(av);rz=ranks(zv);rzflip=ranks({c:-val for c,val in zv.items()});row['examples']=[{'country':c,'before_score':av.get(c),'after_score':zv.get(c),'before_rank':ra.get(c),'after_rank':rz.get(c),'after_sign_matched_rank':rzflip.get(c)} for c in ['SG','KP','IR','EG','US','CN','DE']];row['max_absolute_raw_rank_change_common']=max(abs(ra[c]-rz[c]) for c in common);row['max_absolute_sign_matched_rank_change_common']=max(abs(ra[c]-rzflip[c]) for c in common)
 rows.append(row)
target=[r for r in rows if (r['scheme'],r['level'],r['metric'])==('concepts','0','citations')];flips=[r for r in rows if r['before_after_eci_correlation'] is not None and r['before_after_eci_correlation']<0];x={'status':'independent_sign_convention_diagnostic','baseline_eci_sha256':sha,'enriched_eci_sha256':shz,'methodology_changed':False,'production_changed':False,'models_compared':len(rows),'negative_cross_run_eci_correlations':len(flips),'target_concepts0_citations_all_years':target,'all_model_comparisons':rows};p=v/'core-spectral-sign-diagnostic.json';p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n');print(json.dumps({'negative_models':len(flips),'target':target},ensure_ascii=False))
