from pathlib import Path
import hashlib,json
base=Path('research/country-enrichment-v3/metadata');code=[]
for directory in ['core','policy-engine','crossref-pool','pubmed-pool','receive','work-index','raw-crossref','author-benchmark','decision-summary','title-diagnostic']:
 code.extend(p for p in (base/directory).glob('*.go'))
for name in ['build_work_index.py','build_author_index.py','prepare_matches.py','evaluate_matches.py','check_author_index.py','compare_index.py','compare_index_fixed.py','complete_pubmed_pipeline.py','personal_country_scope.py','test_personal_scope.py','build_crossref_blind_packets.py','build_pubmed_blind_packets.py','extract_selected_pubmed.py','lookup_pubmed_bibliography.py','audit_postal_markers.py','prepare_primary_links.py','explain_authors.py','profile_authors.py','additional_primary_evidence.mjs','pubmed-bridge.mjs','pubmed-curl-bridge.mjs','build_final_summary.py','make_public_files.py']:
 code.append(base/name)
configs=[base/n for n in ['country-aliases.json','source-v1.json','policy-v1.json','policy-v2.json','engine-freeze.json','personal-country-scope-v1.json','recovery-priority-v1.json','known-identity-conflicts-v1.json']]
summaries=[base/n for n in ['final-summary.json','pubmed-decision-summary.json','pubmed-title-diagnostic.json','execution-timing.json','pubmed-source-complete-manifest.json','pubmed-complete-author-manifest.json','pubmed-personal-scope-manifest.json','pubmed-personal-frozen-frame.json','index-go-rejected-benchmark.json']]
docs=[base/n for n in ['report.md','REPRODUCE.md','SOURCE-ATTRIBUTION.md']]
files=[]
for kind,paths in [('source',code),('frozen_config',configs),('pure_summary_no_raw_records',summaries),('documentation',docs)]:
 for p in sorted(paths):
  assert p.is_file(),p
  files.append(dict(path=p.as_posix(),kind=kind,bytes=p.stat().st_size,sha256=hashlib.sha256(p.read_bytes()).hexdigest()))
manifest=dict(status='explicit_publication_allowlist_only',metadata_stage_complete=True,external_actual_admitted_works=0,files=files,file_count=len(files),total_bytes=sum(r['bytes'] for r in files),append_allowlist_file_itself=True,excluded_always=['all dumps and source Parquet data','all candidate/frame member streams','all raw authors and affiliation assertions','all XML/PDF/HTML/source response bytes','all private inputs/outcomes and role-view inputs/outcomes','all blind packet/source byte directories','all Gold/individual reviews and unsanitized gate records','all original per-Work identifier/author/source evidence or postal marker raw text','all transfer receipt/progress detail directories and cached local bulk objects','ad hoc restart_index.py/schedule_index.py operational process-control helpers'],security_note='Do not git add the whole research directory. Add only these explicit paths and this manifest. Go dependencies are in root go.mod/go.sum. The optional original-machine proxy loader is not included; see REPRODUCE.md. No credentials are included.')
(base/'publicFiles.json').write_text(json.dumps(manifest,ensure_ascii=False,indent=2)+'\n',encoding='utf-8');print('PUBLIC',len(files),'files',manifest['total_bytes'],'bytes')
