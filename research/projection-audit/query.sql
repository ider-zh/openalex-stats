SET threads=2;
SET memory_limit='2GB';
SET preserve_insertion_order=false;
SET temp_directory='/mnt/nvme0/public/openalex-stats/audits/projection-raw-cache-v1/temp';
CREATE MACRO wid(value) AS CASE WHEN regexp_full_match(value, '(https://openalex[.]org/)?W[1-9][0-9]*') THEN try_cast(regexp_extract(value, 'W([1-9][0-9]*)$', 1) AS UBIGINT) ELSE NULL END;
CREATE MACRO valid_concept(c) AS c IS NOT NULL AND c.level IS NOT NULL AND c.id IS NOT NULL AND c.id<>'' AND c.score IS NOT NULL AND isfinite(c.score);
CREATE MACRO country_set(xs) AS list_sort(list_distinct(list_filter(xs, x -> x IS NOT NULL AND x<>'')));
CREATE MACRO trimmed_countries(xs) AS country_set(list_transform(xs, x -> trim(x)));
CREATE VIEW raw AS SELECT 'part-0' AS audit_part, id, publication_year, type, is_xpac, is_paratext, is_retracted, authorships, primary_topic, concepts, referenced_works FROM read_parquet('/mnt/hg02/openalex-snapshot/data/parquet/works/updated_date=2016-06-24/part_0000.parquet') UNION ALL SELECT 'part-1' AS audit_part, id, publication_year, type, is_xpac, is_paratext, is_retracted, authorships, primary_topic, concepts, referenced_works FROM read_parquet('/mnt/hg02/openalex-snapshot/data/parquet/works/updated_date=2026-06-26/part_0006.parquet');
CREATE VIEW normalized AS SELECT 'part-0' AS audit_part, * FROM read_parquet('/mnt/nvme0/public/openalex-stats/cache-20260626/part-000000-7ffc4f9448ec4a7ca73f3a8d.parquet') UNION ALL SELECT 'part-1' AS audit_part, * FROM read_parquet('/mnt/nvme0/public/openalex-stats/cache-20260626/part-002292-2625f0ab09d52f895760f5b4.parquet');
CREATE TABLE expected AS
SELECT audit_part, wid(id) AS id, coalesce(publication_year,0) AS year, coalesce(type,'') AS type,
coalesce(is_xpac,false) AS is_xpac, coalesce(is_paratext,false) AS is_paratext, coalesce(is_retracted,false) AS is_retracted,
country_set(flatten(list_transform(coalesce(authorships,[]), a -> coalesce(a.countries,[])))) AS countries,
coalesce(primary_topic.field.id,'') AS field_id, coalesce(primary_topic.subfield.id,'') AS subfield_id,
list_sort(list_transform(list_filter(coalesce(concepts,[]), c -> valid_concept(c) AND c.level=0), c -> struct_pack(id:=c.id,score:=CAST(c.score AS DOUBLE)))) AS concepts0,
list_sort(list_transform(list_filter(coalesce(concepts,[]), c -> valid_concept(c) AND c.level=1), c -> struct_pack(id:=c.id,score:=CAST(c.score AS DOUBLE)))) AS concepts1,
list_filter(list_transform(coalesce(referenced_works,[]), x -> wid(x)), x -> x IS NOT NULL AND x>0) AS refs,
len(coalesce(referenced_works,[]))-len(list_filter(list_transform(coalesce(referenced_works,[]), x -> wid(x)), x -> x IS NOT NULL AND x>0)) AS quality_invalid_references,
len(coalesce(concepts,[]))-len(list_filter(coalesce(concepts,[]), c -> valid_concept(c))) AS quality_invalid_concepts,
publication_year IS NULL AS quality_missing_year, is_xpac IS NULL AS quality_null_xpac
FROM raw;
CREATE TABLE actual AS SELECT audit_part,id,year,type,is_xpac,is_paratext,is_retracted,
country_set(coalesce(countries,[])) AS countries,coalesce(field_id,'') AS field_id,coalesce(subfield_id,'') AS subfield_id,
list_sort(list_transform(range(1,len(coalesce(concept_ids0,[]))+1), i -> struct_pack(id:=concept_ids0[i],score:=concept_scores0[i]))) AS concepts0,
list_sort(list_transform(range(1,len(coalesce(concept_ids1,[]))+1), i -> struct_pack(id:=concept_ids1[i],score:=concept_scores1[i]))) AS concepts1,
len(coalesce(concept_ids0,[]))=len(coalesce(concept_scores0,[])) AS concept0_lengths_valid,
len(coalesce(concept_ids1,[]))=len(coalesce(concept_scores1,[])) AS concept1_lengths_valid,
coalesce("references",[]) AS refs,quality_invalid_references,quality_invalid_concepts,quality_missing_year,quality_null_xpac
FROM normalized;
CREATE TABLE compared AS SELECT coalesce(r.audit_part,n.audit_part) AS audit_part,coalesce(r.id,n.id) AS id,
r.audit_part IS NULL AS missing_raw,n.audit_part IS NULL AS missing_cache,
r.year IS DISTINCT FROM n.year AS year_diff,r.type IS DISTINCT FROM n.type AS type_diff,
r.is_xpac IS DISTINCT FROM n.is_xpac AS xpac_diff,r.is_paratext IS DISTINCT FROM n.is_paratext AS paratext_diff,r.is_retracted IS DISTINCT FROM n.is_retracted AS retracted_diff,
r.countries IS DISTINCT FROM n.countries AS countries_diff,trimmed_countries(r.countries) IS DISTINCT FROM trimmed_countries(n.countries) AS trimmed_countries_diff,
r.field_id IS DISTINCT FROM n.field_id AS field_diff,r.subfield_id IS DISTINCT FROM n.subfield_id AS subfield_diff,
r.concepts0 IS DISTINCT FROM n.concepts0 AS concept0_pairs_diff,r.concepts1 IS DISTINCT FROM n.concepts1 AS concept1_pairs_diff,
n.concept0_lengths_valid IS NOT TRUE AS concept0_length_diff,n.concept1_lengths_valid IS NOT TRUE AS concept1_length_diff,
r.refs IS DISTINCT FROM n.refs AS reference_sequence_diff,list_sort(list_distinct(r.refs)) IS DISTINCT FROM list_sort(list_distinct(n.refs)) AS reference_set_diff,
r.quality_invalid_references IS DISTINCT FROM n.quality_invalid_references AS invalid_reference_quality_diff,
r.quality_invalid_concepts IS DISTINCT FROM n.quality_invalid_concepts AS invalid_concept_quality_diff,
r.quality_missing_year IS DISTINCT FROM n.quality_missing_year AS missing_year_quality_diff,r.quality_null_xpac IS DISTINCT FROM n.quality_null_xpac AS null_xpac_quality_diff
FROM expected r FULL OUTER JOIN actual n ON r.audit_part=n.audit_part AND r.id=n.id;
CREATE TABLE expected_max AS SELECT audit_part,id AS work_id,0 AS concept_level,c.id AS concept_id,max(c.score) AS max_score FROM expected CROSS JOIN UNNEST(concepts0) AS items(c) GROUP BY audit_part,work_id,concept_id UNION ALL SELECT audit_part,id AS work_id,1 AS concept_level,c.id AS concept_id,max(c.score) AS max_score FROM expected CROSS JOIN UNNEST(concepts1) AS items(c) GROUP BY audit_part,work_id,concept_id;
CREATE TABLE actual_max AS SELECT audit_part,id AS work_id,0 AS concept_level,c.id AS concept_id,max(c.score) AS max_score FROM actual CROSS JOIN UNNEST(concepts0) AS items(c) GROUP BY audit_part,work_id,concept_id UNION ALL SELECT audit_part,id AS work_id,1 AS concept_level,c.id AS concept_id,max(c.score) AS max_score FROM actual CROSS JOIN UNNEST(concepts1) AS items(c) GROUP BY audit_part,work_id,concept_id;
CREATE TABLE max_diffs AS SELECT coalesce(r.audit_part,n.audit_part) AS audit_part,coalesce(r.work_id,n.work_id) AS work_id,coalesce(r.concept_level,n.concept_level) AS concept_level FROM expected_max r FULL OUTER JOIN actual_max n ON r.audit_part=n.audit_part AND r.work_id=n.work_id AND r.concept_level=n.concept_level AND r.concept_id=n.concept_id WHERE r.max_score IS DISTINCT FROM n.max_score;
COPY (SELECT t.*,coalesce(m.concept0_max_diff,0)::BIGINT AS concept0_max_diff,coalesce(m.concept1_max_diff,0)::BIGINT AS concept1_max_diff FROM (SELECT audit_part,count(*)::BIGINT AS rows,count(*) FILTER (WHERE missing_raw)::BIGINT AS missing_raw,count(*) FILTER (WHERE missing_cache)::BIGINT AS missing_cache,count(*) FILTER (WHERE year_diff)::BIGINT AS year_diff,count(*) FILTER (WHERE type_diff)::BIGINT AS type_diff,count(*) FILTER (WHERE xpac_diff)::BIGINT AS xpac_diff,count(*) FILTER (WHERE paratext_diff)::BIGINT AS paratext_diff,count(*) FILTER (WHERE retracted_diff)::BIGINT AS retracted_diff,count(*) FILTER (WHERE countries_diff)::BIGINT AS countries_diff,count(*) FILTER (WHERE trimmed_countries_diff)::BIGINT AS trimmed_countries_diff,count(*) FILTER (WHERE field_diff)::BIGINT AS field_diff,count(*) FILTER (WHERE subfield_diff)::BIGINT AS subfield_diff,count(*) FILTER (WHERE concept0_pairs_diff)::BIGINT AS concept0_pairs_diff,count(*) FILTER (WHERE concept1_pairs_diff)::BIGINT AS concept1_pairs_diff,count(*) FILTER (WHERE concept0_length_diff)::BIGINT AS concept0_length_diff,count(*) FILTER (WHERE concept1_length_diff)::BIGINT AS concept1_length_diff,count(*) FILTER (WHERE reference_sequence_diff)::BIGINT AS reference_sequence_diff,count(*) FILTER (WHERE reference_set_diff)::BIGINT AS reference_set_diff,count(*) FILTER (WHERE invalid_reference_quality_diff)::BIGINT AS invalid_reference_quality_diff,count(*) FILTER (WHERE invalid_concept_quality_diff)::BIGINT AS invalid_concept_quality_diff,count(*) FILTER (WHERE missing_year_quality_diff)::BIGINT AS missing_year_quality_diff,count(*) FILTER (WHERE null_xpac_quality_diff)::BIGINT AS null_xpac_quality_diff FROM compared GROUP BY audit_part) t LEFT JOIN (SELECT audit_part,count(DISTINCT work_id) FILTER (WHERE concept_level=0) AS concept0_max_diff,count(DISTINCT work_id) FILTER (WHERE concept_level=1) AS concept1_max_diff FROM max_diffs GROUP BY audit_part) m USING(audit_part) ORDER BY t.audit_part) TO '/mnt/nvme0/public/openalex-stats/audits/projection-raw-cache-v1/comparison.json' (FORMAT JSON,ARRAY true);
COPY (SELECT * FROM compared WHERE missing_raw OR missing_cache OR year_diff OR type_diff OR xpac_diff OR paratext_diff OR retracted_diff OR countries_diff OR trimmed_countries_diff OR field_diff OR subfield_diff OR concept0_pairs_diff OR concept1_pairs_diff OR concept0_length_diff OR concept1_length_diff OR reference_sequence_diff OR reference_set_diff OR invalid_reference_quality_diff OR invalid_concept_quality_diff OR missing_year_quality_diff OR null_xpac_quality_diff LIMIT 20) TO '/mnt/nvme0/public/openalex-stats/audits/projection-raw-cache-v1/mismatch-examples.json' (FORMAT JSON,ARRAY true);
COPY (SELECT audit_part,'raw' AS source,count(*)::BIGINT AS rows,count(DISTINCT id)::BIGINT AS unique_ids,count(*) FILTER (WHERE id IS NULL OR id=0)::BIGINT AS invalid_ids FROM expected GROUP BY audit_part UNION ALL SELECT audit_part,'cache' AS source,count(*)::BIGINT AS rows,count(DISTINCT id)::BIGINT AS unique_ids,count(*) FILTER (WHERE id IS NULL OR id=0)::BIGINT AS invalid_ids FROM actual GROUP BY audit_part) TO '/mnt/nvme0/public/openalex-stats/audits/projection-raw-cache-v1/inventory.json' (FORMAT JSON,ARRAY true);
