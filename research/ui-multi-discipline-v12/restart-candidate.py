import hashlib,json,os,signal,subprocess,time
from pathlib import Path
base=Path('/home/ider/tmpdata/openalex-stats-dev-20261112');out=base/'ui-multi-discipline-v12'
old=base/'oa-stats-v12-multidiscipline-r1';new=base/'oa-stats-v12-multidiscipline-r2'
sha=lambda p:hashlib.sha256(p.read_bytes()).hexdigest()
assert sha(old)=='a68f21cd0646b8d287958684e437730ae4a35ef5f1daeec89c15be3314e7b592'
assert sha(new)=='b08ecc8eeb1c058fe5974e1d3f37fb2312817017e5dc27f8b2f97624eeb05091'
def process(pid):
    p=Path('/proc')/str(pid)
    try:
        stat=(p/'stat').read_text();parts=stat.rsplit(') ',1)[1].split()
        if parts[0]=='Z':return None
        return {'pid':pid,'exe':os.readlink(p/'exe'),'argv':[x.decode() for x in (p/'cmdline').read_bytes().split(b'\0') if x],'start_clock_ticks':int(parts[19])}
    except FileNotFoundError:return None
tail=['serve','--data-core','/mnt/nvme0/public/openalex-stats/results/core-concept-hits-v1','--data-all','/mnt/nvme0/public/openalex-stats/results/all-concept-hits-v1','--listen','0.0.0.0:8901']
identity=process(1209990)
assert identity and identity['exe']==str(old) and identity['argv']==[str(old),*tail]
assert process(identity['pid'])==identity
os.kill(identity['pid'],signal.SIGTERM)
for _ in range(150):
    if process(identity['pid']) is None:break
    assert process(identity['pid'])['start_clock_ticks']==identity['start_clock_ticks'];time.sleep(.2)
else:raise RuntimeError('exact old candidate did not stop')
new.chmod(0o755);env=os.environ.copy();env['GOMAXPROCS']='4'
with (out/'candidate-r2.log').open('ab',buffering=0) as log:
    child=subprocess.Popen([str(new),*tail],cwd=base,stdin=subprocess.DEVNULL,stdout=log,stderr=subprocess.STDOUT,start_new_session=True,env=env)
time.sleep(.2);record={'old':identity,'new':process(child.pid),'binary_sha256':sha(new),'source_sha256':sha(base/'source-v12-multidiscipline-r2.tar.gz')}
(out/'candidate-r2-identity.json').write_text(json.dumps(record,indent=2));print(json.dumps(record))
