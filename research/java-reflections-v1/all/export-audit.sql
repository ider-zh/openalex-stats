SET threads=4; SET memory_limit='16GB';
CREATE TEMP TABLE m AS SELECT * FROM read_json('/mnt/nvme0/public/openalex-stats/results/all-java-reflections-v1/java-reflections-v1.json',format='array',maximum_object_size=33554432,columns={scenario_id:'VARCHAR',year:'INTEGER',scheme:'VARCHAR',level:'VARCHAR',metric:'VARCHAR',result:'STRUCT(countries VARCHAR[],disciplines VARCHAR[],eci DOUBLE[],pci DOUBLE[],active_countries INTEGER[],active_disciplines INTEGER[],status VARCHAR,unstable BOOLEAN)'});

COPY (
WITH positions AS (
 SELECT scenario_id,year,scheme,level,metric,u.id,result.status AS model_status,result.unstable AS unstable,
 coalesce(list_position(result.active_countries,u.ordinality-1),0)::INTEGER AS active_position,result.eci AS score_values
 FROM m,unnest(result.countries) WITH ORDINALITY AS u(id,ordinality)
), expected AS (
 SELECT * EXCLUDE(score_values,active_position),active_position>0 AS active,
 model_status='ok' AND active_position>0 AS valid,
 CASE WHEN model_status='ok' AND active_position>0 THEN list_extract(score_values,active_position) ELSE 0 END AS score
 FROM positions
), actual AS (SELECT * FROM read_parquet('/mnt/nvme0/public/openalex-stats/results/all-java-reflections-v1/country_eci.parquet'))
SELECT 'all' AS corpus,'country_eci' AS artifact,
count(e.id) AS expected_rows,count(a.id) AS actual_rows,
count(*) FILTER(WHERE e.id IS NULL OR a.id IS NULL) AS missing_or_extra,
count(*) FILTER(WHERE e.active<>a.active OR e.valid<>a.valid OR e.model_status<>a.model_status OR e.unstable<>a.unstable) AS semantic_mismatches,
count(*) FILTER(WHERE NOT isfinite(a.score) OR e.score<>a.score) AS score_mismatches,
coalesce(max(abs(e.score-a.score)),0) AS max_score_difference
FROM expected e FULL OUTER JOIN actual a USING(scenario_id,year,scheme,level,metric,id)
) TO '/home/ider/tmpdata/openalex-stats-dev-20261112/methods-v1/all/country_eci-export-audit.json' (FORMAT JSON,ARRAY true);


COPY (
WITH positions AS (
 SELECT scenario_id,year,scheme,level,metric,u.id,result.status AS model_status,result.unstable AS unstable,
 coalesce(list_position(result.active_disciplines,u.ordinality-1),0)::INTEGER AS active_position,result.pci AS score_values
 FROM m,unnest(result.disciplines) WITH ORDINALITY AS u(id,ordinality)
), expected AS (
 SELECT * EXCLUDE(score_values,active_position),active_position>0 AS active,
 model_status='ok' AND active_position>0 AS valid,
 CASE WHEN model_status='ok' AND active_position>0 THEN list_extract(score_values,active_position) ELSE 0 END AS score
 FROM positions
), actual AS (SELECT * FROM read_parquet('/mnt/nvme0/public/openalex-stats/results/all-java-reflections-v1/discipline_pci.parquet'))
SELECT 'all' AS corpus,'discipline_pci' AS artifact,
count(e.id) AS expected_rows,count(a.id) AS actual_rows,
count(*) FILTER(WHERE e.id IS NULL OR a.id IS NULL) AS missing_or_extra,
count(*) FILTER(WHERE e.active<>a.active OR e.valid<>a.valid OR e.model_status<>a.model_status OR e.unstable<>a.unstable) AS semantic_mismatches,
count(*) FILTER(WHERE NOT isfinite(a.score) OR e.score<>a.score) AS score_mismatches,
coalesce(max(abs(e.score-a.score)),0) AS max_score_difference
FROM expected e FULL OUTER JOIN actual a USING(scenario_id,year,scheme,level,metric,id)
) TO '/home/ider/tmpdata/openalex-stats-dev-20261112/methods-v1/all/discipline_pci-export-audit.json' (FORMAT JSON,ARRAY true);
