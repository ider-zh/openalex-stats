"""Close the release only after real production acceptance; stop exact candidates."""
import datetime, hashlib, json, os, signal, time, urllib.request
from pathlib import Path
BASE=Path('/home/ider/tmpdata/openalex-stats-dev-20261112');OUT=BASE/'ui-complexity-method-v13'
read=lambda p:json.loads(p.read_text());sha=lambda b:hashlib.sha256(b).hexdigest()
def get(path):
    with urllib.request.urlopen('http://127.0.0.1:8899'+path,timeout=60) as r:return r.read()
def process(pid):
    p=Path('/proc')/str(pid)
    try:
        parts=(p/'stat').read_text().rsplit(') ',1)[1].split()
        if parts[0]=='Z':return None
        return {'pid':pid,'exe':os.readlink(p/'exe'),'argv':[x.decode() for x in (p/'cmdline').read_bytes().split(b'\0') if x],'start_clock_ticks':int(parts[19])}
    except FileNotFoundError:return None
def exact(identity):
    p=process(identity['pid']);assert p and p==identity,'identity changed';return p
def stop(identity):
    exact(identity);os.kill(identity['pid'],signal.SIGTERM)
    for _ in range(150):
        if process(identity['pid']) is None:return
        time.sleep(.2)
    raise RuntimeError('candidate did not stop')
promotion=read(OUT/'promotion.json');assert promotion['status']=='ready'
identity=exact(promotion['new']);assert sha(Path(identity['exe']).read_bytes())==promotion['new_binary_sha256']
reports={}
for corpus in ['core','all']:
    p=OUT/f'production-{corpus}-browser-qa.json';q=read(p)
    assert q['status']=='passed' and q['url']=='http://192.168.1.229:8899' and q['mode']=='formal' and q['dataset']['corpus']==corpus and len(q['checks'])==34 and not q['errors'] and not q['requests_outside_service'] and 'asset_override' not in q
    reports[corpus]={'checks':34,'sha256':sha(p.read_bytes())}
for name,count in [('feature-production-qa.json',17),('method-production-qa.json',12)]:
    q=read(OUT/name);assert q['status']=='passed' and q['url']=='http://192.168.1.229:8899' and q['asset_override'] is False and len(q['checks'])==count and all(c['pass'] for c in q['checks'])
    reports[name]={'checks':count,'sha256':sha((OUT/name).read_bytes())}
assert read(OUT/'production-visual-review.json')['status']=='passed'
assets=read(OUT/'asset-hashes.json')
for name,expected in assets.items():assert sha(get('/' if name=='index.html' else '/static/'+name))==expected
health={}
for corpus,run in [('core','4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455'),('all','dd679f119b4d24a9d6725e79dec96af9d731265de59844ae04bbd028a719153e')]:
    meta=json.loads(get('/api/meta?corpus='+corpus));assert meta['ready'] and meta['max_series']==48 and len(meta['flag_codes'])==248
    assert [m['id'] for m in meta['methods']]==['spectral-v1','java-reflections-v1'] and meta['eci_default_method']=='spectral-v1'
    assert all(m['source_run_id']==run for m in meta['methods']);health[corpus]={'run_id':run,'selected_works':meta['selected_works'],'methods':meta['methods']}
candidate=promotion['candidate'];stop(candidate)
r1=read(OUT/'candidate-r1-identity.json');stop(r1)
assert process(963184) and '0.0.0.0:8900' in process(963184)['argv']
result={'status':'passed','completed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'url':'http://192.168.1.229:8899','production':identity,'binary_sha256':promotion['new_binary_sha256'],'source_archive':'source-v13-complexity-method-r2.tar.gz','source_sha256':sha((BASE/'source-v13-complexity-method-r2.tar.gz').read_bytes()),'datasets':health,'production_qa':reports,'assets_sha256':assets,'candidate_stopped':[candidate,r1],'old_core_reference_preserved':963184,'rollback_used':False,'behavior':{'max_curves':48,'tested_cartesian_curves':27,'eci_country_only':True,'pci_global_subject_only':True,'default_method':'spectral-v1','java_country_iterations':16,'java_discipline_iterations':17,'separate_method_orientation':True,'count_metrics_method_invariant':True,'local_svg_flags':248}}
(OUT/'final-acceptance.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({'status':'passed','pid':identity['pid'],'final_acceptance_sha256':sha((OUT/'final-acceptance.json').read_bytes())}))
