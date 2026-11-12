"""Mechanical prediction-free Crossref source packets for independent reviewers."""
import pathlib,json,hashlib,os,shutil,collections
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');md=b/'metadata';req=json.loads((b/'validation/crossref-selected-raw-line-requests.json').read_text());out=md/'crossref-blind-packets';out.mkdir(exist_ok=True);os.chmod(out,0o700)
selected=collections.defaultdict(list)
for r in req['work_domains']:selected[r['id']].append(dict(domain=r['domain'],cluster_id=r['cluster_id']))
assert len(selected)==256
raw=json.loads((md/'crossref-selected-raw/manifest.json').read_text());lineby={r['source_line']:r for r in raw['records']};xml=json.loads((md/'crossref-selected-pubmed-raw/manifest-v3.json').read_text());xmlby={r['pmid']:r for r in xml['records']};bio=collections.defaultdict(list)
for line in (md/'crossref-selected-pubmed-bibliography/bibliography.jsonl').read_text().splitlines():
 r=json.loads(line);bio[r['doi']].append(r)
def sha(p):return hashlib.sha256(pathlib.Path(p).read_bytes()).hexdigest()
def copy(src,dest):
 dest.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(src,dest);os.chmod(dest,0o600);return dict(path=str(dest),sha256=sha(dest))
packets=[];found=set()
for line in (md/'full-crossref-policy/crossref-validation-candidates.jsonl').open():
 r=json.loads(line);wid=r['id']
 if wid not in selected:continue
 found.add(wid);root=out/'works'/('W'+str(wid));root.mkdir(parents=True,exist_ok=True);os.chmod(root,0o700)
 p=dict(id=wid,sample_domains=selected[wid],original_work=dict(title=r['title'],year=r['year'],doi=r['doi'],pmid=r['pmid'],authors=[{k:a.get(k,'') for k in ['name','raw_author_name','orcid','raw_orcid']} for a in r['original_authors']]),crossref_original_assertions=[],independent_pubmed_doi_matches=[])
 for er in r['evidence_records']:
  e=er['evidence'];source=lineby[e['source_line']];assert e['source_line_sha256']==source['source_line_sha256']
  original=copy(source['private_raw_path'],root/'crossref'/('line-'+str(e['source_line'])+'.raw.json'));review=copy(source['redacted_path'],root/'crossref'/('line-'+str(e['source_line'])+'.redacted.json'))
  p['crossref_original_assertions'].append(dict(source_file=raw['source_file'],source_sha256=raw['source_sha256'],source_line=e['source_line'],source_trimmed_line_sha256=e['source_line_sha256'],original_source_json=json.loads(pathlib.Path(source['private_raw_path']).read_text()),original_file=original,redacted_review_file=review))
 for m in bio[r['doi']]:
  x=xmlby[m['pmid']];original=copy(x['private_server_path'],root/'pubmed'/(m['pmid']+'.raw.xml'));review=copy(x['redacted_path'],root/'pubmed'/(m['pmid']+'.redacted.xml'))
  p['independent_pubmed_doi_matches'].append(dict(bibliography=m,source_compressed_sha256=x['source_compressed_sha256'],uncompressed_byte_start=x['uncompressed_byte_start'],uncompressed_byte_end=x['uncompressed_byte_end'],raw_xml_sha256=x['raw_record_sha256'],original_file=original,redacted_review_file=review,link_basis='Exact DOI only; no identity or country correctness label'))
 path=root/'packet.json';path.write_text(json.dumps(p,ensure_ascii=False,indent=2)+'\n');os.chmod(path,0o600);packets.append(dict(id=wid,path=str(path),sha256=sha(path),pubmed_records=len(p['independent_pubmed_doi_matches']),assertion_records=len(p['crossref_original_assertions'])))
assert found==set(selected)
m=dict(status='prediction_free_independent_review_packets',work_count=len(packets),source_assertions=sum(r['assertion_records'] for r in packets),pubmed_xml_records=sum(r['pubmed_records'] for r in packets),selected_requests_sha256=sha(b/'validation/crossref-selected-raw-line-requests.json'),packets=packets,removed_derived_fields=['proposed_countries','eligible','engine Countries','identity reasons','country_support_roles'],original_source_country_content_retained=True,gold_generated=False)
(out/'manifest.json').write_text(json.dumps(m,indent=2)+'\n');print({k:m[k] for k in ['status','work_count','source_assertions','pubmed_xml_records']})
