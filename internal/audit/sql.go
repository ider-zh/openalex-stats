package audit

import (
	"fmt"
	"path/filepath"
	"strings"

	"openalexstats/internal/results"
)

type sampleCell struct {
	Alias, Country, Scheme, Level, Discipline string
	Weight                                    string
}

var sampleCells = func() []sampleCell {
	var cells []sampleCell
	for _, country := range []string{"CN", "US", "GB"} {
		prefix := strings.ToLower(country)
		for _, field := range []string{"17", "11"} {
			cells = append(cells, sampleCell{prefix + "_f" + field, country, "topics", "field", "https://openalex.org/fields/" + field, prefix + "_weight * CASE WHEN field" + field + " THEN 1.0 ELSE 0.0 END"})
		}
		cells = append(cells, sampleCell{prefix + "_biology", country, "concepts", "0", "https://openalex.org/C86803240", prefix + "_weight * biology_weight"})
	}
	return cells
}()

func sqlLiteral(s string) string {
	return "'" + strings.ReplaceAll(filepath.ToSlash(s), "'", "''") + "'"
}

func buildSQL(cfg Config, paths, blacklist []string, inventories ...[]results.Scenario) string {
	scenarios := auditScenarios("fractional")
	if len(inventories) > 0 {
		scenarios = inventories[0]
	}
	return buildSQLForCorpus(cfg, paths, blacklist, "core", scenarios)
}

