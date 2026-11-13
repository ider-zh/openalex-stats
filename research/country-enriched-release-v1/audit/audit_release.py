"""Read-only result admission audit and annual country coverage summary.

Run on 229 with DuckDB. Writes only a new audit folder; never edits results.
"""
import csv
import hashlib
import json
import sys
from pathlib import Path

import duckdb

OUT = Path(sys.argv[1])
OUT.mkdir(parents=True, exist_ok=False)
BASE = Path('/mnt/nvme0/public/openalex-stats/results')
q = duckdb.connect()
q.execute('set threads=8')
q.execute("set memory_limit='12GB'")

def digest(p):
    h = hashlib.sha256()
    with p.open('rb') as stream:
        for b in iter(lambda: stream.read(8 * 1024 * 1024), b''):
            h.update(b)
    return h.hexdigest()

def read(p):
    return json.loads(p.read_text())

def check_artifacts(root, completion, methods=False):
    records = list(completion['artifacts'])
    if not methods:
        records.append(completion['run'])
    checked = []
    for a in records:
        p = root / Path(a['path']).name
        assert p.stat().st_size == a['bytes'], p
        assert digest(p) == a['sha256'], p
        checked.append(dict(filename=p.name, bytes=a['bytes'], sha256=a['sha256']))
    return checked

def integer(value):
    assert abs(value - round(value)) < .001, value
    return round(value)

manifest = dict(version='country-enriched-release-v1', status='auditing',
                preferred_dataset='country-enriched-v3-r3',
                patch_sha256='fe6d3abaf6d94e5ca3c209c6a5e5270f7c8c85964dc34ac3d3ce9e6c662db5f3',
                external_admitted_works=0, originals_modified=False,
                source_scope='Strict text only; original UNKNOWN works only; no repair or expansion of originally known country sets.',
                datasets=[])
annual = []
for corpus in ['core', 'all']:
    variants = [
        ('original', corpus + '-concept-hits-v1', corpus + '-java-reflections-v1'),
        ('country-enriched-v3-r3', corpus + '-country-validated-v3-r3-20261113', corpus + '-country-java-v3-r3-20261113'),
    ]
    annual_by_version = {}
    for version, basename, methodname in variants:
        root, methodroot = BASE / basename, BASE / methodname
        completion, run = read(root/'completion.json'), read(root/'run.json')
        mc, mm = read(methodroot/'completion.json'), read(methodroot/'manifest.json')
        assert run['run_id'] == completion['run_id'] == mm['base_run_id'] == mc['base_run_id']
        assert run['status'] == 'full_snapshot_complete'
        assert mm['corpus'] == corpus
        assert digest(root/'completion.json') == mm['source_completion_sha256']
        assert digest(methodroot/'manifest.json') == mc['manifest_sha256']
        artifacts = check_artifacts(root, completion)
        method_artifacts = check_artifacts(methodroot, mc, True)
        scenarios = read(root/'scenarios.json')
        assert len(scenarios) == 6
        coverage = read(root/'coverage.json')
        assert len(coverage) == 24
        dictionary = read(root/'dictionary.json')
        score_checks = []
        for method_id, sr in [('spectral-v1',root),('java-reflections-v1',methodroot)]:
            for filename in ['country_eci.parquet','discipline_pci.parquet']:
                p = str(sr/filename)
                n = q.execute('select count(*) from (select distinct scenario_id,year,scheme,level,metric from read_parquet(?))',[p]).fetchone()[0]
                assert n == 216, (p,n)
                score_checks.append(dict(method=method_id,file=filename,models=n))
        statfile = str(root/'statistics.parquet')
        rows = q.execute("""select year,sum(publications),sum(citations),
          coalesce(sum(publications) filter(where country='UNKNOWN'),0),
          coalesce(sum(citations) filter(where country='UNKNOWN'),0)
          from read_parquet(?) where scenario_id='fractional-concept-whole-0.3'
          and scheme='topics' and level='field' group by year order by year""",[statfile]).fetchall()
        assert [r[0] for r in rows] == list(range(2000,2027))
        ar = []
        for y,p,c,up,uc in rows:
            p,c,up,uc = map(integer,[p,c,up,uc])
            ar.append(dict(corpus=corpus,dataset=version,year=y,partial_year=y==2026,
                           publication_works=p,known_country_publications=p-up,missing_country_publications=up,
                           publication_country_coverage=(p-up)/p if p else None,
                           citation_events=c,known_country_citations=c-uc,missing_country_citations=uc,
                           citation_country_coverage=(c-uc)/c if c else None))
        annual.extend(ar)
        annual_by_version[version] = ar
        entry = dict(corpus=corpus,dataset=version,base_directory=str(root),methods_directory=str(methodroot),
                     run_id=run['run_id'],completion_sha256=digest(root/'completion.json'),
                     method_completion_sha256=digest(methodroot/'completion.json'),
                     method_manifest_sha256=digest(methodroot/'manifest.json'),
                     selected_works=run['selected_works'],quality=run['quality'],
                     scenarios=scenarios,known_dictionary_countries=len(dictionary['countries']),
                     dictionary_sha256=digest(root/'dictionary.json'),
                     artifacts=artifacts,method_artifacts=method_artifacts,score_checks=score_checks)
        if version != 'original':
            entry['enrichment'] = read(root/'enrichment.json')
        manifest['datasets'].append(entry)
        print(corpus,version,'checksums and 216-model score schemas passed',flush=True)
    for before,after in zip(annual_by_version['original'],annual_by_version['country-enriched-v3-r3']):
        assert before['publication_works'] == after['publication_works']
        assert before['citation_events'] == after['citation_events']
        assert before['known_country_publications'] <= after['known_country_publications']
        assert before['known_country_citations'] <= after['known_country_citations']
manifest['status'] = 'passed_result_checksums_method_binding_score_schemas_and_annual_coverage'
manifest['notes'] = ['No scientific recomputation performed; full r3 recomputation already independently validated.',
 'Coverage is at least one observed country, not full authorship-country completeness or correctness.',
 'Frozen route source-supported precision255/256 is not precision of all original country tags.',
 'Counts from conserved fractional primary-Field totals include UNKNOWN subjects; integer rounding residual <0.001.',
 'Metadata 2026 is partial year; citation coverage follows citing year and target-country attribution.',
 'Spectral Concepts0 citations cross-version sign reversal retained; no unannounced reorientation.',
 'Historical production_applied=false in immutable computation manifests is provenance, not current deployment status.']
(OUT/'manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
(OUT/'annual-country-coverage.json').write_text(json.dumps(annual,indent=2)+'\n')
with (OUT/'annual-country-coverage.csv').open('w',newline='') as stream:
    writer=csv.DictWriter(stream,fieldnames=list(annual[0]));writer.writeheader();writer.writerows(annual)
files=[dict(path=p.name,bytes=p.stat().st_size,sha256=digest(p)) for p in sorted(OUT.iterdir()) if p.is_file()]
(OUT/'audit-files.json').write_text(json.dumps(files,indent=2)+'\n')
print(json.dumps(dict(status=manifest['status'],datasets=len(manifest['datasets']),annual_rows=len(annual))))
