
SET threads=12;
SET memory_limit='32GB';
CREATE TEMP TABLE inst AS SELECT id, country_code FROM read_parquet('/mnt/hg02/openalex-snapshot/data/parquet/institutions/**/*.parquet') WHERE country_code IS NOT NULL AND country_code<>'';
CREATE TEMP TABLE sample AS
WITH base AS (
SELECT id, publication_year, is_xpac, type,
coalesce(list_distinct(list_filter(flatten(list_transform(authorships,a->a.countries)),c->c IS NOT NULL AND c<>'')),[]) AS countries,
coalesce(flatten(list_transform(authorships,a->a.institutions)),[]) AS institutions,
coalesce(flatten(list_transform(authorships,a->a.affiliations)),[]) AS affiliations,
coalesce(flatten(list_transform(authorships,a->a.raw_affiliation_strings)),[]) AS raw_affiliations
FROM read_parquet(['/mnt/hg02/openalex-snapshot/data/parquet/works/updated_date=2025-11-06/part_0070.parquet','/mnt/hg02/openalex-snapshot/data/parquet/works/updated_date=2025-11-06/part_0559.parquet','/mnt/hg02/openalex-snapshot/data/parquet/works/updated_date=2025-11-06/part_1048.parquet','/mnt/hg02/openalex-snapshot/data/parquet/works/updated_date=2026-03-20/part_0005.parquet','/mnt/hg02/openalex-snapshot/data/parquet/works/updated_date=2026-06-26/part_0159.parquet']) WHERE publication_year BETWEEN 2000 AND 2026 AND NOT coalesce(is_paratext,false)
AND coalesce(type,'') NOT IN ('editorial','erratum','retraction','paratext','peer-review','reference-entry','libguides','supplementary-materials','dataset','database','software','grant','standard','book','dissertation')
)
SELECT *, coalesce(list_distinct(list_filter(list_transform(institutions,i->i.country_code),c->c IS NOT NULL AND c<>'')),[]) AS institution_countries,
list_distinct(list_concat(list_transform(institutions,i->i.id),coalesce(flatten(list_transform(affiliations,a->a.institution_ids)),[]))) AS institution_ids FROM base;
CREATE TEMP TABLE resolved AS SELECT s.id, list_distinct(list(i.country_code)) AS looked_up_countries FROM sample s,unnest(s.institution_ids) ids(id) JOIN inst i ON ids.id=i.id GROUP BY s.id;
COPY (SELECT coalesce(is_xpac,false) AS is_xpac, count(*) AS eligible_sample_works,
count(*) FILTER(WHERE len(countries)=0) AS missing_country,
count(*) FILTER(WHERE len(countries)=0 AND len(institution_countries)>0) AS recoverable_embedded_country,
count(*) FILTER(WHERE len(countries)=0 AND len(r.looked_up_countries)>0) AS recoverable_institution_lookup,
count(*) FILTER(WHERE len(countries)=0 AND (len(institution_countries)>0 OR len(r.looked_up_countries)>0)) AS recoverable_either,
count(*) FILTER(WHERE len(countries)=0 AND (len(raw_affiliations)>0 OR len(affiliations)>0)) AS missing_with_raw_affiliation,
count(*) FILTER(WHERE len(countries)=0 AND len(institution_ids)>0) AS missing_with_institution_id
FROM sample s LEFT JOIN resolved r USING(id) GROUP BY ALL ORDER BY is_xpac) TO '/home/ider/tmpdata/openalex-stats-dev-20261112/country-coverage-probe/summary.json' (FORMAT JSON,ARRAY true);
COPY (SELECT s.id,s.publication_year,s.is_xpac,s.countries,s.institution_countries,r.looked_up_countries,s.institution_ids FROM sample s LEFT JOIN resolved r USING(id) WHERE len(countries)=0 AND (len(institution_countries)>0 OR len(r.looked_up_countries)>0) LIMIT 20) TO '/home/ider/tmpdata/openalex-stats-dev-20261112/country-coverage-probe/examples.json' (FORMAT JSON,ARRAY true);
