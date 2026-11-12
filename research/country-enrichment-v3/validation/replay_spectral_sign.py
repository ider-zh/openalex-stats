import pathlib,json,math,hashlib
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/results');v=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/validation')
def cov(p):
 mx=sum(x for x,y in p)/len(p);my=sum(y for x,y in p)/len(p);return sum((x-mx)*(y-my) for x,y in p)
def vectors(r):return {r['countries'][c]:val for c,val in zip(r['active_countries'],r.get('eci',[]))}
results=[]
for name in ['core-concept-hits-v1','core-country-text-v3-20261112']:
 models=json.loads((b/name/'eci.json').read_text());models=sorted(models,key=lambda m:m['year']);previous={};mismatches=[];nearzero=[];target=[]
 for m in models:
  r=m['result'];key=(m['scenario_id'],m['scheme'],str(m['level']),m['metric'])
  if r['status']!='ok':continue
  stored=vectors(r);div={r['countries'][i]:sum(r['advantage'][i]) for i in r['active_countries']};cv=cov([(stored[k],div[k]) for k in stored]);orientation=-1 if cv<0 else 1
  if abs(cv)<1e-14:nearzero.append({**{k:m[k] for k in ['scheme','level','metric','year']},'diversity_cov':cv})
  fresh={k:orientation*x for k,x in stored.items()};prev=previous.get(key)
  if prev:
   pairs=[(fresh[k],prev[k]) for k in fresh.keys()&prev.keys()]
   if len(pairs)>=2 and cov(pairs)<0:fresh={k:-x for k,x in fresh.items()}
  mismatch=max(abs(fresh[k]-stored[k]) for k in stored)
  if mismatch>1e-12:mismatches.append({**{k:m[k] for k in ['scheme','level','metric','year']},'max_stored_replay_diff':mismatch})
  previous[key]=fresh
  if key[1:]==('concepts','0','citations') and m['year'] in [2000,2025]:
   E=r['eci'];M=r['advantage'];active=r['active_countries'];disc=r['active_disciplines'];kc=[sum(M[i]) for i in active];kd={j:sum(M[i][j] for i in active) for j in disc};pj={j:sum(E[x] for x,i in enumerate(active) if M[i][j])/kd[j] for j in disc};te=[sum(pj[j] for j in disc if M[i][j])/kc[x] for x,i in enumerate(active)];mean=sum(te)/len(te);lam=r['second_eigenvalue'];rel=math.sqrt(sum(((te[x]-mean)-lam*E[x])**2 for x in range(len(E)))/sum(x*x for x in E));target.append({'year':m['year'],'stored_eci_diversity_covariance':cv,'independent_centered_row_operator_eigen_residual_relative':rel,'second_eigenvalue':lam,'active_countries':len(E),'active_disciplines':len(disc),'max_replayed_alignment_difference':mismatch})
 results.append({'dataset':name,'align_replay_mismatches':mismatches,'nearzero_diversity_covariance_models':nearzero,'target_operator_checks':target})
x={'status':'independent_alignment_and_operator_replay_completed','production_or_model_changed':False,'algorithm':'Restore annual positive-diversity orientation from each stored eigenvector, then independently replay declared common-country covariance temporal alignment. Separately evaluate centered row-stochastic T=Dc^-1 M Dp^-1 M^T operator on stored ECI.','results':results};p=v/'core-spectral-sign-replay.json';p.write_text(json.dumps(x,ensure_ascii=False,indent=2)+'\n');print(json.dumps(x))
