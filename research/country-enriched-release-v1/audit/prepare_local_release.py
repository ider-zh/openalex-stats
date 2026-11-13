"""Unpack only checksummed completion-owned files and prepare release entries.

No GitHub API or credentials. The original results are never overwritten.
"""
import hashlib
import json
import tarfile
from datetime import datetime, timezone
from pathlib import Path

ROOT = Path(__file__).resolve().parents[3]
MIRROR = ROOT / 'bin/country-enriched-r3-results'
TRANSPORT = MIRROR / 'transport'
AUDIT = Path(__file__).resolve().parent
source = json.loads((TRANSPORT/'release-source-manifest.json').read_text())
verified = json.loads((AUDIT/'manifest.json').read_text())
tag = 'snapshot-20260626-country-enriched-r3'
urlbase = 'https://github.com/ider-zh/openalex-stats/releases/download/' + tag

def digest(p):
    h = hashlib.sha256()
    with p.open('rb') as stream:
        for b in iter(lambda: stream.read(8*1024*1024), b''):
            h.update(b)
    return h.hexdigest()

upload, public_assets = [], []
for group in source['groups']:
    label = group['corpus'] + '-' + group['group']
    packed = TRANSPORT/(label+'.tar.gz')
    assert packed.stat().st_size == group['archive_bytes']
    assert digest(packed) == group['archive_sha256']
    destination = MIRROR/label
    destination.mkdir(exist_ok=False)
    expected = {a['filename']: a for a in group['files']}
    seen = set()
    with tarfile.open(packed, 'r:gz') as archive:
        for member in archive:
            assert member.isfile() and member.name in expected and member.name not in seen
            assert Path(member.name).name == member.name
            assert member.size == expected[member.name]['bytes']
            path = destination/member.name
            stream = archive.extractfile(member)
            h = hashlib.sha256()
            with path.open('xb') as output:
                while b := stream.read(8*1024*1024):
                    h.update(b)
                    output.write(b)
            assert h.hexdigest() == expected[member.name]['sha256']
            seen.add(member.name)
    assert seen == set(expected)
    for filename, artifact in sorted(expected.items()):
        prefix = group['corpus'] + ('-methods-' if group['group']=='methods' else '-')
        name = prefix + filename
        assert artifact['bytes'] < 2*1024**3
        upload.append(dict(name=name,source=str((destination/filename).resolve()),bytes=artifact['bytes'],sha256=artifact['sha256']))
        public_assets.append(dict(name=name,filename=filename,corpus=group['corpus'],group=group['group'],bytes=artifact['bytes'],sha256=artifact['sha256'],url=urlbase+'/'+name))
    print(json.dumps(dict(group=label,files=len(seen),status='unpacked_and_sha256_verified')),flush=True)

datasets={}
for item in verified['datasets']:
    if item['dataset'] != 'country-enriched-v3-r3':
        continue
    corpus=item['corpus']
    mm=json.loads((MIRROR/(corpus+'-methods')/'manifest.json').read_text())
    methods=mm['methods']
    for method in methods:
        if method['id']=='java-reflections-v1':method['display_name']='小倪方法论'
    datasets[corpus]=dict(run_id=item['run_id'],completion_sha256=item['completion_sha256'],selected_works=item['selected_works'],publication_works=item['quality']['publication_works'],retained_edges=item['quality']['citation_events'],methods=methods,method_completion_sha256=item['method_completion_sha256'],country_coverage_summary='research/country-enriched-release-v1/audit/release-summary.json')
index=dict(version=1,status='prepared',snapshot_date='2026-06-26',dataset_version='country-enriched-v3-r3',release_tag=tag,release_url='https://github.com/ider-zh/openalex-stats/releases/tag/'+tag,datasets=datasets,assets=sorted(public_assets,key=lambda a:a['name']),verified_at_utc=datetime.now(timezone.utc).isoformat(),scope_notes=['Strict-text UNKNOWN-only country enrichment; original nonempty country sets unchanged.','External Crossref/PubMed/PMC/PDF actual admissions0.','Prepared local bytes verified; publication status must be updated only after GitHub asset verification.'])
(AUDIT/'release-upload.json').write_text(json.dumps(upload,indent=2)+'\n',encoding='utf8')
(ROOT/'results/country-enriched-v3-r3-index.json').write_text(json.dumps(index,ensure_ascii=False,indent=2)+'\n',encoding='utf8')
print(json.dumps(dict(status='prepared_not_uploaded',assets=len(upload),bytes=sum(a['bytes'] for a in upload),index='results/country-enriched-v3-r3-index.json')))
