package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"

	"github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress/zstd"
	"openalexstats/internal/checkpoint"
	"openalexstats/internal/pipeline"
	"openalexstats/internal/results"
)

type scoreRow struct {
	ScenarioID     string  `json:"scenario_id" parquet:"scenario_id"`
	Year           int     `json:"year" parquet:"year"`
	Scheme         string  `json:"scheme" parquet:"scheme"`
	Level          string  `json:"level" parquet:"level"`
	Metric         string  `json:"metric" parquet:"metric"`
	ID             string  `json:"id" parquet:"id"`
	Score          float64 `json:"score" parquet:"score"`
	Valid          bool    `json:"valid" parquet:"valid"`
	Active         bool    `json:"active" parquet:"active"`
	ModelStatus    string  `json:"model_status" parquet:"model_status"`
	Unstable       bool    `json:"unstable" parquet:"unstable"`
	ProfileID      string  `json:"profile_id" parquet:"profile_id"`
	ProfileRunID   string  `json:"profile_run_id" parquet:"profile_run_id"`
	SourceRunID    string  `json:"source_run_id" parquet:"source_run_id"`
	Method         string  `json:"method" parquet:"method"`
	GlobalRank     *int    `json:"global_rank" parquet:"global_rank,optional"`
	GlobalUniverse int     `json:"global_universe" parquet:"global_universe"`
}

type modelRanks struct {
	Year               int    `json:"year"`
	Scheme             string `json:"scheme"`
	Level              string `json:"level"`
	Metric             string `json:"metric"`
	CountryRanks       []int  `json:"country_ranks"`
	DisciplineRanks    []int  `json:"discipline_ranks"`
	CountryUniverse    int    `json:"country_universe"`
	DisciplineUniverse int    `json:"discipline_universe"`
}

func competition(scores []float64) ([]int, error) {
	indices := make([]int, len(scores))
	for i, score := range scores {
		if math.IsNaN(score) || math.IsInf(score, 0) {
			return nil, fmt.Errorf("nonfinite model score")
		}
		indices[i] = i
	}
	sort.SliceStable(indices, func(i, j int) bool { return scores[indices[i]] > scores[indices[j]] })
	ranks := make([]int, len(scores))
	for start := 0; start < len(indices); {
		end := start + 1
		for end < len(indices) && scores[indices[end]] == scores[indices[start]] {
			end++
		}
		for _, index := range indices[start:end] {
			ranks[index] = start + 1
		}
		start = end
	}
	return ranks, nil
}

func scoreExports(models []pipeline.ModelResult, sourceRunID, profileID, profileRunID, method string) ([]scoreRow, []scoreRow, []modelRanks, error) {
	var countries, disciplines []scoreRow
	var rankModels []modelRanks
	for _, m := range models {
		r := m.Result
		mr := modelRanks{Year: m.Year, Scheme: m.Scheme, Level: m.Level, Metric: m.Metric, CountryRanks: []int{}, DisciplineRanks: []int{}}
		if r.Status == "ok" {
			if len(r.ECI) != len(r.ActiveCountries) || len(r.PCI) != len(r.ActiveDisciplines) {
				return nil, nil, nil, fmt.Errorf("model scores/active indices mismatch")
			}
			var err error
			if mr.CountryRanks, err = competition(r.ECI); err != nil {
				return nil, nil, nil, err
			}
			if mr.DisciplineRanks, err = competition(r.PCI); err != nil {
				return nil, nil, nil, err
			}
			mr.CountryUniverse, mr.DisciplineUniverse = len(r.ActiveCountries), len(r.ActiveDisciplines)
		}
		for side, labels := range [][]string{r.Countries, r.Disciplines} {
			active, scores, ranks, universe := r.ActiveCountries, r.ECI, mr.CountryRanks, mr.CountryUniverse
			if side == 1 {
				active, scores, ranks, universe = r.ActiveDisciplines, r.PCI, mr.DisciplineRanks, mr.DisciplineUniverse
			}
			indices := map[int]int{}
			for position, index := range active {
				if index < 0 || index >= len(labels) {
					return nil, nil, nil, fmt.Errorf("invalid active index")
				}
				if _, exists := indices[index]; exists {
					return nil, nil, nil, fmt.Errorf("duplicate active index")
				}
				indices[index] = position
			}
			for index, id := range labels {
				row := scoreRow{ScenarioID: results.MainScenarioID, Year: m.Year, Scheme: m.Scheme, Level: m.Level, Metric: m.Metric, ID: id, ModelStatus: r.Status, Unstable: r.Unstable, ProfileID: profileID, ProfileRunID: profileRunID, SourceRunID: sourceRunID, Method: method, GlobalUniverse: universe}
				if position, ok := indices[index]; ok {
					row.Active = true
					if r.Status == "ok" {
						row.Valid = true
						row.Score = scores[position]
						rank := ranks[position]
						row.GlobalRank = &rank
					}
				}
				if side == 0 {
					countries = append(countries, row)
				} else {
					disciplines = append(disciplines, row)
				}
			}
		}
		rankModels = append(rankModels, mr)
	}
	return countries, disciplines, rankModels, nil
}

func writeScoreArtifact(root, relative string, rows []scoreRow) (artifact, error) {
	p := filepath.Join(root, relative)
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return artifact{}, err
	}
	w := parquet.NewGenericWriter[scoreRow](f, parquet.Compression(&zstd.Codec{}), parquet.MaxRowsPerRowGroup(65536))
	_, err = w.Write(rows)
	if e := w.Close(); err == nil {
		err = e
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return artifact{}, err
	}
	a, err := checkpoint.Hash(p)
	return artifact{Path: filepath.ToSlash(relative), Bytes: a.Bytes, SHA256: a.SHA256}, err
}

func compareWithBaseline(source sourceData, output computed) []comparison {
	var reports []comparison
	for _, method := range []string{"spectral-v1", "java-reflections-v1"} {
		for i, current := range output.models[method] {
			base := source.Baseline[method][i]
			report := comparison{Year: current.Year, Scheme: current.Scheme, Level: current.Level, Metric: current.Metric, Method: method}
			if current.Result.Status == "ok" && base.Result.Status == "ok" {
				old := map[string]float64{}
				for position, index := range base.Result.ActiveCountries {
					old[base.Result.Countries[index]] = base.Result.ECI[position]
				}
				var pairs [][2]float64
				for position, index := range current.Result.ActiveCountries {
					if prior, ok := old[current.Result.Countries[index]]; ok {
						pairs = append(pairs, [2]float64{prior, current.Result.ECI[position]})
					}
				}
				report.CommonCountries = len(pairs)
				if len(pairs) >= 2 {
					a, b := 0.0, 0.0
					for _, pair := range pairs {
						a += pair[0]
						b += pair[1]
					}
					a /= float64(len(pairs))
					b /= float64(len(pairs))
					aa, bb, ab := 0.0, 0.0, 0.0
					for _, pair := range pairs {
						da, db := pair[0]-a, pair[1]-b
						aa += da * da
						bb += db * db
						ab += da * db
					}
					if aa > 0 && bb > 0 {
						signed := ab / math.Sqrt(aa*bb)
						absolute := math.Abs(signed)
						report.SignedCorrelation = &signed
						report.AbsoluteCorrelation = &absolute
					}
				}
			}
			reports = append(reports, report)
		}
	}
	return reports
}
