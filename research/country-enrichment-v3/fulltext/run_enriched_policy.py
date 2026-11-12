"""Resume source-only validation after the complete shared authors receipt appears."""
import pathlib,json,time,subprocess,shutil
base=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');root=base/'fulltext';out=root/'enriched';dev=pathlib.Path('/home/ider/tmpdata/openalex-stats-dev-20261112')
stage=out/'pipeline-stage.json'
def update(status):stage.write_text(json.dumps(dict(status=status,utc=time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),production_applied=False),indent=2)+'\n');print(status,flush=True)
sample=json.loads((root/'validation-cohort-metadata.json').read_text());part_indices=set(w['source_index'] for w in sample['works'])
def cohort_parts_ready():
 return all((base/'shared/author-identities'/('part-%05d.parquet'%i)).exists() and (base/'shared/author-identities'/('part-%05d.json'%i)).exists() for i in part_indices)
update('waiting_for_all_exact_3000_cohort_source_parts_never_treat_unread_as_missing')
while not cohort_parts_ready():time.sleep(5)
update('exporting_original_author_inputs')
subprocess.run(['python',str(dev/'fulltext-v3-export-original-authors.py'),'--sample',str(root/'validation-cohort-metadata.json'),'--out',str(out/'original')],check=True)
shutil.copyfile(root/'frozen-policy.json',out/'frozen-policy.json')
update('applying_unchanged_frozen_country_identity_rules')
subprocess.run([str(dev/'country-fulltext-v3-frozen'),'-mode','apply-frozen-policy','-sample',str(root/'validation-cohort-metadata.json'),'-policy-input',str(out/'xml-results'),'-original-authors',str(out/'original/authors'),'-out',str(out)],check=True)
update('exporting_automatic_candidate_stream_for_independent_gate')
subprocess.run(['python',str(dev/'fulltext-v3-export-candidates.py'),'--sample',str(root/'validation-cohort-metadata.json'),'--results',str(out/'policy-results'),'--authors',str(out/'original/authors'),'--out',str(out/'export')],check=True)
update('candidate_stream_complete_not_a_passed_patch')
