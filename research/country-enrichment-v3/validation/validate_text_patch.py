import duckdb,json,pathlib,hashlib
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/text-validation');p=b/'validated-text-patch.parquet';c=duckdb.connect();c.execute('set threads=8');c.execute("set memory_limit='16GB'")
gate=json.load(open(b/'raw-gate.json'))['routes'][0];wrapper=json.load(open(b/'passed-gate.json'));manifest=json.load(open(b/'overlay-manifest.json'));q=json.load(open(b/'quarantine.json'))
assert hashlib.sha256((b/'raw-gate.json').read_bytes()).hexdigest()==wrapper['independent_gate_sha256']
assert hashlib.sha256((b/'quarantine.json').read_bytes()).hexdigest()==wrapper['mandatory_quarantine_sha256']
assert hashlib.sha256((b/'passed-gate.json').read_bytes()).hexdigest()==manifest['gates'][0]['sha256']
assert hashlib.sha256(p.read_bytes()).hexdigest()==manifest['patch_sha256']
n=c.execute('''select count(*),count(distinct id),sum(id=4237692947),
 sum(len(original_countries)>0),sum(len(proposed_countries)=0),
 sum(len(proposed_countries)!=list_unique(proposed_countries)),
 sum(country_set_complete!='unknown') from read_parquet(?)''',[str(p)]).fetchone()
assert n==(2667268,2667268,0,0,0,0,0),n
assert gate['Selected']==256 and gate['Successes']==255 and gate['WilsonLower']>=.97 and gate['Precision']>=.99
result=dict(status='independent_validation_passed',rows=n[0],unique_ids=n[1],known_error_absent=True,all_original_countries_empty=True,all_candidate_sets_nonempty_distinct=True,no_full_country_completeness_claim=True,hash_chain_passed=True,precision_denominator=256,precision_successes=255,pre_quarantine_precision_retained=True,known_error_class_quarantine=q,patch_sha256=manifest['patch_sha256'],gate_sha256=manifest['gates'][0]['sha256'],production_applied=False)
out=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/validation/root-text-overlay-independent-check.json');out.write_text(json.dumps(result,ensure_ascii=False,indent=2)+'\n');print(json.dumps(result,ensure_ascii=False))
