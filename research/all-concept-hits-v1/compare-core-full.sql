-- Count monotonicity only. Shares/RCA/ECI are intentionally not compared.
SET threads=4;
SET memory_limit='64GB';
CREATE VIEW core AS SELECT * FROM read_parquet('/mnt/nvme0/public/openalex-stats/results/core-concept-hits-v1/statistics.parquet');
CREATE VIEW fullset AS SELECT * FROM read_parquet('/mnt/nvme0/public/openalex-stats/results/all-concept-hits-v1/statistics.parquet');
COPY (
 SELECT b.scenario_id,b.scheme,b.level,count(*)::BIGINT AS core_cells,
 count(*) FILTER(WHERE a.scenario_id IS NULL)::BIGINT AS core_cells_missing_in_full,
 count(*) FILTER(WHERE a.scenario_id IS NOT NULL AND (a.publications IS NULL OR a.citations IS NULL OR NOT isfinite(a.publications) OR NOT isfinite(a.citations) OR a.publications<0 OR a.citations<0))::BIGINT AS invalid_full_count_cells,
 count(*) FILTER(WHERE a.publications+1e-7+1e-10*abs(b.publications)<b.publications)::BIGINT AS publication_decreases,
 count(*) FILTER(WHERE a.citations+1e-7+1e-10*abs(b.citations)<b.citations)::BIGINT AS citation_decreases,
 coalesce(max(greatest(b.publications-a.publications,0)),0)::DOUBLE AS max_publication_shortfall,
 coalesce(max(greatest(b.citations-a.citations,0)),0)::DOUBLE AS max_citation_shortfall
 FROM core b LEFT JOIN fullset a USING(scenario_id,country,year,scheme,level,discipline_id)
 GROUP BY b.scenario_id,b.scheme,b.level ORDER BY b.scenario_id,b.scheme,b.level
) TO '/home/ider/tmpdata/openalex-stats-dev-20261112/all-concept-hits-v1/core-full-monotonicity.json' (FORMAT JSON,ARRAY true);
COPY (
 SELECT scheme,level,count(*)::BIGINT AS cells,
 count(*) FILTER(WHERE publications IS NULL OR NOT isfinite(publications) OR publications<0 OR publications<>trunc(publications))::BIGINT AS invalid_publication_integer_cells,
 count(*) FILTER(WHERE citations IS NULL OR NOT isfinite(citations) OR citations<0 OR citations<>trunc(citations))::BIGINT AS invalid_citation_integer_cells
 FROM fullset WHERE country_mode='whole' AND concept_mode='whole'
 GROUP BY scheme,level ORDER BY scheme,level
) TO '/home/ider/tmpdata/openalex-stats-dev-20261112/all-concept-hits-v1/full-integrality.json' (FORMAT JSON,ARRAY true);

