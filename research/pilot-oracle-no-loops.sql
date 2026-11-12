SET threads=1;
SET memory_limit='2GB';
WITH sample AS MATERIALIZED (
  SELECT id, publication_year, type, is_paratext, referenced_works
  FROM read_parquet('/mnt/hg02/openalex-snapshot/data/parquet/works/updated_date=2026-06-26/part_0006.parquet')
  LIMIT 100000
), q AS MATERIALIZED (
  SELECT * FROM sample
  WHERE (type IS NULL OR type NOT IN ('editorial','erratum','retraction','paratext','peer-review','reference-entry','libguides','supplementary-materials','dataset','database','software','grant','standard','book','dissertation'))
    AND is_paratext IS NOT TRUE
), edges AS MATERIALIZED (
  SELECT DISTINCT a.id AS source, b.id AS target, a.publication_year AS year
  FROM q a CROSS JOIN UNNEST(a.referenced_works) AS refs(target_id)
  JOIN q b ON b.id=refs.target_id
  WHERE a.publication_year BETWEEN 2000 AND 2026
)
SELECT (SELECT count(*) FROM q) AS selected_works,
       (SELECT count(*) FROM edges WHERE source<>target) AS retained_edges,
       (SELECT count(*) FROM edges WHERE source=target) AS self_loops;

