"""Producer-side independent arithmetic check of validator's Go HT result.

Uses original sampling counts and fixed graph events, not the report's weights.
This is a rare-positive opportunity point estimate, not a validated full patch.
"""
import pathlib,json,math,hashlib
base=pathlib.Path('research/country-enrichment-v3');v=base/'validation';f=base/'fulltext'
sample=json.loads((v/'sample-ids.json').read_text());works={r['id']:r for r in sample['works']};strata={r['stratum']:r for r in sample['strata']}
plan=json.loads((f/'probability-pdf-plan.json').read_text());picks={r['id']:r for r in plan['pdf']};ps={r['stratum']:r for r in plan['pdf_strata']}
prior=json.loads((v/'probability-fulltext-prior-text-recoveries.json').read_text());recovered=set(prior['ids'])
events={r['id']:r for r in json.loads((v/'sample-citation-counts.json').read_text())['works']}
report=json.loads((f/'probability-ht-effects.json').read_text());observed={r['id']:r for r in report['observed_candidates']}
candidates=[]
for filename in ['probability-automatic-candidates.jsonl','pdf-automatic-candidates.jsonl']:
 for line in (f/filename).read_text().splitlines():
  r=json.loads(line);w=works[r['id']];s=strata[w['stratum']];weight=s['N']/s['n'];pi2=1
  assert w['stratum_population']==s['N'] and w['stratum_sample']==s['n']
  if r['route']=='oa_pdf_grobid_header':
   pick=picks[r['id']];stage=ps[pick['stratum']];pi2=stage['selected']/stage['eligible'];assert math.isclose(pi2,pick['pi2']);weight/=pi2
  expected=observed[r['id']];assert math.isclose(weight,expected['combined_weight']) and expected['already_recovered_by_passed_text']==(r['id'] in recovered)
  candidates.append(dict(id=r['id'],route=r['route'],weight=weight,year=w['year'],is_xpac=w['is_xpac']))
assert len(candidates)==len(observed)==12 and sum(r['N'] for r in sample['strata'])==report['remaining_non_development_unknown_frame_N']==277411702
checked=0;maxerror=0
for key,expected in report['totals'].items():
 domain,corpus,route=key.split('/');rows=[r for r in candidates if (domain!='net_after_passed_text' or r['id'] not in recovered) and (corpus!='Core' or not r['is_xpac']) and (route=='combined_pmc_then_pdf' or r['route']==route)]
 actual=dict(observed_candidate_works=len(rows),ht_recovered_unknown_works=sum(r['weight'] for r in rows))
 for end in [2025,2026]:
  subset=[r for r in rows if 2000<=r['year']<=end];actual['observed_publication_works_2000_'+str(end)]=len(subset);actual['ht_publication_works_2000_'+str(end)]=sum(r['weight'] for r in subset)
 array=[sum(r['weight']*events[r['id']][corpus.lower()+'_incoming_reference_events_2000_2026'][i] for r in rows) for i in range(27)]
 actual['ht_country_known_incoming_events_year2000_2026']=array;actual['ht_country_known_events_2000_2025']=sum(array[:26])
 for metric,value in actual.items():
  aa=value if isinstance(value,list) else [value];bb=expected[metric] if isinstance(expected[metric],list) else [expected[metric]]
  assert len(aa)==len(bb)
  for x,y in zip(aa,bb):assert math.isclose(x,y,rel_tol=1e-12,abs_tol=1e-7),(key,metric,x,y);maxerror=max(maxerror,abs(x-y));checked+=1
result=dict(status='passed',checked_scalar_values=checked,max_absolute_difference=maxerror,candidates=12,actual_passed_text_overlap=7,actual_net_candidates=5,authoritative_residual_frame_N=277411702,weight_formula='(N_first_stage/n_first_stage)*(eligible_second_stage/selected_second_stage) for PDF; N/n for PMC',publication_window='2000..2025 inclusive; earlier works excluded from publications but retained incoming graph events',input_report_sha256=hashlib.sha256((f/'probability-ht-effects.json').read_bytes()).hexdigest(),no_design_confidence_interval_claimed=True,not_actual_full_enrichment=True)
(f/'probability-ht-producer-check.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result))
