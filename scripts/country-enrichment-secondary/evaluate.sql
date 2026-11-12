-- Independent batch analysis of review-only secondary candidates.
-- Substitute @WORK@ (frozen v1), @SECONDARY@, @OUT@, @TEMP@.
SET threads=32;
SET memory_limit='256GB';
SET temp_directory='@TEMP@';
CREATE TEMP TABLE anchors AS
 SELECT id,year,is_xpac,country,raw_normalized_sha256 AS h,raw_affiliation AS raw,single_institution_group AS institution_group
 FROM read_parquet('@WORK@/anchors/*.parquet');
CREATE TEMP TABLE dictionary AS
 SELECT h,min(country) AS country,count(DISTINCT country) AS countries,count(DISTINCT id) AS work_support,
 count(DISTINCT nullif(institution_group,'')) AS institution_groups,
 count(*) FILTER(WHERE institution_group='') AS no_single_group_anchors,
 min(institution_group) AS example_institution_group,min(id) AS example_work_1,max(id) AS example_work_2,
 min(raw) AS example_raw,
 len(min(raw))>=30 AND contains(min(raw),',') AND regexp_matches(lower(min(raw)),'universit|universid|universida|institut|hospital|college|academy|academia|centre|center|laborator|research|school|大学|研究|医院') AS address_specificity
 FROM anchors GROUP BY h;
COPY (SELECT count(*) AS raw_string_keys,
 count(*) FILTER(WHERE countries=1) AS country_unique_keys,
 count(*) FILTER(WHERE countries>1) AS country_conflicting_keys,
 count(*) FILTER(WHERE countries=1 AND work_support>=5 AND address_specificity) AS country_unique_support5_address_keys,
 count(*) FILTER(WHERE countries=1 AND work_support>=5 AND address_specificity AND institution_groups=1 AND no_single_group_anchors=0) AS strict_single_institution_support5_keys
 FROM dictionary) TO '@OUT@/dictionary-inventory.json' (FORMAT JSON,ARRAY true);
COPY (SELECT * FROM dictionary WHERE countries=1 AND work_support>=5 AND address_specificity) TO '@OUT@/country-unique-dictionary.parquet' (FORMAT PARQUET,COMPRESSION ZSTD);
CREATE TEMP TABLE texts AS SELECT * FROM read_parquet('@SECONDARY@/texts/*.parquet');
COPY (SELECT t.is_xpac,threshold,
 count(DISTINCT t.id) AS country_unique_address_candidates,
 count(DISTINCT t.id) FILTER(WHERE d.institution_groups=1 AND d.no_single_group_anchors=0) AS strict_single_institution_candidates
 FROM texts t JOIN dictionary d ON t.raw_normalized_sha256=d.h CROSS JOIN (VALUES(2),(3),(5),(10)) thresholds(threshold)
 WHERE t.year BETWEEN 2000 AND 2026 AND d.countries=1 AND d.address_specificity AND d.work_support>=threshold
 GROUP BY ALL ORDER BY t.is_xpac,threshold) TO '@OUT@/dictionary-thresholds.json' (FORMAT JSON,ARRAY true);

-- Same Work never enters training and testing. This measures consistency with
-- existing labels, not human-reviewed truth or unseen-institution precision.
CREATE TEMP TABLE test_targets AS
 SELECT id,h,min(country) AS country,min(institution_group) AS institution_group
 FROM anchors GROUP BY id,h HAVING count(DISTINCT country)=1 AND count(DISTINCT institution_group)=1;
CREATE TEMP TABLE work_train AS
 SELECT h,min(country) AS country,count(DISTINCT country) AS countries,count(DISTINCT id) AS support
 FROM anchors WHERE hash(id)%5<>0 GROUP BY h;
COPY (SELECT count(*) AS held_out_work_string_targets,
 count(*) FILTER(WHERE tr.countries=1 AND tr.support>=5) AS matched_targets,
 count(*) FILTER(WHERE tr.countries=1 AND tr.support>=5 AND tr.country=t.country) AS same_country_targets,
 count(DISTINCT t.id) AS held_out_works
 FROM test_targets t LEFT JOIN work_train tr USING(h) WHERE hash(t.id)%5=0)
 TO '@OUT@/holdout-work.json' (FORMAT JSON,ARRAY true);
CREATE TEMP TABLE institution_train AS
 SELECT h,min(country) AS country,count(DISTINCT country) AS countries,count(DISTINCT id) AS support,
 count(DISTINCT institution_group) AS institution_groups
 FROM anchors WHERE institution_group<>'' AND hash(institution_group)%5<>0 GROUP BY h;
COPY (SELECT count(*) AS held_out_institution_string_targets,
 count(*) FILTER(WHERE tr.countries=1 AND tr.support>=5) AS matched_targets,
 count(*) FILTER(WHERE tr.countries=1 AND tr.support>=5 AND tr.country=t.country) AS same_country_targets,
 count(DISTINCT t.institution_group) AS held_out_institution_ids,
 (SELECT count(DISTINCT institution_group) FROM anchors WHERE institution_group<>'' AND hash(institution_group)%5<>0) AS training_institution_ids
 FROM test_targets t LEFT JOIN institution_train tr USING(h)
 WHERE t.institution_group<>'' AND hash(t.institution_group)%5=0)
 TO '@OUT@/holdout-institution.json' (FORMAT JSON,ARRAY true);

