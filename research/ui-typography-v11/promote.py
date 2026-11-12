"""Promote the verified typography-only dual-corpus service, preserving data."""
import datetime,fcntl,hashlib,json,os,signal,subprocess,sys,time,urllib.request
from pathlib import Path
BASE=Path('/home/ider/tmpdata/openalex-stats-dev-20261112')
OUT=BASE/'ui-typography-v11'
OLD=str(BASE/'oa-stats-v10')
NEW=str(BASE/'oa-stats-v11-typography-r2')
OLD_SHA='ac0efd9804294ed05c9f96d28a20b477d46d963e881edd836aff969e56b10fba'
NEW_SHA='9c0d2bd5b9a559a679a1b1a5ca3b6f8f64e48773c0eb5f4a8d7a4e612a26b00c'
TAIL=['serve','--data-core','/mnt/nvme0/public/openalex-stats/results/core-concept-hits-v1','--data-all','/mnt/nvme0/public/openalex-stats/results/all-concept-hits-v1','--listen']
RUNS={'core':'4b15b4dd0b68e80aeec24172418d55f7d20972b8ef6468a087bca0db7d4ab455','all':'dd679f119b4d24a9d6725e79dec96af9d731265de59844ae04bbd028a719153e'}
STATE=OUT/'promotion.json'
def read(p):return json.loads(Path(p).read_text())
def digest(p):return hashlib.sha256(Path(p).read_bytes()).hexdigest()
def process(pid):
    p=Path('/proc')/str(pid)
    try:
        stat=(p/'stat').read_text();fields=stat.rsplit(') ',1)[1].split()
        if fields[0]=='Z':return None
        return {'pid':pid,'exe':os.readlink(p/'exe'),'argv':[x.decode() for x in (p/'cmdline').read_bytes().split(b'\0') if x],'start_clock_ticks':int(fields[19]),'kernel_stat':stat}
    except FileNotFoundError:return None
def exact(identity):
    current=process(identity['pid'])
    assert current and all(current[k]==identity[k] for k in ['exe','argv','start_clock_ticks']),'project process identity changed'
    return current
def persist(state):
    state['updated_at_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat()
    temp=STATE.with_suffix('.tmp');temp.write_text(json.dumps(state,indent=2)+'\n');temp.replace(STATE)
def get(url):
    with urllib.request.urlopen(url,timeout=15) as response:return json.load(response)
def probe(port):
    base=f'http://127.0.0.1:{port}'
    cat=get(base+'/api/catalog');assert [x['id'] for x in cat['datasets']]==['core','all']
    runs={}
    for corpus,expected in RUNS.items():
        meta=get(base+'/api/meta?corpus='+corpus);run=get(base+'/download?file=run.json&corpus='+corpus)
        assert meta['ready'] and meta['corpus']==corpus and run['run_id']==expected
        runs[corpus]={'run_id':run['run_id'],'selected_works':meta['selected_works']}
    return {'catalog':cat,'runs':runs}
def stop(identity):
    exact(identity);os.kill(identity['pid'],signal.SIGTERM)
    for _ in range(150):
        current=process(identity['pid'])
        if current is None:return
        assert current['start_clock_ticks']==identity['start_clock_ticks'],'PID reused'
        time.sleep(.2)
    raise RuntimeError('exact service did not stop')
def launch(binary,logname):
    env=os.environ.copy();env['GOMAXPROCS']='4'
    argv=[binary,*TAIL,'0.0.0.0:8899']
    with (OUT/logname).open('ab',buffering=0) as log:
        child=subprocess.Popen(['nohup',*argv],cwd=BASE,stdin=subprocess.DEVNULL,stdout=log,stderr=subprocess.STDOUT,start_new_session=True,env=env)
    for _ in range(100):
        identity=process(child.pid)
        if identity and identity['exe']==binary and identity['argv']==argv:return child,identity
        if child.poll() is not None:raise RuntimeError('new service exited')
        time.sleep(.05)
    raise RuntimeError('cannot verify new process')
def wait_ready(child,identity):
    for _ in range(450):
        if child.poll() is not None:raise RuntimeError('new service exited '+str(child.returncode))
        exact(identity)
        try:return probe(8899)
        except Exception:time.sleep(2)
    raise RuntimeError('new service readiness timeout')
def restore(state):
    if state.get('new') and process(state['new']['pid']):stop(state['new'])
    child,identity=launch(OLD,'rollback-v10.log');state.update(status='rollback_loading',rollback=identity);persist(state)
    state.update(status='rollback_ready',rollback_health=wait_ready(child,identity));persist(state)
def main():
    assert digest(OLD)==OLD_SHA and digest(NEW)==NEW_SHA
    if sys.argv[1:]==['--rollback']:
        state=read(STATE);restore(state);return
    assert len(sys.argv)==1
    old=process(1031515)
    assert old and old['exe']==OLD and old['argv']==[OLD,*TAIL,'0.0.0.0:8899'] and old['start_clock_ticks']==5121163093
    candidate=process(1171297)
    assert candidate and candidate['exe']==NEW and candidate['argv']==[NEW,*TAIL,'0.0.0.0:8901'] and candidate['start_clock_ticks']==5121811773
    gates={}
    for corpus in ['core','all']:
        path=OUT/f'candidate-r2-{corpus}-browser-qa.json';q=read(path)
        assert q['status']=='passed' and q['mode']=='formal' and q['url']=='http://192.168.1.229:8901'
        assert q['dataset']['corpus']==corpus and len(q['checks'])==34 and not q['errors'] and not q['requests_outside_service'] and 'asset_override' not in q
        gates[corpus]={'path':str(path),'sha256':digest(path),'checks':34}
    visual=read(OUT/'typography-candidate-r2-qa.json')
    assert visual['status']=='passed' and visual['asset_override'] is False and len(visual['checks'])==2 and all(c['status']=='passed' for c in visual['checks'])
    review=read(OUT/'visual-review.json');assert review['status']=='passed' and review['revision']=='r2'
    state={'status':'prepared','old':old,'candidate':candidate,'new_binary_sha256':NEW_SHA,'old_binary_sha256':OLD_SHA,'browser_gates':gates,'visual_qa_sha256':digest(OUT/'typography-candidate-r2-qa.json'),'candidate_health':probe(8901),'old_health':probe(8899)}
    persist(state);stopped=False
    try:
        stop(old);stopped=True;state['status']='old_stopped';persist(state)
        child,identity=launch(NEW,'production.log');state.update(status='loading',new=identity);persist(state)
        health=wait_ready(child,identity);state.update(status='ready',health=health);persist(state)
        print(json.dumps(state),flush=True)
    except Exception as error:
        state.update(status='failed',error=str(error));persist(state)
        if stopped:restore(state)
        raise
if __name__=='__main__':
    with (BASE/'production-service-8899.lock').open('a') as lock:
        fcntl.flock(lock,fcntl.LOCK_EX|fcntl.LOCK_NB);main()
