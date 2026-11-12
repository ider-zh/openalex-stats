import hashlib,json,os,signal,subprocess,time
from pathlib import Path
base=Path('/home/ider/tmpdata/openalex-stats-dev-20261112')
evidence=base/'ui-typography-v11'
old=Path('/proc/1156056')
assert os.readlink(old/'exe')==str(base/'oa-stats-v11-typography')
assert int((old/'stat').read_text().rsplit(') ',1)[1].split()[19])==5121776040
args=[x.decode() for x in (old/'cmdline').read_bytes().split(b'\0') if x]
assert args[-2:]==['--listen','0.0.0.0:8901']
assert hashlib.sha256((base/'oa-stats-v11-typography').read_bytes()).hexdigest()=='37627165cb1df80b3302e9e3212a95da8cfe8141d3c426cd48ae8de2ba597d67'
os.kill(1156056,signal.SIGTERM)
for _ in range(100):
    if not old.exists() or (old/'stat').read_text().rsplit(') ',1)[1].split()[0]=='Z':break
    time.sleep(.1)
else:raise RuntimeError('candidate did not stop')
new=base/'oa-stats-v11-typography-r2'
new.chmod(0o755)
env=os.environ.copy();env['GOMAXPROCS']='4'
with (evidence/'candidate-r2.log').open('ab',buffering=0) as log:
    child=subprocess.Popen(['nohup',str(new),*args[1:]],cwd=base,stdin=subprocess.DEVNULL,stdout=log,stderr=subprocess.STDOUT,start_new_session=True,env=env)
time.sleep(.2)
p=Path('/proc')/str(child.pid)
record={'pid':child.pid,'argv':[str(new),*args[1:]],'kernel_stat':(p/'stat').read_text(),'sha256':hashlib.sha256(new.read_bytes()).hexdigest()}
(evidence/'candidate-r2-identity.json').write_text(json.dumps(record,indent=2))
print(json.dumps(record))