CREATE TEMP TABLE unresolved AS SELECT * FROM read_parquet('@WORK@/unresolved/*.parquet');
CREATE TEMP TABLE geo_hits AS
 SELECT u.id,u.year,u.type,u.is_xpac,list_distinct(flatten(list(g.country_candidates))) AS countries,
 list(DISTINCT struct_pack(institution_id:=g.institution_id,country_name:=g.geo_country_name,countries:=g.country_candidates,city:=g.city,mapping_source:=g.mapping_source)) AS evidence
 FROM unresolved u,unnest(u.institution_ids) ids(institution_id)
 JOIN read_parquet('@SECONDARY@/geo-institution-candidates.parquet') g ON ids.institution_id=g.institution_id
 GROUP BY u.id,u.year,u.type,u.is_xpac;
CREATE TEMP TABLE dictionary_hits AS
 SELECT t.id,t.year,t.type,t.is_xpac,list_distinct(list(d.country)) AS countries,
 list(DISTINCT struct_pack(country:=d.country,raw:=t.raw_affiliation,raw_sha256:=d.h,work_support:=d.work_support,institution_groups:=d.institution_groups,example_institution:=d.example_institution_group,example_work_1:=d.example_work_1,example_work_2:=d.example_work_2)) AS evidence
 FROM texts t JOIN dictionary d ON t.raw_normalized_sha256=d.h
 WHERE d.countries=1 AND d.work_support>=5 AND d.address_specificity AND d.institution_groups=1 AND d.no_single_group_anchors=0
 GROUP BY t.id,t.year,t.type,t.is_xpac;
CREATE TEMP TABLE secondary AS
 SELECT u.id,u.year,u.type,u.is_xpac,
 coalesce(len(g.countries)>0,false) AS has_geo,
 coalesce(len(d.countries)>0,false) AS has_strict_dictionary,
 list_sort(list_distinct(list_concat(coalesce(g.countries,[]),coalesce(d.countries,[])))) AS country_candidates,
 coalesce(g.countries,[]) AS typed_geo_countries,
 coalesce(d.countries,[]) AS exact_dictionary_countries,
 g.evidence AS geo_evidence,d.evidence AS dictionary_evidence,
 u.raw_affiliations,u.institution_ids,
 coalesce(g.countries,[])<>coalesce(d.countries,[]) AND len(coalesce(g.countries,[]))>0 AND len(coalesce(d.countries,[]))>0 AS differing_source_country_sets,
 true AS review_only,'secondary-review-v1b' AS rule_version
 FROM unresolved u LEFT JOIN geo_hits g USING(id) LEFT JOIN dictionary_hits d USING(id)
 WHERE len(coalesce(g.countries,[]))>0 OR len(coalesce(d.countries,[]))>0;
COPY (SELECT * FROM secondary) TO '@OUT@/secondary-patches.parquet' (FORMAT PARQUET,COMPRESSION ZSTD);
COPY (SELECT is_xpac,count(*) AS work_candidates,count(*) FILTER(WHERE has_geo) AS typed_geo_name_candidates,
 count(*) FILTER(WHERE has_strict_dictionary) AS exact_dictionary_candidates,
 count(*) FILTER(WHERE has_geo AND has_strict_dictionary) AS overlap,
 count(*) FILTER(WHERE differing_source_country_sets) AS differing_source_country_sets,
 count(*) FILTER(WHERE len(country_candidates)>1) AS multi_country_candidate_sets
 FROM secondary WHERE year BETWEEN 2000 AND 2026 GROUP BY ALL ORDER BY is_xpac)
 TO '@OUT@/secondary-output-potential.json' (FORMAT JSON,ARRAY true);
COPY (SELECT is_xpac,year,count(*) AS work_candidates,count(*) FILTER(WHERE has_geo) AS typed_geo_name_candidates,
 count(*) FILTER(WHERE has_strict_dictionary) AS exact_dictionary_candidates,
 count(*) FILTER(WHERE has_geo AND has_strict_dictionary) AS overlap FROM secondary GROUP BY ALL ORDER BY is_xpac,year)
 TO '@OUT@/secondary-year-potential.json' (FORMAT JSON,ARRAY true);
COPY (SELECT * EXCLUDE(n) FROM (SELECT *,row_number() OVER(PARTITION BY is_xpac,has_geo,has_strict_dictionary,differing_source_country_sets ORDER BY hash(id)) AS n FROM secondary) WHERE n<=10)
 TO '@OUT@/secondary-review-samples.json' (FORMAT JSON,ARRAY true);
COPY (SELECT count(*) AS secondary_rows,count(DISTINCT s.id) AS unique_ids,
 count(*) FILTER(WHERE p.newly_known) AS overlap_with_v1_newly_known,
 count(*) FILTER(WHERE len(s.country_candidates)=0 OR NOT s.review_only) AS invalid_secondary_rows
 FROM secondary s LEFT JOIN read_parquet('@WORK@/patch/*.parquet') p USING(id))
 TO '@OUT@/secondary-invariants.json' (FORMAT JSON,ARRAY true);
