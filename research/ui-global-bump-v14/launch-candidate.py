import hashlib,json,os,socket,subprocess,time
from pathlib import Path
base=Path('/home/ider/tmpdata/openalex-stats-dev-20261112');out=base/'ui-global-bump-v14';out.mkdir(exist_ok=True)
binary=base/'oa-stats-v14-global-bump-r1'
binary.chmod(0o755)
old=Path('/proc/1283477');assert os.readlink(old/'exe')==str(base/'oa-stats-v13-complexity-method-r2');assert (old/'stat').read_text().rsplit(') ',1)[1].split()[19]=='5122332324'
for port in [8902]:
 with socket.socket() as s:assert s.connect_ex(('127.0.0.1',port))!=0,'candidate port is occupied'
argv=[str(binary),'serve','--data-core','/mnt/nvme0/public/openalex-stats/results/core-concept-hits-v1','--data-all','/mnt/nvme0/public/openalex-stats/results/all-concept-hits-v1','--methods-core','/mnt/nvme0/public/openalex-stats/results/core-java-reflections-v1','--methods-all','/mnt/nvme0/public/openalex-stats/results/all-java-reflections-v1','--listen','0.0.0.0:8902']
env=os.environ.copy();env['GOMAXPROCS']='4'
with (out/'candidate-r1.log').open('ab',buffering=0) as log:p=subprocess.Popen(['nohup',*argv],stdin=subprocess.DEVNULL,stdout=log,stderr=subprocess.STDOUT,start_new_session=True,env=env,cwd=base)
for _ in range(100):
 proc=Path('/proc')/str(p.pid)
 try:matches=proc.exists() and os.readlink(proc/'exe')==str(binary)
 except FileNotFoundError:matches=False
 if matches:
  identity={'pid':p.pid,'exe':str(binary),'argv':argv,'start_clock_ticks':int((proc/'stat').read_text().rsplit(') ',1)[1].split()[19]),'binary_sha256':hashlib.sha256(binary.read_bytes()).hexdigest()};(out/'candidate-r1-identity.json').write_text(json.dumps(identity,indent=2)+'\n');print(json.dumps(identity));break
 if p.poll() is not None:raise RuntimeError('preview exited')
 time.sleep(.05)
else:raise RuntimeError('preview identity unavailable')
