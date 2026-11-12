-- Replace @WORK@, @CACHE@, @OUT@ and @TEMP@ with isolated absolute paths.
-- DuckDB is an independent validator, not the production enrichment engine.
SET threads=16;
SET memory_limit='128GB';
SET temp_directory='@TEMP@';
CREATE TEMP TABLE patches AS SELECT * FROM read_parquet('@WORK@/patch/*.parquet');
COPY (SELECT *,
 CASE WHEN NOT was_unknown AND len(added_countries)>0 THEN 'existing_country_set_extension'
 WHEN newly_known AND conflict THEN 'mixed_evidence_review_only'
 WHEN newly_known AND len(institution_added)>0 AND len(text_added)>0 THEN 'institution_and_text_candidate'
 WHEN newly_known AND len(institution_added)>0 THEN 'institution_record_candidate'
 WHEN newly_known THEN 'explicit_country_text_candidate'
 ELSE 'conflict_or_ambiguity_review_only' END AS confidence_class,
 true AS requires_user_review,false AS automatic_application
 FROM patches) TO '@OUT@/review-candidate-patches.parquet' (FORMAT PARQUET,COMPRESSION ZSTD);
COPY (
 SELECT count(*) AS patch_rows,count(DISTINCT id) AS unique_work_ids,
 count(*) FILTER(WHERE NOT list_has_all(conservative_countries,original_countries)) AS original_country_removed,
 count(*) FILTER(WHERE NOT list_has_all(candidate_countries,original_countries)) AS original_country_removed_from_candidates,
 count(*) FILTER(WHERE list_has_any(added_countries,original_countries)) AS added_country_already_original,
 count(*) FILTER(WHERE list_sort(list_distinct(list_concat(original_countries,added_countries)))<>conservative_countries) AS invalid_conservative_union,
 count(*) FILTER(WHERE list_sort(list_distinct(list_concat(institution_added,text_added)))<>added_countries) AS invalid_source_union,
 count(*) FILTER(WHERE was_unknown<>(len(original_countries)=0)) AS invalid_unknown_flag,
 count(*) FILTER(WHERE newly_known<>(len(original_countries)=0 AND len(added_countries)>0)) AS invalid_newly_known_flag,
 count(*) FILTER(WHERE NOT list_has_all(candidate_countries,conservative_countries)) AS conservative_not_in_candidates
 FROM patches
) TO '@OUT@/patch-invariants.json' (FORMAT JSON,ARRAY true);
COPY (
 SELECT p.is_xpac, count(*) AS patch_rows,
 count(*) FILTER(WHERE p.newly_known) AS newly_known,
 count(*) FILTER(WHERE p.newly_known AND len(p.institution_added)>0) AS newly_known_institution,
 count(*) FILTER(WHERE p.newly_known AND len(p.text_added)>0) AS newly_known_text,
 count(*) FILTER(WHERE p.newly_known AND len(p.institution_added)>0 AND len(p.text_added)>0) AS newly_known_both,
 count(*) FILTER(WHERE NOT p.was_unknown AND len(p.added_countries)>0) AS already_known_extended,
 count(*) FILTER(WHERE p.was_unknown AND NOT p.newly_known AND len(p.review_only_countries)>0) AS unknown_review_only,
 count(*) FILTER(WHERE p.conflict) AS conflict_works,
 count(*) FILTER(WHERE p.newly_known AND NOT p.conflict) AS newly_known_without_work_conflict
 FROM patches p WHERE p.year BETWEEN 2000 AND 2026 GROUP BY ALL ORDER BY is_xpac
) TO '@OUT@/patch-output-counts.json' (FORMAT JSON,ARRAY true);
COPY (
 SELECT p.is_xpac,p.year,count(*) FILTER(WHERE p.newly_known) AS newly_known,
 count(*) FILTER(WHERE p.newly_known AND len(p.institution_added)>0) AS institution,
 count(*) FILTER(WHERE p.newly_known AND len(p.text_added)>0) AS text,
 count(*) FILTER(WHERE p.newly_known AND len(p.institution_added)>0 AND len(p.text_added)>0) AS both,
 count(*) FILTER(WHERE NOT p.was_unknown AND len(p.added_countries)>0) AS known_extensions
 FROM patches p GROUP BY ALL ORDER BY is_xpac,year
) TO '@OUT@/patch-year-counts.json' (FORMAT JSON,ARRAY true);
CREATE TEMP TABLE cache_patch_check AS
 SELECT p.id,p.year<>c.year AS bad_year,p.type<>c.type AS bad_type,p.is_xpac<>c.is_xpac AS bad_corpus,
 p.original_countries<>list_sort(list_distinct(c.countries)) AS bad_original_countries
 FROM read_parquet('@CACHE@/part-*.parquet') c JOIN patches p USING(id);
COPY (SELECT (SELECT count(*) FROM patches) AS expected_rows,count(*) AS joined_rows,
 count(DISTINCT id) AS unique_ids,count(*) FILTER(WHERE bad_year OR bad_type OR bad_corpus OR bad_original_countries) AS invalid_patch_baseline
 FROM cache_patch_check) TO '@OUT@/cache-patch-validation.json' (FORMAT JSON,ARRAY true);
COPY (
 SELECT * EXCLUDE(n) FROM (
 SELECT *,row_number() OVER(PARTITION BY is_xpac,review_class,decade ORDER BY hash(id)) AS n FROM (
 SELECT *,CASE WHEN conflict THEN 'conflict_review'
 WHEN newly_known AND len(institution_added)>0 AND len(text_added)>0 THEN 'new_both'
 WHEN newly_known AND len(institution_added)>0 THEN 'new_institution'
 WHEN newly_known THEN 'new_text'
 WHEN NOT was_unknown AND len(added_countries)>0 THEN 'known_extension'
 ELSE 'ambiguous_or_review_only' END AS review_class,
 CASE WHEN year<=0 THEN 'missing_year' WHEN year<2000 THEN 'pre2000' ELSE cast(floor(year/10)*10 AS VARCHAR) END AS decade
 FROM patches)) WHERE n<=4 ORDER BY is_xpac,review_class,decade,n
) TO '@OUT@/review-samples.json' (FORMAT JSON,ARRAY true);
COPY (
 SELECT is_xpac,country,count(*) AS work_country_assignments,
 count(*) FILTER(WHERE newly_known) AS newly_known_works_for_country,
 count(*) FILTER(WHERE NOT was_unknown) AS already_known_work_extensions
 FROM (SELECT is_xpac,newly_known,was_unknown,unnest(added_countries) AS country FROM patches) GROUP BY ALL ORDER BY is_xpac,newly_known_works_for_country DESC,country
) TO '@OUT@/new-country-distribution.json' (FORMAT JSON,ARRAY true);
COPY (
 SELECT is_xpac,count(*) AS unresolved_works,
 count(*) FILTER(WHERE len(raw_affiliations)>0) AS unresolved_with_raw_text,
 count(*) FILTER(WHERE len(institution_ids)>0) AS unresolved_with_institution_id,
 count(*) FILTER(WHERE len(institution_geo_country_candidates)>0) AS geo_country_review_potential,
 count(*) FILTER(WHERE len(exact_institution_name_candidates)>0) AS exact_institution_name_review_potential,
 count(*) FILTER(WHERE len(exact_institution_name_candidates)=1) AS unique_country_exact_name_review_potential
 FROM read_parquet('@WORK@/unresolved/*.parquet') WHERE year BETWEEN 2000 AND 2026 GROUP BY ALL ORDER BY is_xpac
) TO '@OUT@/secondary-field-potential.json' (FORMAT JSON,ARRAY true);
