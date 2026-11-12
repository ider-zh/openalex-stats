"""Sequential isolated stages; waits never classify absent sources as negative."""
import pathlib,json,time,subprocess
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');m=b/'metadata'
def ready(p,status):
 try:return json.loads(p.read_text()).get('status')==status
 except (FileNotFoundError,json.JSONDecodeError):return False
while not (ready(m/'pubmed-pool-data/manifest.json','complete_frozen_baseline_source_preparation') and ready(m/'full-crossref-policy/manifest.json','frozen_policy_candidates_pending_independent_gate')):time.sleep(15)
assert json.loads((m/'pubmed-pool-data/manifest.json').read_text())['file_count']==1334
print('FULL_SOURCE_AND_FIRST_AUTHOR_VERSION_READY',flush=True)
def run(*args):subprocess.run(['python3','-u',*map(str,args)],check=True)
run(m/'prepare_matches.py','--mode','full','--input',b/'shared/identifiers/manifest.json','--out',m/'full-source-discovery')
run(m/'build_author_index.py','--targets',m/'full-source-discovery/author-targets.parquet',b/'fulltext/pmc-source-eligible.parquet','--out',b/'shared/author-identities-pubmed-complete','--reuse-manifest',b/'shared/author-identities/manifest.json')
run(m/'evaluate_matches.py','--matches',m/'full-source-discovery','--authors',b/'shared/author-identities-pubmed-complete/manifest.json','--out',m/'full-pubmed-policy','--routes','pubmed_baseline')
print('PUBMED_CANDIDATE_FRAME_READY_NOT_YET_GATED',flush=True)
