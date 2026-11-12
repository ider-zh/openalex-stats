"""Extract selected raw PubMed XML using immutable cached source files only."""
import pathlib,json,argparse,collections,subprocess,concurrent.futures,hashlib,os,time
p=argparse.ArgumentParser();p.add_argument('--requests',required=True);p.add_argument('--out',required=True);p.add_argument('--shards',type=int,default=6);a=p.parse_args();assert 1<=a.shards<=6
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');md=b/'metadata';out=pathlib.Path(a.out);out.mkdir(parents=True,exist_ok=True);os.chmod(out,0o700)
source=json.loads((md/'pubmed-pool-data/manifest.json').read_text());assert source['file_count']==1334;by={r['Name']:r for r in source['files']};request=json.loads(pathlib.Path(a.requests).read_text());group=collections.defaultdict(set)
for r in request['records']:
 assert by[r['source_file']]['SourceSHA']==r['source_sha256'] and str(r['pmid']).isdigit();group[r['source_file']].add(str(r['pmid']))
batches=[[] for _ in range(a.shards)];weights=[0]*a.shards
for name in sorted(group,key=lambda n:pathlib.Path(by[n]['SourcePath']).stat().st_size,reverse=True):
 i=min(range(a.shards),key=lambda i:weights[i]);batches[i].append(dict(name=name,pmids=sorted(group[name])));weights[i]+=pathlib.Path(by[name]['SourcePath']).stat().st_size
binary=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-pilot-20261112/metadata/country-pilot-pubmed-raw-snippets');start=time.time()
def run(i):
 d=out/f'shard-{i:02d}';q=out/f'shard-{i:02d}-requests.json';q.write_text(json.dumps(dict(sources=batches[i]),indent=2)+'\n');env=dict(os.environ,GOMAXPROCS='8')
 with (out/f'shard-{i:02d}-log.txt').open('w') as log:subprocess.run([str(binary),str(md/'pubmed-source-links'),str(q),str(d)],stdout=log,stderr=subprocess.STDOUT,env=env,check=True)
 return d/'manifest.json'
with concurrent.futures.ThreadPoolExecutor(max_workers=a.shards) as ex:manifests=list(ex.map(run,range(a.shards)))
records=[]
for path in manifests:
 for r in json.loads(path.read_text())['records']:
  r=dict(r,source_compressed_sha256=by[r['source_file']]['SourceSHA'],source_compressed_path=by[r['source_file']]['SourcePath']);records.append(r)
assert len(records)==sum(len(x) for x in group.values())
sha=lambda x:hashlib.sha256(pathlib.Path(x).read_bytes()).hexdigest()
m=dict(status='independent_original_xml_bytes_extracted',record_count=len(records),source_files=len(group),records=records,source_manifest_sha256=sha(md/'pubmed-pool-data/manifest.json'),request_sha256=sha(a.requests),byte_splitter_sha256=sha(binary),byte_splitter_manifests=[dict(path=str(p),sha256=sha(p)) for p in manifests],concurrent_local_gzip_workers=a.shards*8,seconds=time.time()-start,selection='All independent validation-selected records, without country/identity filtering; source bytes and offsets preserved',per_work_api_calls=0,gold_generated=False)
(out/'manifest-v3.json').write_text(json.dumps(m,indent=2)+'\n');print(json.dumps({k:m[k] for k in ['status','record_count','source_files','seconds','concurrent_local_gzip_workers']}),flush=True)