func buildSQLForCorpus(cfg Config, paths, blacklist []string, corpus string, scenarios []results.Scenario) string {
	main, _ := mainAuditScenario(scenarios)
	quoted := make([]string, len(paths))
	for i, p := range paths {
		quoted[i] = sqlLiteral(p)
	}
	blocked := make([]string, len(blacklist))
	for i, value := range blacklist {
		blocked[i] = sqlLiteral(value)
	}
	corpusPredicate := "FALSE"
	switch corpus {
	case "core":
		corpusPredicate = "is_xpac IS FALSE AND quality_null_xpac IS FALSE"
	case "all":
		corpusPredicate = "TRUE"
	}
	q := "id > 0 AND (" + corpusPredicate + ") AND is_paratext IS NOT TRUE AND coalesce(type, '') NOT IN (" + strings.Join(blocked, ",") + ")"
	var b strings.Builder
	fmt.Fprintf(&b, "-- Independent DuckDB oracle; does not read Go counter shards or edge projections.\nSET threads=%d;\nSET memory_limit=%s;\nSET temp_directory=%s;\nSET preserve_insertion_order=false;\n", cfg.Threads, sqlLiteral(cfg.MemoryLimit), sqlLiteral(filepath.Join(cfg.WorkDir, "duckdb-temp")))
	fmt.Fprintf(&b, "CREATE VIEW raw AS SELECT * FROM read_parquet([%s], union_by_name=false);\n", strings.Join(quoted, ","))
	fmt.Fprintf(&b, "COPY (SELECT coalesce(type,'') AS type, CASE WHEN quality_null_xpac IS NOT FALSE THEN 'unknown' WHEN is_xpac IS TRUE THEN 'xpac' ELSE 'core' END AS corpus_group, count(*)::UBIGINT AS works, count(*) FILTER (WHERE %s)::UBIGINT AS selected FROM raw GROUP BY type, corpus_group ORDER BY type, corpus_group) TO %s (FORMAT JSON, ARRAY true);\n", q, sqlLiteral(filepath.Join(cfg.WorkDir, "inventory.json")))
	// Only the columns needed for exact membership and representative target
	// weights are materialized; references are streamed once in the graph join.
	b.WriteString("CREATE TABLE q AS WITH selected AS (SELECT id,year,trim(field_id) AS field_id, list_sort(list_distinct(list_filter(list_transform(countries,x -> trim(x)),x -> x IS NOT NULL AND x <> ''))) AS countries")
	for _, threshold := range []float64{0, .3, .5} {
		for _, level := range []int{0, 1} {
			fmt.Fprintf(&b, ", %s AS %s", conceptListSQL(level, threshold), conceptColumn(level, threshold))
		}
	}
	fmt.Fprintf(&b, " FROM raw WHERE %s) SELECT id,year,greatest(1,len(countries))::UBIGINT AS country_assignments, CASE WHEN list_contains(countries,'CN') THEN 1.0/len(countries) ELSE 0.0 END AS cn_weight, CASE WHEN list_contains(countries,'US') THEN 1.0/len(countries) ELSE 0.0 END AS us_weight, CASE WHEN list_contains(countries,'GB') THEN 1.0/len(countries) ELSE 0.0 END AS gb_weight, field_id='https://openalex.org/fields/17' AS field17, field_id='https://openalex.org/fields/11' AS field11", q)
	bio := "1.0/len(concepts0_t3)"
	if main.ConceptMode == "whole" {
		bio = "1.0"
	}
	fmt.Fprintf(&b, ",CASE WHEN list_contains(concepts0_t3,'https://openalex.org/C86803240') THEN %s ELSE 0.0 END AS biology_weight", bio)
	for _, threshold := range []float64{0, .3, .5} {
		for _, level := range []int{0, 1} {
			col := conceptColumn(level, threshold)
			fmt.Fprintf(&b, ",greatest(1,len(%s))::UBIGINT AS %s_assignments", col, col)
		}
	}
	b.WriteString(" FROM selected;\n")
	fmt.Fprintf(&b, "COPY (SELECT count(*)::UBIGINT AS selected, count(DISTINCT id)::UBIGINT AS unique_ids FROM q) TO %s (FORMAT JSON, ARRAY true);\n", sqlLiteral(filepath.Join(cfg.WorkDir, "q.json")))
	b.WriteString("CREATE TABLE publication_year AS SELECT year, count(*)::UBIGINT AS works")
	for _, cell := range sampleCells {
		fmt.Fprintf(&b, ", sum(%s)::DOUBLE AS %s", cell.Weight, cell.Alias)
	}
	writeAssignmentSums(&b, "", false)
	b.WriteString(" FROM q WHERE year BETWEEN 2000 AND 2026 GROUP BY year;\n")
	fmt.Fprintf(&b, "COPY (SELECT * FROM publication_year ORDER BY year) TO %s (FORMAT JSON, ARRAY true);\n", sqlLiteral(filepath.Join(cfg.WorkDir, "publication_year.json")))
	// A single complete edge join provides raw annual totals and all 9 weighted
	// target cells. Same-work loops are audited separately and excluded from sums.
	b.WriteString("CREATE TABLE citation_year AS SELECT src.year AS year, count(*) FILTER (WHERE t.id <> src.id)::UBIGINT AS edges, count(*) FILTER (WHERE t.id=src.id)::UBIGINT AS self_loops, count(*) FILTER (WHERE t.year > src.year AND t.id <> src.id)::UBIGINT AS future_edges")
	for _, cell := range sampleCells {
		fmt.Fprintf(&b, ", coalesce(sum(%s) FILTER (WHERE t.id <> src.id),0)::DOUBLE AS %s", qualifyWeight(cell.Weight, "t."), cell.Alias)
	}
	writeAssignmentSums(&b, "t.", true)
	fmt.Fprintf(&b, " FROM (SELECT id,year,\"references\" AS refs FROM raw WHERE %s AND year BETWEEN 2000 AND 2026) src CROSS JOIN UNNEST(list_distinct(src.refs)) AS ref(target_id) JOIN q t ON t.id=ref.target_id WHERE ref.target_id > 0 GROUP BY src.year;\n", q)
	fmt.Fprintf(&b, "COPY (SELECT * FROM citation_year ORDER BY year) TO %s (FORMAT JSON, ARRAY true);\n", sqlLiteral(filepath.Join(cfg.WorkDir, "citation_year.json")))
	cube := sqlLiteral(filepath.Join(cfg.ResultDir, "cube.parquet"))
	stats := sqlLiteral(filepath.Join(cfg.ResultDir, "statistics.parquet"))
	fmt.Fprintf(&b, "COPY (SELECT 'cube' AS source, %s AS scenario_id, scheme,level,year,count(*)::UBIGINT AS rows,sum(publications)::DOUBLE AS publications,sum(citations)::DOUBLE AS citations FROM read_parquet(%s) GROUP BY scheme,level,year UNION ALL SELECT 'statistics' AS source,scenario_id,scheme,level,year,count(*)::UBIGINT AS rows,sum(publications)::DOUBLE AS publications,sum(citations)::DOUBLE AS citations FROM read_parquet(%s) GROUP BY scenario_id,scheme,level,year) TO %s (FORMAT JSON, ARRAY true);\n", sqlLiteral(main.ID), cube, stats, sqlLiteral(filepath.Join(cfg.WorkDir, "output_year.json")))
	selectedCells := "country IN ('CN','US','GB') AND ((scheme='topics' AND level='field' AND discipline_id IN ('https://openalex.org/fields/17','https://openalex.org/fields/11')) OR (scheme='concepts' AND level='0' AND discipline_id='https://openalex.org/C86803240'))"
	fmt.Fprintf(&b, "COPY (SELECT 'cube' AS source,country,scheme,level,discipline_id,year,sum(publications)::DOUBLE AS publications,sum(citations)::DOUBLE AS citations FROM read_parquet(%s) WHERE %s GROUP BY country,scheme,level,discipline_id,year UNION ALL SELECT 'statistics' AS source,country,scheme,level,discipline_id,year,sum(publications)::DOUBLE AS publications,sum(citations)::DOUBLE AS citations FROM read_parquet(%s) WHERE scenario_id=%s AND %s GROUP BY country,scheme,level,discipline_id,year) TO %s (FORMAT JSON, ARRAY true);\n", cube, selectedCells, stats, sqlLiteral(main.ID), selectedCells, sqlLiteral(filepath.Join(cfg.WorkDir, "output_cells.json")))
	fmt.Fprintf(&b, "COPY (SELECT (SELECT count(*) FROM read_parquet(%s))::UBIGINT AS cube_rows, (SELECT count(*) FROM read_parquet(%s))::UBIGINT AS statistics_rows) TO %s (FORMAT JSON, ARRAY true);\n", cube, stats, sqlLiteral(filepath.Join(cfg.WorkDir, "output_counts.json")))
	dictionary := sqlLiteral(filepath.Join(cfg.ResultDir, "dictionary.json"))
	fmt.Fprintf(&b, "CREATE TABLE audit_labels AS SELECT layer.scheme AS scheme,layer.level AS level,label.id AS discipline_id FROM read_json_auto(%s) doc, UNNEST(doc.layers) ls(layer), UNNEST(layer.labels) ds(label);\nCREATE TABLE audit_countries AS SELECT country_label.id AS country FROM read_json_auto(%s) doc, UNNEST(doc.countries) cs(country_label);\n", dictionary, dictionary)
	var metadata []string
	for _, s := range scenarios {
		metadata = append(metadata, fmt.Sprintf("(scenario_id=%s AND country_mode=%s AND concept_mode=%s AND concept_threshold=%g AND main=%t)", sqlLiteral(s.ID), sqlLiteral(s.CountryMode), sqlLiteral(s.ConceptMode), s.ConceptThreshold, s.Main))
	}
	fmt.Fprintf(&b, "COPY (SELECT count(*) FILTER (WHERE country_mode='whole' AND concept_mode='whole' AND (publications IS NULL OR citations IS NULL OR NOT isfinite(publications) OR NOT isfinite(citations) OR publications<0 OR citations<0 OR publications<>trunc(publications) OR citations<>trunc(citations)))::UBIGINT AS invalid_whole_integers, count(*) FILTER (WHERE publications IS NULL OR citations IS NULL OR NOT isfinite(publications) OR NOT isfinite(citations) OR publications<0 OR citations<0)::UBIGINT AS invalid_counts, count(*) FILTER (WHERE scheme IS NULL OR level IS NULL OR year IS NULL OR NOT ((scheme='topics' AND level IN ('field','subfield')) OR (scheme='concepts' AND level IN ('0','1'))) OR year NOT BETWEEN 2000 AND 2026 OR country IS NULL OR trim(country)='' OR discipline_id IS NULL OR trim(discipline_id)='')::UBIGINT AS invalid_dimensions, count(*) FILTER (WHERE (%s) IS NOT TRUE)::UBIGINT AS invalid_scenario_metadata, count(*) FILTER (WHERE (discipline_id<>'UNKNOWN' AND d.discipline_id IS NULL) OR (country<>'UNKNOWN' AND c.country IS NULL))::UBIGINT AS invalid_dictionary_labels FROM read_parquet(%s) s LEFT JOIN audit_labels d USING(scheme,level,discipline_id) LEFT JOIN audit_countries c USING(country)) TO %s (FORMAT JSON, ARRAY true);\n", strings.Join(metadata, " OR "), stats, sqlLiteral(filepath.Join(cfg.WorkDir, "output_integrity.json")))
	return b.String()
}

