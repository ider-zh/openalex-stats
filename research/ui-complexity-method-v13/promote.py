"""Promote the verified dual-method viewer; counts and sidecars stay immutable."""
import datetime,fcntl,hashlib,json,os,signal,subprocess,sys,time,urllib.request
from pathlib import Path
BASE=Path('/home/ider/tmpdata/openalex-stats-dev-20261112');OUT=BASE/'ui-complexity-method-v13'
OLD=str(BASE/'oa-stats-v12-multidiscipline-r2');NEW=str(BASE/'oa-stats-v13-complexity-method-r2')
OLD_SHA='b08ecc8eeb1c058fe5974e1d3f37fb2312817017e5dc27f8b2f97624eeb05091';NEW_SHA='3406495a08ad76e8727ac080b196c0d9f13ab41a2c29d12eaf1df3ee2be28442'
DATA=['serve','--data-core','/mnt/nvme0/public/openalex-stats/results/core-concept-hits-v1','--data-all','/mnt/nvme0/public/openalex-stats/results/all-concept-hits-v1']
METHODS=['--methods-core','/mnt/nvme0/public/openalex-stats/results/core-java-reflections-v1','--methods-all','/mnt/nvme0/public/openalex-stats/results/all-java-reflections-v1']
RUNS={'core':'4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455','all':'dd679f119b4d24a9d6725e79dec96af9d731265de59844ae04bbd028a719153e'}
STATE=OUT/'promotion.json'
read=lambda p:json.loads(p.read_text())
digest=lambda p:hashlib.sha256(Path(p).read_bytes()).hexdigest()
def process(pid):
    p=Path('/proc')/str(pid)
    try:
        stat=(p/'stat').read_text();f=stat.rsplit(') ',1)[1].split()
        if f[0]=='Z':return None
        return {'pid':pid,'exe':os.readlink(p/'exe'),'argv':[x.decode() for x in (p/'cmdline').read_bytes().split(b'\0') if x],'start_clock_ticks':int(f[19])}
    except FileNotFoundError:return None
def exact(identity):
    current=process(identity['pid']);assert current and all(current[k]==identity[k] for k in ['exe','argv','start_clock_ticks']),'process identity changed';return current
def persist(state):
    state['updated_at_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();p=STATE.with_suffix('.tmp');p.write_text(json.dumps(state,indent=2)+'\n');p.replace(STATE)
def get(port,path):
    with urllib.request.urlopen(f'http://127.0.0.1:{port}'+path,timeout=30) as r:return json.load(r)
def probe(port,methods=True):
    catalog=get(port,'/api/catalog');assert [x['id'] for x in catalog['datasets']]==['core','all'];datasets={}
    for corpus,expected in RUNS.items():
        meta=get(port,'/api/meta?corpus='+corpus);run=get(port,'/download?file=run.json&corpus='+corpus);assert meta['ready'] and run['run_id']==expected
        if methods:
            assert [m['id'] for m in meta['methods']]==['spectral-v1','java-reflections-v1']
            assert meta['methods'][1]['country_iterations']==16 and meta['methods'][1]['discipline_iterations']==17
            assert all(m['source_run_id']==expected for m in meta['methods'])
        datasets[corpus]={'run_id':run['run_id'],'selected_works':meta['selected_works']}
    return {'catalog':catalog,'datasets':datasets}
def stop(identity):
    exact(identity);os.kill(identity['pid'],signal.SIGTERM)
    for _ in range(150):
        current=process(identity['pid'])
        if current is None:return
        assert current['start_clock_ticks']==identity['start_clock_ticks'];time.sleep(.2)
    raise RuntimeError('exact service did not stop')
def launch(binary,tail,logname):
    env=os.environ.copy();env['GOMAXPROCS']='4';argv=[binary,*tail,'--listen','0.0.0.0:8899']
    with (OUT/logname).open('ab',buffering=0) as log:child=subprocess.Popen(['nohup',*argv],cwd=BASE,stdin=subprocess.DEVNULL,stdout=log,stderr=subprocess.STDOUT,start_new_session=True,env=env)
    for _ in range(100):
        identity=process(child.pid)
        if identity and identity['exe']==binary and identity['argv']==argv:return child,identity
        if child.poll() is not None:raise RuntimeError('new service exited')
        time.sleep(.05)
    raise RuntimeError('new process identity unavailable')
def wait(child,identity,methods):
    for _ in range(450):
        if child.poll() is not None:raise RuntimeError('new service exited '+str(child.returncode))
        exact(identity)
        try:return probe(8899,methods)
        except Exception:time.sleep(2)
    raise RuntimeError('readiness timeout')
def restore(state):
    if state.get('new') and process(state['new']['pid']):stop(state['new'])
    child,identity=launch(OLD,DATA,'rollback-v12.log');state.update(status='rollback_loading',rollback=identity);persist(state);state.update(status='rollback_ready',rollback_health=wait(child,identity,False));persist(state)
def main():
    assert digest(OLD)==OLD_SHA and digest(NEW)==NEW_SHA
    if sys.argv[1:]==['--rollback']:restore(read(STATE));return
    assert len(sys.argv)==1
    old=process(1224966);assert old and old['exe']==OLD and old['argv']==[OLD,*DATA,'--listen','0.0.0.0:8899'] and old['start_clock_ticks']==5122089525
    candidate=process(1277003);assert candidate and candidate['exe']==NEW and candidate['argv']==[NEW,*DATA,*METHODS,'--listen','0.0.0.0:8902']
    gates={}
    for corpus in ['core','all']:
        path=OUT/f'candidate-r2-{corpus}-browser-qa.json';q=read(path);assert q['status']=='passed' and q['mode']=='formal' and q['url']=='http://192.168.1.229:8902' and q['dataset']['corpus']==corpus and len(q['checks'])==34 and not q['errors'] and not q['requests_outside_service'] and 'asset_override' not in q
        gates[corpus]={'path':str(path),'sha256':digest(path),'checks':34}
    for name,count in [('feature-candidate-r2-qa.json',17),('method-candidate-r2-qa.json',12)]:
        q=read(OUT/name);assert q['status']=='passed' and q['url']=='http://192.168.1.229:8902' and q['asset_override'] is False and len(q['checks'])==count and all(c['pass'] for c in q['checks']);gates[name]={'sha256':digest(OUT/name),'checks':count}
    review=read(OUT/'visual-review.json');assert review['status']=='passed' and review['revision']=='r2'
    state={'status':'prepared','old':old,'candidate':candidate,'new_binary_sha256':NEW_SHA,'old_binary_sha256':OLD_SHA,'gates':gates,'candidate_health':probe(8902),'old_health':probe(8899,False)};persist(state);stopped=False
    try:
        stop(old);stopped=True;state['status']='old_stopped';persist(state);child,identity=launch(NEW,[*DATA,*METHODS],'production.log');state.update(status='loading',new=identity);persist(state);health=wait(child,identity,True);state.update(status='ready',health=health);persist(state);print(json.dumps({'status':'ready','new':identity,'health':health}),flush=True)
    except Exception as error:
        state.update(status='failed',error=str(error));persist(state)
        if stopped:restore(state)
        raise
if __name__=='__main__':
    with (BASE/'production-service-8899.lock').open('a') as lock:fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB);main()
