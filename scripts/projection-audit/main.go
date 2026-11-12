// projection-audit independently compares two bounded real raw Parquet files
// with their normalized cache shards using DuckDB, never the Go scanner.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type part struct {
	InputPath         string `json:"input_path"`
	InputSize         int64  `json:"input_size"`
	InputModTimeNS    int64  `json:"input_mod_time_ns"`
	Path              string `json:"path"`
	Bytes             int64  `json:"bytes"`
	Rows              int64  `json:"rows"`
	SHA256            string `json:"sha256"`
	SourceFingerprint string `json:"source_fingerprint"`
}
type cacheManifest struct {
	Complete             bool   `json:"complete"`
	Format               string `json:"format_version"`
	SourceManifestSHA256 string `json:"source_manifest_sha256"`
	Parts                []part `json:"parts"`
}
type evidence struct {
	Path   string `json:"path"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}
type report struct {
	Version              string           `json:"version"`
	Status               string           `json:"status"`
	Passed               bool             `json:"passed"`
	StartedAt            time.Time        `json:"started_at"`
	CompletedAt          time.Time        `json:"completed_at"`
	SourceManifestSHA256 string           `json:"source_manifest_sha256"`
	DuckDBVersion        string           `json:"duckdb_version"`
	Threads              int              `json:"threads"`
	MemoryLimit          string           `json:"memory_limit"`
	Parts                []part           `json:"parts"`
	RawRows              int64            `json:"raw_rows"`
	ComparedRows         int64            `json:"compared_rows"`
	Checks               map[string]int64 `json:"mismatch_counts"`
	Evidence             []evidence       `json:"evidence"`
	Seconds              float64          `json:"seconds"`
	Failure              string           `json:"failure,omitempty"`
	Notes                []string         `json:"notes"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() (err error) {
	manifestPath := flag.String("cache-manifest", "", "complete normalized cache manifest")
	directory := flag.String("work", "", "dedicated output directory")
	duckdb := flag.String("duckdb", "duckdb", "DuckDB CLI")
	timeout := flag.Duration("timeout", 5*time.Minute, "bounded query timeout")
	flag.Parse()
	if *manifestPath == "" || *directory == "" || flag.NArg() != 0 {
		return fmt.Errorf("--cache-manifest and --work required")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	ctx, stop := context.WithTimeout(ctx, *timeout)
	defer stop()
	manifestBytes, e := os.ReadFile(*manifestPath)
	if e != nil {
		return e
	}
	var manifest cacheManifest
	if e = json.Unmarshal(manifestBytes, &manifest); e != nil {
		return e
	}
	if !manifest.Complete || manifest.Format != "openalexstats-normalized-parquet-v1" {
		return fmt.Errorf("unsupported or incomplete cache")
	}
	var selected []part
	for _, suffix := range []string{"/works/updated_date=2016-06-24/part_0000.parquet", "/works/updated_date=2026-06-26/part_0006.parquet"} {
		for _, p := range manifest.Parts {
			if strings.HasSuffix(filepath.ToSlash(p.InputPath), suffix) {
				selected = append(selected, p)
			}
		}
	}
	if len(selected) != 2 {
		return fmt.Errorf("expected exactly the two predefined real source shards")
	}
	if e = os.MkdirAll(*directory, 0755); e != nil {
		return e
	}
	r := report{Version: "raw-cache-projection-audit-v1", Status: "running", StartedAt: time.Now().UTC(), SourceManifestSHA256: manifest.SourceManifestSHA256, Threads: 2, MemoryLimit: "2GB", Parts: selected, Checks: map[string]int64{}, Notes: []string{"Bounded two-file, all-row comparison: earliest shard plus latest part_0006, not a full source projection audit.", "Uses only DuckDB raw schema operations and normalized file contents, not Go scanner/projection helpers.", "Checks exact canonical ID/year/type/flags, raw and trimmed country sets, primary IDs, concept pair multisets and per-ID max scores, reference sequence and set, and every persisted row-quality counter.", "Raw files are bound by recorded size/mtime/source fingerprint; normalized shards and audit evidence are SHA256 checked. Whole raw files are not separately SHA256 hashed."}}
	defer func() {
		r.CompletedAt = time.Now().UTC()
		r.Seconds = r.CompletedAt.Sub(r.StartedAt).Seconds()
		if err != nil {
			r.Status = "failed"
			r.Failure = err.Error()
		}
		b, _ := json.MarshalIndent(r, "", "  ")
		_ = os.WriteFile(filepath.Join(*directory, "audit.json"), append(b, '\n'), 0644)
	}()
	for _, p := range selected {
		if e = validateSource(p); e != nil {
			return e
		}
		hash, e := fileHash(ctx, p.Path)
		if e != nil {
			return e
		}
		if hash.Bytes != p.Bytes || hash.SHA256 != p.SHA256 {
			return fmt.Errorf("normalized shard changed: %s", p.Path)
		}
		r.Evidence = append(r.Evidence, hash)
		r.RawRows += p.Rows
	}
	version, e := exec.CommandContext(ctx, *duckdb, "--version").Output()
	if e != nil {
		return e
	}
	r.DuckDBVersion = strings.TrimSpace(string(version))
	sql := querySQL(selected, *directory)
	queryPath := filepath.Join(*directory, "query.sql")
	if e = os.WriteFile(queryPath, []byte(sql), 0644); e != nil {
		return e
	}
	stdout, e := os.Create(filepath.Join(*directory, "duckdb.stdout.log"))
	if e != nil {
		return e
	}
	defer stdout.Close()
	stderr, e := os.Create(filepath.Join(*directory, "duckdb.stderr.log"))
	if e != nil {
		return e
	}
	defer stderr.Close()
	cmd := exec.CommandContext(ctx, *duckdb, ":memory:", "-batch")
	cmd.Stdin = strings.NewReader(sql)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if e = cmd.Run(); e != nil {
		return fmt.Errorf("DuckDB projection comparison: %w; see stderr log", e)
	}
	var inventory []struct {
		Part       string `json:"audit_part"`
		Source     string `json:"source"`
		Rows       int64  `json:"rows"`
		UniqueIDs  int64  `json:"unique_ids"`
		InvalidIDs int64  `json:"invalid_ids"`
	}
	if e = readJSON(filepath.Join(*directory, "inventory.json"), &inventory); e != nil {
		return e
	}
	if len(inventory) != 4 {
		return fmt.Errorf("expected four raw/cache inventory groups")
	}
	for _, v := range inventory {
		if v.Rows != v.UniqueIDs || v.InvalidIDs != 0 {
			return fmt.Errorf("duplicate or invalid main ID in %s/%s", v.Part, v.Source)
		}
	}
	var comparisons []map[string]json.RawMessage
	if e = readJSON(filepath.Join(*directory, "comparison.json"), &comparisons); e != nil {
		return e
	}
	if len(comparisons) != 2 {
		return fmt.Errorf("expected two comparison groups")
	}
	for _, values := range comparisons {
		for key, raw := range values {
			if key == "audit_part" {
				continue
			}
			var count int64
			if e = json.Unmarshal(raw, &count); e != nil {
				return e
			}
			if key == "rows" {
				r.ComparedRows += count
			} else {
				r.Checks[key] += count
			}
		}
	}
	if r.ComparedRows != r.RawRows {
		return fmt.Errorf("comparison row count differs from cache/source manifest")
	}
	for key, count := range r.Checks {
		if count != 0 {
			return fmt.Errorf("projection mismatch %s=%d", key, count)
		}
	}
	for _, p := range selected {
		if e = validateSource(p); e != nil {
			return e
		}
		hash, e := fileHash(ctx, p.Path)
		if e != nil {
			return e
		}
		if hash.SHA256 != p.SHA256 {
			return fmt.Errorf("normalized shard changed during audit")
		}
	}
	for _, name := range []string{"query.sql", "inventory.json", "comparison.json", "mismatch-examples.json", "duckdb.stdout.log", "duckdb.stderr.log"} {
		hash, e := fileHash(ctx, filepath.Join(*directory, name))
		if e != nil {
			return e
		}
		r.Evidence = append(r.Evidence, hash)
	}
	r.Status = "passed"
	r.Passed = true
	summary, _ := json.Marshal(map[string]any{"status": r.Status, "raw_rows": r.RawRows, "compared_rows": r.ComparedRows, "mismatch_counts": r.Checks})
	fmt.Println(string(summary))
	return nil
}
func validateSource(p part) error {
	info, e := os.Stat(p.InputPath)
	if e != nil {
		return e
	}
	if info.Size() != p.InputSize || info.ModTime().UnixNano() != p.InputModTimeNS {
		return fmt.Errorf("raw source changed since projection: %s", p.InputPath)
	}
	return nil
}
func fileHash(ctx context.Context, path string) (evidence, error) {
	out := evidence{Path: path}
	f, e := os.Open(path)
	if e != nil {
		return out, e
	}
	defer f.Close()
	h := sha256.New()
	buffer := make([]byte, 1<<20)
	for {
		if e = ctx.Err(); e != nil {
			return out, e
		}
		n, err := f.Read(buffer)
		if n > 0 {
			_, _ = h.Write(buffer[:n])
			out.Bytes += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return out, err
		}
	}
	out.SHA256 = hex.EncodeToString(h.Sum(nil))
	return out, nil
}
func readJSON(path string, v any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func lit(s string) string { return "'" + strings.ReplaceAll(filepath.ToSlash(s), "'", "''") + "'" }

func querySQL(parts []part, directory string) string {
	var raw, normalized []string
	for i, p := range parts {
		label := lit(fmt.Sprintf("part-%d", i))
		raw = append(raw, "SELECT "+label+" AS audit_part, id, publication_year, type, is_xpac, is_paratext, is_retracted, authorships, primary_topic, concepts, referenced_works FROM read_parquet("+lit(p.InputPath)+")")
		normalized = append(normalized, "SELECT "+label+" AS audit_part, * FROM read_parquet("+lit(p.Path)+")")
	}
	var b strings.Builder
	b.WriteString("SET threads=2;\nSET memory_limit='2GB';\nSET preserve_insertion_order=false;\nSET temp_directory=" + lit(filepath.Join(directory, "temp")) + ";\n")
	b.WriteString(`CREATE MACRO wid(value) AS CASE WHEN regexp_full_match(value, '(https://openalex[.]org/)?W[1-9][0-9]*') THEN try_cast(regexp_extract(value, 'W([1-9][0-9]*)$', 1) AS UBIGINT) ELSE NULL END;
CREATE MACRO valid_concept(c) AS c IS NOT NULL AND c.level IS NOT NULL AND c.id IS NOT NULL AND c.id<>'' AND c.score IS NOT NULL AND isfinite(c.score);
CREATE MACRO country_set(xs) AS list_sort(list_distinct(list_filter(xs, x -> x IS NOT NULL AND x<>'')));
CREATE MACRO trimmed_countries(xs) AS country_set(list_transform(xs, x -> trim(x)));
`)
	b.WriteString("CREATE VIEW raw AS " + strings.Join(raw, " UNION ALL ") + ";\nCREATE VIEW normalized AS " + strings.Join(normalized, " UNION ALL ") + ";\n")
	b.WriteString(`CREATE TABLE expected AS
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
`)
	fields := []string{"missing_raw", "missing_cache", "year_diff", "type_diff", "xpac_diff", "paratext_diff", "retracted_diff", "countries_diff", "trimmed_countries_diff", "field_diff", "subfield_diff", "concept0_pairs_diff", "concept1_pairs_diff", "concept0_max_diff", "concept1_max_diff", "concept0_length_diff", "concept1_length_diff", "reference_sequence_diff", "reference_set_diff", "invalid_reference_quality_diff", "invalid_concept_quality_diff", "missing_year_quality_diff", "null_xpac_quality_diff"}
	// DuckDB 1.4 does not bind an outer lambda variable inside the nested lambda
	// needed for a per-ID max macro. Independent SQL GROUP BY is both explicit
	// and compatible with the installed engine.
	for _, table := range []string{"expected", "actual"} {
		var levels []string
		for level := 0; level < 2; level++ {
			levels = append(levels, fmt.Sprintf("SELECT audit_part,id AS work_id,%d AS concept_level,c.id AS concept_id,max(c.score) AS max_score FROM %s CROSS JOIN UNNEST(concepts%d) AS items(c) GROUP BY audit_part,work_id,concept_id", level, table, level))
		}
		b.WriteString("CREATE TABLE " + table + "_max AS " + strings.Join(levels, " UNION ALL ") + ";\n")
	}
	b.WriteString("CREATE TABLE max_diffs AS SELECT coalesce(r.audit_part,n.audit_part) AS audit_part,coalesce(r.work_id,n.work_id) AS work_id,coalesce(r.concept_level,n.concept_level) AS concept_level FROM expected_max r FULL OUTER JOIN actual_max n ON r.audit_part=n.audit_part AND r.work_id=n.work_id AND r.concept_level=n.concept_level AND r.concept_id=n.concept_id WHERE r.max_score IS DISTINCT FROM n.max_score;\n")
	aggregates := []string{"audit_part", "count(*)::BIGINT AS rows"}
	var mismatch []string
	for _, field := range fields {
		if field == "concept0_max_diff" || field == "concept1_max_diff" {
			continue
		}
		aggregates = append(aggregates, "count(*) FILTER (WHERE "+field+")::BIGINT AS "+field)
		mismatch = append(mismatch, field)
	}
	b.WriteString("COPY (SELECT t.*,coalesce(m.concept0_max_diff,0)::BIGINT AS concept0_max_diff,coalesce(m.concept1_max_diff,0)::BIGINT AS concept1_max_diff FROM (SELECT " + strings.Join(aggregates, ",") + " FROM compared GROUP BY audit_part) t LEFT JOIN (SELECT audit_part,count(DISTINCT work_id) FILTER (WHERE concept_level=0) AS concept0_max_diff,count(DISTINCT work_id) FILTER (WHERE concept_level=1) AS concept1_max_diff FROM max_diffs GROUP BY audit_part) m USING(audit_part) ORDER BY t.audit_part) TO " + lit(filepath.Join(directory, "comparison.json")) + " (FORMAT JSON,ARRAY true);\n")
	b.WriteString("COPY (SELECT * FROM compared WHERE " + strings.Join(mismatch, " OR ") + " LIMIT 20) TO " + lit(filepath.Join(directory, "mismatch-examples.json")) + " (FORMAT JSON,ARRAY true);\n")
	b.WriteString("COPY (SELECT audit_part,'raw' AS source,count(*)::BIGINT AS rows,count(DISTINCT id)::BIGINT AS unique_ids,count(*) FILTER (WHERE id IS NULL OR id=0)::BIGINT AS invalid_ids FROM expected GROUP BY audit_part UNION ALL SELECT audit_part,'cache' AS source,count(*)::BIGINT AS rows,count(DISTINCT id)::BIGINT AS unique_ids,count(*) FILTER (WHERE id IS NULL OR id=0)::BIGINT AS invalid_ids FROM actual GROUP BY audit_part) TO " + lit(filepath.Join(directory, "inventory.json")) + " (FORMAT JSON,ARRAY true);\n")
	return b.String()
}
