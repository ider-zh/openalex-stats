import json,os
from pathlib import Path
out=Path('/home/ider/tmpdata/openalex-stats-dev-20261112/ui-complexity-method-v13')
for pid in [1249585,963184]:
 p=Path('/proc')/str(pid);f=(p/'stat').read_text().rsplit(') ',1)[1].split();d={'pid':pid,'exe':os.readlink(p/'exe'),'argv':[x.decode() for x in (p/'cmdline').read_bytes().split(b'\0') if x],'start_clock_ticks':int(f[19])};print(json.dumps(d))
 if pid==1249585:(out/'candidate-r1-identity.json').write_text(json.dumps(d,indent=2)+'\n')
