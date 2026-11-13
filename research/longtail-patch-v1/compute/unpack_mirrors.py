"""Verify transport and completion-owned bundle paths; unpack without overwrite."""
import hashlib
import argparse
import json
import tarfile
from pathlib import Path

parser=argparse.ArgumentParser();parser.add_argument('--version',choices=['v1','v2'],default='v1');args=parser.parse_args()
ROOT=Path(__file__).resolve().parents[3]
BASE=ROOT/('bin/longtail-profiles-'+args.version)
AUDIT=Path(__file__).with_name('compute-audit'+('-v2'if args.version=='v2'else'')+'.json')
report=json.loads(AUDIT.read_text())
assert report['status']=='passed_artifacts_baseline_exact_scores_and_independent_ranks'

def sha(path):
    h=hashlib.sha256()
    with path.open('rb')as stream:
        for b in iter(lambda:stream.read(8*1024*1024),b''):h.update(b)
    return h.hexdigest()

results=[]
for bundle in report['bundles']:
    archive=BASE/'transport'/(bundle['name']+'.tar.gz')
    assert archive.stat().st_size==bundle['archive_bytes']and sha(archive)==bundle['archive_sha256']
    with tarfile.open(archive,'r:gz')as tar:
        manifest=tar.extractfile('manifest.json').read()
        assert hashlib.sha256(manifest).hexdigest()==bundle['manifest_sha256']
        m=json.loads(manifest);expected={}
        for profile in m['profiles']:
            entries=[*profile['models'].values(),*profile['ranks'].values(),profile['diagnostics'],profile['baseline_comparisons']]
            for mapping in profile['downloads'].values():entries.extend(mapping.values())
            for a in entries:
                if a['path']in expected:assert expected[a['path']]==a
                expected[a['path']]=a
        expected['manifest.json']=dict(path='manifest.json',bytes=len(manifest),sha256=bundle['manifest_sha256'])
        members=tar.getmembers()
        assert len(members)==len(expected)and {x.name for x in members}==set(expected)
        out=BASE/bundle['name'];out.mkdir(exist_ok=False)
        for member in members:
            rel=Path(member.name)
            assert member.isfile()and not rel.is_absolute()and '..'not in rel.parts
            a=expected[member.name];assert member.size==a['bytes']
            path=out/rel;assert path.resolve().is_relative_to(out.resolve())
            path.parent.mkdir(parents=True,exist_ok=True)
            h=hashlib.sha256()
            with tar.extractfile(member)as stream,path.open('xb')as output:
                while b:=stream.read(8*1024*1024):h.update(b);output.write(b)
            assert h.hexdigest()==a['sha256'],path
    results.append(dict(name=bundle['name'],files=len(expected),manifest_sha256=bundle['manifest_sha256'],bytes=bundle['unpacked_bytes']))
    print(json.dumps(dict(bundle=bundle['name'],files=len(expected),status='mirror_verified')),flush=True)
Path(__file__).with_name('local-mirror-verification'+('-v2'if args.version=='v2'else'')+'.json').write_text(json.dumps(dict(status='passed',bundles=results),indent=2)+'\n')
