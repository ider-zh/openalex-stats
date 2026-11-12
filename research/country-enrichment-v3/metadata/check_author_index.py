"""Cross-reader author projection check, not an admission-policy test."""
import pathlib,json,duckdb
b=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');m=json.loads((b/'shared/author-identities/manifest.json').read_text());sample=json.loads((b/'validation/author-requests-authors.json').read_text())['works'];original={r['id']:r for r in sample};c=duckdb.connect();c.read_parquet([r['path'] for r in m['parts']]).create_view('authors');rows=c.execute('select id,authors,authors_loaded,source_id_year_type_xpac_verified from authors where id in (select unnest(?))',[list(original)]).fetchall();diff=[]
for wid,authors,loaded,verified in rows:
 expected=[{k:a.get(k,'') for k in ['name','raw_author_name','orcid','raw_orcid']} for a in original[wid]['authors']]
 if authors!=expected or not loaded or not verified:diff.append(wid)
r=dict(status='passed' if len(rows)==len(original) and not diff else 'failed',independent_reader_rows=len(original),projected_rows=len(rows),author_field_difference_ids=diff,comparison='DuckDB full original list projection vs safe sparse Go original-author helper, exact list/order/name/raw_author_name/orcid/raw_orcid',policy_changed=False)
(b/'metadata/author-index-cross-reader-check.json').write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r));assert r['status']=='passed'
