"""Checkpoint one fully bridged batch through the unchanged Go policy engine."""
import pathlib,json,hashlib,argparse,subprocess,shutil,time
p=argparse.ArgumentParser();p.add_argument('--out',required=True);p.add_argument('--sample',required=True);p.add_argument('--plan',required=True);p.add_argument('--archive',required=True);p.add_argument('--archive-sha256',required=True);a=p.parse_args()
base=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');root=base/'fulltext';out=pathlib.Path(a.out);out.mkdir(parents=True,exist_ok=True);dev=pathlib.Path('/home/ider/tmpdata/openalex-stats-dev-20261112')
expected='09312f219af3980328199a6cad05386a71ad8b957e505cb847ae6eefae1c28a1';assert hashlib.sha256((root/'frozen-policy.json').read_bytes()).hexdigest()==expected
start=time.time();sample=json.load(open(a.sample));n=len(sample['works'])
def stage(s):
 (out/'pipeline-stage.json').write_text(json.dumps(dict(status=s,works=n,utc=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),production_applied=False),indent=2)+'\n');print(s,flush=True)
def run(args,log):
 with open(out/log,'ab',buffering=0) as f:subprocess.run(args,stdout=f,stderr=subprocess.STDOUT,check=True)
stage('verifying_bridge_and_fixed_source_availability')
run(['python',str(dev/'fulltext-v3-import-pmc-cache.py'),'--archive',a.archive,'--archive-sha256',a.archive_sha256,'--sample',a.sample,'--plan',a.plan,'--out',a.out],'bridge.log')
stage('exporting_complete_original_author_inputs')
run(['python',str(dev/'fulltext-v3-export-original-authors.py'),'--sample',a.sample,'--out',str(out/'original')],'original-authors.log')
stage('go_cached_xml_extraction_no_additional_source_lookup')
run([str(dev/'country-fulltext-v3-frozen'),'-mode','xml','-sample',a.sample,'-plan',str(out/'execution-plan.json'),'-dictionary',str(root/'dictionary.json'),'-out',a.out,'-workers','24','-result-subdir','xml-results'],'xml-extraction.log')
stage('verifying_source_evidence_integrity')
run(['python',str(dev/'fulltext-v3-verify-extraction-sources.py'),'--results',str(out/'xml-results'),'--out',str(out/'source-evidence-integrity.json')],'source-integrity.log')
integrity=json.loads((out/'source-evidence-integrity.json').read_text());assert integrity['outcomes']==n
shutil.copyfile(root/'frozen-policy.json',out/'frozen-policy.json')
stage('applying_unchanged_frozen_identity_and_country_policy')
run([str(dev/'country-fulltext-v3-frozen'),'-mode','apply-frozen-policy','-sample',a.sample,'-policy-input',str(out/'xml-results'),'-original-authors',str(out/'original/authors'),'-out',a.out],'frozen-policy.log')
stage('exporting_complete_outcomes_and_automatic_candidates_not_application')
run(['python',str(dev/'fulltext-v3-export-candidates.py'),'--sample',a.sample,'--results',str(out/'policy-results'),'--authors',str(out/'original/authors'),'--out',str(out/'export')],'export.log')
accept=json.loads((out/'export/export-acceptance.json').read_text());assert accept['total_operational_outcomes']==n
receipt=dict(status='completed_fixed_batch_review_candidates_not_applied',works=n,automatic_candidates=accept['automatic_positive_candidates'],candidate_stream_sha256=accept['candidate_stream_sha256'],original_plan_sha256=hashlib.sha256(pathlib.Path(a.plan).read_bytes()).hexdigest(),sample_file_sha256=hashlib.sha256(pathlib.Path(a.sample).read_bytes()).hexdigest(),rule_sha256=expected,archive_sha256=a.archive_sha256,elapsed_seconds=time.time()-start,production_applied=False)
(out/'batch-completion.json').write_text(json.dumps(receipt,indent=2)+'\n');stage(receipt['status']);print(json.dumps(receipt),flush=True)
