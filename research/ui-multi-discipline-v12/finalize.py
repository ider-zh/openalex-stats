import datetime,hashlib,json,os,signal,time,urllib.request
from pathlib import Path
base=Path('/home/ider/tmpdata/openalex-stats-dev-20261112');out=base/'ui-multi-discipline-v12'
read=lambda p:json.loads(p.read_text())
sha=lambda b:hashlib.sha256(b).hexdigest()
def get(path):
    with urllib.request.urlopen('http://127.0.0.1:8899'+path,timeout=30) as r:return r.read()
def process(pid):
    p=Path('/proc')/str(pid)
    try:
        stat=(p/'stat').read_text();parts=stat.rsplit(') ',1)[1].split()
        if parts[0]=='Z':return None
        return {'pid':pid,'exe':os.readlink(p/'exe'),'argv':[x.decode() for x in (p/'cmdline').read_bytes().split(b'\0') if x],'start_clock_ticks':int(parts[19])}
    except FileNotFoundError:return None
promotion=read(out/'promotion.json');assert promotion['status']=='ready'
identity=process(promotion['new']['pid']);assert identity and all(identity[k]==promotion['new'][k] for k in ['exe','argv','start_clock_ticks'])
reports=[]
for corpus in ['core','all']:
    p=out/f'production-{corpus}-browser-qa.json';q=read(p)
    assert q['status']=='passed' and q['mode']=='formal' and q['dataset']['corpus']==corpus and len(q['checks'])==34 and not q['errors'] and not q['requests_outside_service'] and 'asset_override' not in q
    reports.append({'corpus':corpus,'checks':len(q['checks']),'sha256':sha(p.read_bytes())})
feature=read(out/'feature-production-qa.json');assert feature['status']=='passed' and len(feature['checks'])==17 and feature['asset_override'] is False
served={}
for name,expected in read(out/'asset-hashes.json').items():
    b=get('/' if name=='index.html' else '/static/'+name);assert sha(b)==expected;served[name]=expected
health={}
for corpus,run_id in [('core','4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455'),('all','dd679f119b4d24a9d6725e79dec96af9d731265de59844ae04bbd028a719153e')]:
    meta=json.loads(get('/api/meta?corpus='+corpus));run=json.loads(get('/download?file=run.json&corpus='+corpus));assert run['run_id']==run_id and meta['max_series']==48 and len(meta['flag_codes'])==248
    health[corpus]={'run_id':run_id,'selected_works':meta['selected_works']}
result={'status':'passed','completed_at_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'url':'http://192.168.1.229:8899','production':identity,'binary_sha256':sha(Path(identity['exe']).read_bytes()),'source_archive':'source-v12-multidiscipline-r2.tar.gz','source_sha256':sha((base/'source-v12-multidiscipline-r2.tar.gz').read_bytes()),'datasets':health,'production_browser_qa':reports,'feature_checks':17,'feature_qa_sha256':sha((out/'feature-production-qa.json').read_bytes()),'assets_sha256':served,'flags':{'project':'lipis/flag-icons','version':'7.6.0','license':'MIT','embedded':248,'runtime_external_requests':0},'behavior':{'cartesian_counts':True,'max_curves':48,'eci_country_only':True,'pci_global_subject_only':True,'default_country_count':9,'tested_country_subject_combinations':27},'candidate_stopped':False,'rollback_used':False}
candidate=process(promotion['candidate']['pid'])
if candidate:
    assert all(candidate[k]==promotion['candidate'][k] for k in ['exe','argv','start_clock_ticks']);os.kill(candidate['pid'],signal.SIGTERM)
    for _ in range(150):
        if process(candidate['pid']) is None:break
        time.sleep(.2)
    else:raise RuntimeError('candidate did not stop')
result['candidate_stopped']=True
(out/'final-acceptance.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps({'status':'passed','pid':identity['pid'],'binary_sha256':result['binary_sha256'],'final_acceptance_sha256':sha((out/'final-acceptance.json').read_bytes())}))
