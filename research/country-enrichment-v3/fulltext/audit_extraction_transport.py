import pathlib,json,collections
root=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112/fulltext/enriched')
rr=[json.loads(p.read_text()) for p in (root/'xml-results').glob('W*.json')]
uncached=[r for r in rr if not r.get('source',{}).get('cached')]
print(dict(total=len(rr),uncached_server_xml=len(uncached),uncached_server_statuses=dict(collections.Counter(r.get('source',{}).get('http_status') for r in uncached)),uncached_server_errors=sum(bool(r.get('source',{}).get('error')) for r in uncached),uncached_elapsed=sum(r.get('source',{}).get('seconds',0) for r in uncached)))
