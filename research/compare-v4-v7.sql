-- Full exported-cell equivalence; independent of Go aggregation.
SET threads=12;
SET memory_limit='128GB';
SET temp_directory='/mnt/nvme1/backup/openalex-stats/equivalence-v7/temp';
CREATE VIEW b AS SELECT * FROM read_parquet('/mnt/nvme0/public/openalex-stats/results/core-goal-v1/cube.parquet');
CREATE VIEW c AS SELECT * FROM read_parquet('/mnt/nvme0/public/openalex-stats/results/core-goal-v2/cube.parquet');
COPY (SELECT 'cube' AS artifact,
count(*) FILTER (WHERE b.country IS NULL)::BIGINT AS missing_baseline,
count(*) FILTER (WHERE c.country IS NULL)::BIGINT AS missing_candidate,
count(*) FILTER (WHERE b.country IS NOT NULL AND c.country IS NOT NULL)::BIGINT AS matched_rows,
(SELECT count(*)-count(DISTINCT (country,year,scheme,level,discipline_id)) FROM b)::BIGINT AS duplicate_baseline,
(SELECT count(*)-count(DISTINCT (country,year,scheme,level,discipline_id)) FROM c)::BIGINT AS duplicate_candidate,
count(*) FILTER (WHERE (b.country IS NOT NULL AND c.country IS NOT NULL) AND ((NOT isfinite(b.publications) OR NOT isfinite(c.publications) OR b.publications IS NULL OR c.publications IS NULL OR abs(b.publications-c.publications) > 1e-7 + 1e-10*greatest(abs(b.publications),abs(c.publications))) OR (NOT isfinite(b.citations) OR NOT isfinite(c.citations) OR b.citations IS NULL OR c.citations IS NULL OR abs(b.citations-c.citations) > 1e-7 + 1e-10*greatest(abs(b.citations),abs(c.citations)))))::BIGINT AS numeric_mismatch_rows,
count(*) FILTER (WHERE (b.country IS NOT NULL AND c.country IS NOT NULL) AND (false))::BIGINT AS flag_mismatch_rows,
coalesce(max(abs(b.publications-c.publications)),0)::DOUBLE AS max_abs_publications,
coalesce(max(abs(b.citations-c.citations)),0)::DOUBLE AS max_abs_citations FROM b FULL OUTER JOIN c USING (country,year,scheme,level,discipline_id)) TO '/mnt/nvme1/backup/openalex-stats/equivalence-v7/cube.json' (FORMAT JSON, ARRAY true);
DROP VIEW b;
DROP VIEW c;
CREATE VIEW b AS SELECT * FROM read_parquet('/mnt/nvme0/public/openalex-stats/results/core-goal-v1/statistics.parquet');
CREATE VIEW c AS SELECT * FROM read_parquet('/mnt/nvme0/public/openalex-stats/results/core-goal-v2/statistics.parquet');
COPY (SELECT 'statistics' AS artifact,
count(*) FILTER (WHERE b.scenario_id IS NULL)::BIGINT AS missing_baseline,
count(*) FILTER (WHERE c.scenario_id IS NULL)::BIGINT AS missing_candidate,
count(*) FILTER (WHERE b.scenario_id IS NOT NULL AND c.scenario_id IS NOT NULL)::BIGINT AS matched_rows,
(SELECT count(*)-count(DISTINCT (scenario_id,country,year,scheme,level,discipline_id)) FROM b)::BIGINT AS duplicate_baseline,
(SELECT count(*)-count(DISTINCT (scenario_id,country,year,scheme,level,discipline_id)) FROM c)::BIGINT AS duplicate_candidate,
count(*) FILTER (WHERE (b.scenario_id IS NOT NULL AND c.scenario_id IS NOT NULL) AND ((NOT isfinite(b.publications) OR NOT isfinite(c.publications) OR b.publications IS NULL OR c.publications IS NULL OR abs(b.publications-c.publications) > 1e-7 + 1e-10*greatest(abs(b.publications),abs(c.publications))) OR (NOT isfinite(b.citations) OR NOT isfinite(c.citations) OR b.citations IS NULL OR c.citations IS NULL OR abs(b.citations-c.citations) > 1e-7 + 1e-10*greatest(abs(b.citations),abs(c.citations))) OR (NOT isfinite(b.publication_share) OR NOT isfinite(c.publication_share) OR b.publication_share IS NULL OR c.publication_share IS NULL OR abs(b.publication_share-c.publication_share) > 1e-7 + 1e-10*greatest(abs(b.publication_share),abs(c.publication_share))) OR (NOT isfinite(b.citation_share) OR NOT isfinite(c.citation_share) OR b.citation_share IS NULL OR c.citation_share IS NULL OR abs(b.citation_share-c.citation_share) > 1e-7 + 1e-10*greatest(abs(b.citation_share),abs(c.citation_share))) OR (NOT isfinite(b.publication_rca) OR NOT isfinite(c.publication_rca) OR b.publication_rca IS NULL OR c.publication_rca IS NULL OR abs(b.publication_rca-c.publication_rca) > 1e-7 + 1e-10*greatest(abs(b.publication_rca),abs(c.publication_rca))) OR (NOT isfinite(b.citation_rca) OR NOT isfinite(c.citation_rca) OR b.citation_rca IS NULL OR c.citation_rca IS NULL OR abs(b.citation_rca-c.citation_rca) > 1e-7 + 1e-10*greatest(abs(b.citation_rca),abs(c.citation_rca))) OR (NOT isfinite(b.publication_matrix_total) OR NOT isfinite(c.publication_matrix_total) OR b.publication_matrix_total IS NULL OR c.publication_matrix_total IS NULL OR abs(b.publication_matrix_total-c.publication_matrix_total) > 1e-7 + 1e-10*greatest(abs(b.publication_matrix_total),abs(c.publication_matrix_total))) OR (NOT isfinite(b.citation_matrix_total) OR NOT isfinite(c.citation_matrix_total) OR b.citation_matrix_total IS NULL OR c.citation_matrix_total IS NULL OR abs(b.citation_matrix_total-c.citation_matrix_total) > 1e-7 + 1e-10*greatest(abs(b.citation_matrix_total),abs(c.citation_matrix_total)))))::BIGINT AS numeric_mismatch_rows,
count(*) FILTER (WHERE (b.scenario_id IS NOT NULL AND c.scenario_id IS NOT NULL) AND (b.country_mode IS DISTINCT FROM c.country_mode OR b.concept_mode IS DISTINCT FROM c.concept_mode OR b.concept_threshold IS DISTINCT FROM c.concept_threshold OR b.main IS DISTINCT FROM c.main OR b.publication_share_valid IS DISTINCT FROM c.publication_share_valid OR b.citation_share_valid IS DISTINCT FROM c.citation_share_valid OR b.publication_rca_valid IS DISTINCT FROM c.publication_rca_valid OR b.citation_rca_valid IS DISTINCT FROM c.citation_rca_valid))::BIGINT AS flag_mismatch_rows,
coalesce(max(abs(b.publications-c.publications)),0)::DOUBLE AS max_abs_publications,
coalesce(max(abs(b.citations-c.citations)),0)::DOUBLE AS max_abs_citations,
coalesce(max(abs(b.publication_share-c.publication_share)),0)::DOUBLE AS max_abs_publication_share,
coalesce(max(abs(b.citation_share-c.citation_share)),0)::DOUBLE AS max_abs_citation_share,
coalesce(max(abs(b.publication_rca-c.publication_rca)),0)::DOUBLE AS max_abs_publication_rca,
coalesce(max(abs(b.citation_rca-c.citation_rca)),0)::DOUBLE AS max_abs_citation_rca,
coalesce(max(abs(b.publication_matrix_total-c.publication_matrix_total)),0)::DOUBLE AS max_abs_publication_matrix_total,
coalesce(max(abs(b.citation_matrix_total-c.citation_matrix_total)),0)::DOUBLE AS max_abs_citation_matrix_total FROM b FULL OUTER JOIN c USING (scenario_id,country,year,scheme,level,discipline_id)) TO '/mnt/nvme1/backup/openalex-stats/equivalence-v7/statistics.json' (FORMAT JSON, ARRAY true);
DROP VIEW b;
DROP VIEW c;
CREATE VIEW b AS SELECT * FROM read_parquet('/mnt/nvme0/public/openalex-stats/results/core-goal-v1/rca.parquet');
CREATE VIEW c AS SELECT * FROM read_parquet('/mnt/nvme0/public/openalex-stats/results/core-goal-v2/rca.parquet');
COPY (SELECT 'rca' AS artifact,
count(*) FILTER (WHERE b.scenario_id IS NULL)::BIGINT AS missing_baseline,
count(*) FILTER (WHERE c.scenario_id IS NULL)::BIGINT AS missing_candidate,
count(*) FILTER (WHERE b.scenario_id IS NOT NULL AND c.scenario_id IS NOT NULL)::BIGINT AS matched_rows,
(SELECT count(*)-count(DISTINCT (scenario_id,country,year,scheme,level,metric,discipline_id)) FROM b)::BIGINT AS duplicate_baseline,
(SELECT count(*)-count(DISTINCT (scenario_id,country,year,scheme,level,metric,discipline_id)) FROM c)::BIGINT AS duplicate_candidate,
count(*) FILTER (WHERE (b.scenario_id IS NOT NULL AND c.scenario_id IS NOT NULL) AND ((NOT isfinite(b.rca) OR NOT isfinite(c.rca) OR b.rca IS NULL OR c.rca IS NULL OR abs(b.rca-c.rca) > 1e-7 + 1e-10*greatest(abs(b.rca),abs(c.rca)))))::BIGINT AS numeric_mismatch_rows,
count(*) FILTER (WHERE (b.scenario_id IS NOT NULL AND c.scenario_id IS NOT NULL) AND (b.valid IS DISTINCT FROM c.valid))::BIGINT AS flag_mismatch_rows,
coalesce(max(abs(b.rca-c.rca)),0)::DOUBLE AS max_abs_rca FROM b FULL OUTER JOIN c USING (scenario_id,country,year,scheme,level,metric,discipline_id)) TO '/mnt/nvme1/backup/openalex-stats/equivalence-v7/rca.json' (FORMAT JSON, ARRAY true);
DROP VIEW b;
DROP VIEW c;

