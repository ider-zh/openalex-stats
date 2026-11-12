SET threads=4;
SET memory_limit='2GB';
CREATE VIEW s AS SELECT * FROM read_parquet('/mnt/nvme0/public/openalex-stats/results/core-goal-v2/statistics.parquet') WHERE scenario_id='whole-concept-0.3';
COPY (SELECT scheme,level,count(*)::BIGINT AS cells,count(*) FILTER(WHERE publications<>trunc(publications))::BIGINT AS noninteger_publication_cells,max(abs(publications-round(publications))) AS max_distance_to_integer FROM s GROUP BY scheme,level ORDER BY scheme,level) TO '/mnt/nvme0/public/openalex-stats/audits/counting-review-20261112/integrality.json' (FORMAT JSON, ARRAY true);
CREATE TABLE totals AS SELECT country,year,scheme,level,sum(publications) AS publications FROM s GROUP BY country,year,scheme,level;
COPY (SELECT t.scheme,t.level,count(*)::BIGINT AS country_years,max(abs(t.publications-b.publications)) AS max_delta_from_topics_whole_total,count(*) FILTER(WHERE abs(t.publications-b.publications)>1e-7+1e-12*abs(b.publications))::BIGINT AS mismatched_country_years FROM totals t JOIN totals b ON t.country=b.country AND t.year=b.year AND b.scheme='topics' AND b.level='field' GROUP BY t.scheme,t.level ORDER BY t.scheme,t.level) TO '/mnt/nvme0/public/openalex-stats/audits/counting-review-20261112/country_totals.json' (FORMAT JSON, ARRAY true);
COPY (SELECT * FROM totals WHERE country='CN' AND year=2025 ORDER BY scheme,level) TO '/mnt/nvme0/public/openalex-stats/audits/counting-review-20261112/example_total.json' (FORMAT JSON, ARRAY true);

