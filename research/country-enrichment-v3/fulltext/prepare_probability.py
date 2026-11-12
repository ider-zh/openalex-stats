import json,pathlib
root=pathlib.Path('/mnt/nvme0/public/openalex-stats/analysis-country-enrichment-v3-20261112');out=root/'fulltext/probability';out.mkdir(parents=True,exist_ok=True)
s=json.load(open(root/'validation/sample-metadata.json'));s['source_metadata_path']=str(root/'validation/sample-metadata.json');s['pmc_discovery_scope']='DOI/PMID identifier-only to match planned full rollout; original locations retained in original metadata for PDF'
for w in s['works']:w['locations']=[];w['pmcid']=''
(out/'sample-xml-identifier-only.json').write_text(json.dumps(s,ensure_ascii=False))
