"""Prediction-free original XML + original Work packets for PubMed validation."""
import pathlib,json,argparse,shutil,hashlib,os
p=argparse.ArgumentParser();p.add_argument('--members',required=True);p.add_argument('--raw-manifest',required=True);p.add_argument('--out',required=True);a=p.parse_args();out=pathlib.Path(a.out);out.mkdir(parents=True,exist_ok=True);os.chmod(out,0o700)
raw=json.loads(pathlib.Path(a.raw_manifest).read_text());by={(r['source_file'],r['pmid']):r for r in raw['records']};packets=[]
def sha(path):return hashlib.sha256(pathlib.Path(path).read_bytes()).hexdigest()
def copy(src,dest,expected):
 assert sha(src)==expected,'Raw source packet hash changed';dest.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(src,dest);os.chmod(dest,0o600);assert sha(dest)==expected;return dict(path=str(dest),sha256=expected)
for line in pathlib.Path(a.members).open():
 r=json.loads(line);wid=r['id'];d=out/'works'/('W'+str(wid));d.mkdir(parents=True,exist_ok=True);os.chmod(d,0o700)
 packet=dict(id=wid,original_work=dict(title=r['title'],year=r['year'],doi=r['doi'],pmid=r['pmid'],authors=[{k:x.get(k,'') for k in ['name','raw_author_name','orcid','raw_orcid']} for x in r['original_authors']]),original_pubmed_source_records=[])
 for er in r['evidence_records']:
  e=er['evidence'];source=json.loads(e['private_record_json']);s=by[e['source_file'],source['PMID']];assert s['source_compressed_sha256']==e['source_sha256']
  original=copy(s['private_server_path'],d/'xml'/(source['PMID']+'.raw.xml'),s['raw_record_sha256']);review=copy(s['redacted_path'],d/'xml'/(source['PMID']+'.redacted.xml'),s['redacted_record_sha256'])
  packet['original_pubmed_source_records'].append(dict(source_file=s['source_file'],source_compressed_sha256=s['source_compressed_sha256'],record_ordinal=e['record_ordinal'],source_pmid=source['PMID'],source_doi=source.get('DOI',''),uncompressed_byte_start=s['uncompressed_byte_start'],uncompressed_byte_end=s['uncompressed_byte_end'],original_file=original,redacted_review_file=review))
 path=d/'packet.json';path.write_text(json.dumps(packet,ensure_ascii=False,indent=2)+'\n');os.chmod(path,0o600);packets.append(dict(id=wid,path=str(path),sha256=sha(path),source_records=len(packet['original_pubmed_source_records'])))
m=dict(status='prediction_free_independent_review_packets',work_count=len(packets),source_records=sum(r['source_records'] for r in packets),selected_member_stream_sha256=sha(a.members),source_byte_manifest_sha256=sha(a.raw_manifest),packets=packets,removed_derived_fields=['proposed_countries','eligible','engine Countries','identity reasons','country_support_roles','scope_status','collective_only_quarantine_countries'],original_source_country_content_retained=True,gold_generated=False)
(out/'manifest.json').write_text(json.dumps(m,indent=2)+'\n');print({k:m[k] for k in ['status','work_count','source_records']})