func qualifyWeight(expression, qualifier string) string {
	for _, name := range []string{"cn_weight", "us_weight", "gb_weight", "field17", "field11", "biology_weight"} {
		expression = strings.ReplaceAll(expression, name, qualifier+name)
	}
	return expression
}

// A qualifying repeated ID is retained once. Filtering before distinct is
// equivalent to maximum finite score per ID then thresholding, separately by level.
func conceptColumn(level int, threshold float64) string {
	return fmt.Sprintf("concepts%d_t%.0f", level, threshold*10)
}
func conceptListSQL(level int, threshold float64) string {
	return fmt.Sprintf("list_distinct(list_transform(list_filter(range(1,len(concept_ids%d)+1),i -> isfinite(concept_scores%d[i]) AND concept_scores%d[i] >= %s AND trim(concept_ids%d[i]) <> ''),i -> trim(concept_ids%d[i])))", level, level, level, fmt.Sprintf("%.1f", threshold), level, level)
}
func writeAssignmentSums(b *strings.Builder, qualifier string, citations bool) {
	filter := ""
	if citations {
		filter = " FILTER (WHERE t.id <> src.id)"
	}
	fmt.Fprintf(b, ",coalesce(sum(%scountry_assignments)%s,0)::UBIGINT AS country_assignments", qualifier, filter)
	for _, threshold := range []float64{0, .3, .5} {
		for _, level := range []int{0, 1} {
			col := conceptColumn(level, threshold) + "_assignments"
			fmt.Fprintf(b, ",coalesce(sum(%s%s)%s,0)::UBIGINT AS %s,coalesce(sum(%scountry_assignments * %s%s)%s,0)::UBIGINT AS country_%s", qualifier, col, filter, col, qualifier, qualifier, col, filter, col)
		}
	}
}
