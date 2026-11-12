package web

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	"openalexstats/internal/eci"
)

type Query struct {
	Method      string   `json:"method"`
	Corpus      string   `json:"corpus"`
	Scenario    string   `json:"scenario"`
	Scheme      string   `json:"scheme"`
	Level       string   `json:"level"`
	Discipline  string   `json:"discipline"`
	Disciplines []string `json:"disciplines"`
	Countries   []string `json:"countries"`
	Metric      string   `json:"metric"`
	Basis       string   `json:"basis"`
	Start       int      `json:"start"`
	End         int      `json:"end"`
}
type Point struct {
	Year         int      `json:"year"`
	Value        *float64 `json:"value"`
	Status       string   `json:"status"`
	Partial      bool     `json:"partial"`
	Rank         *int     `json:"rank"`
	RankUniverse int      `json:"rank_universe"`
}
type Series struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Country        string  `json:"country"`
	CountryName    string  `json:"country_name"`
	Discipline     string  `json:"discipline"`
	DisciplineName string  `json:"discipline_name"`
	Points         []Point `json:"points"`
}

const MaxSeries = 48

type Response struct {
	Query         Query      `json:"query"`
	Attribution   string     `json:"attribution"`
	Definition    string     `json:"definition"`
	Series        []Series   `json:"series"`
	Counting      Counting   `json:"counting"`
	Method        MethodInfo `json:"method"`
	MethodApplies bool       `json:"method_applies"`
}

type Counting struct {
	CountryMode              string  `json:"country_mode"`
	ConceptMode              string  `json:"concept_mode"`
	ConceptThreshold         float64 `json:"concept_threshold"`
	ConceptThresholdApplies  bool    `json:"concept_threshold_applies"`
	PublicationCountsInteger bool    `json:"publication_counts_integer"`
}

func (d *dataset) counting(q Query) (Counting, string) {
	var c Counting
	for _, s := range d.scenarios {
		if s.ID == q.Scenario {
			c = Counting{CountryMode: s.CountryMode, ConceptMode: s.ConceptMode, ConceptThreshold: s.ConceptThreshold, ConceptThresholdApplies: q.Scheme == "concepts", PublicationCountsInteger: s.CountryMode == "whole" && (q.Scheme == "topics" || s.ConceptMode == "whole")}
			break
		}
	}
	country := "国家均分：各归属国分得 1/N，论文量仍可为小数。"
	if c.CountryMode == "whole" {
		country = "国家全计数：每个归属国记 1。"
	}
	if q.Scheme == "topics" {
		return c, country + "Topics 按主主题映射至当前层级；Concepts 阈值不适用。"
	}
	concept := "Concepts 同层命中标签均分（旧口径）。"
	if c.ConceptMode == "whole" {
		concept = "Concepts 同层去重后每个命中标签记 1，不在标签间均分。"
	}
	if c.PublicationCountsInteger {
		concept += "当前论文量为整数计数。"
	}
	return c, country + concept + fmt.Sprintf("Concepts score ≥ %g。", c.ConceptThreshold)
}

func (d *dataset) parseQuery(params url.Values) (Query, error) {
	allowed := map[string]bool{"method": true, "scenario": true, "scheme": true, "level": true, "discipline": true, "disciplines": true, "countries": true, "metric": true, "basis": true, "start": true, "end": true}
	for k, values := range params {
		if !allowed[k] || len(values) != 1 {
			return Query{}, fmt.Errorf("未知或重复参数：%s", k)
		}
	}
	q := Query{Scenario: params.Get("scenario"), Scheme: params.Get("scheme"), Level: params.Get("level"), Discipline: params.Get("discipline"), Metric: params.Get("metric"), Basis: params.Get("basis"), Start: max(2000, d.start), End: min(2025, d.end)}
	q.Method = params.Get("method")
	if q.Method == "" {
		q.Method = DefaultMethod
	}
	if _, ok := d.method(q.Method); !ok {
		return q, fmt.Errorf("未知或未提供的知识复杂度方法")
	}
	if q.Scenario == "" {
		q.Scenario = d.eciScenario
	}
	if !d.scenarioSet[q.Scenario] {
		return q, fmt.Errorf("未知统计场景")
	}
	if !validLayer(q.Scheme, q.Level) {
		return q, fmt.Errorf("无效分类层级；Topics使用field/subfield，Concepts使用0/1")
	}
	if q.Metric == "" {
		q.Metric = "publications"
	}
	if q.Basis == "" {
		q.Basis = "publications"
	}
	if q.Basis != "publications" && q.Basis != "citations" {
		return q, fmt.Errorf("basis只能是publications或citations")
	}
	if q.Metric != "publications" && q.Metric != "citations" && q.Metric != "share" && q.Metric != "rca" && q.Metric != "eci" && q.Metric != "pci" {
		return q, fmt.Errorf("未知指标")
	}
	if _, multi := params["disciplines"]; multi {
		if _, single := params["discipline"]; single {
			return q, fmt.Errorf("discipline与disciplines不能同时提供")
		}
		q.Discipline = params.Get("disciplines")
		if q.Discipline == "" {
			return q, fmt.Errorf("disciplines不能为空")
		}
	} else if strings.Contains(q.Discipline, ",") {
		return q, fmt.Errorf("单学科参数discipline不能包含多个学科，请使用disciplines")
	}
	seenDisciplines := map[string]bool{}
	if q.Discipline != "" {
		for _, id := range strings.Split(q.Discipline, ",") {
			if !d.disciplineSet[q.Scheme+"/"+q.Level][id] {
				return q, fmt.Errorf("学科参数不属于当前层级：%s", id)
			}
			if !seenDisciplines[id] {
				q.Disciplines = append(q.Disciplines, id)
				seenDisciplines[id] = true
			}
		}
	}
	if len(q.Disciplines) == 0 && q.Metric != "eci" {
		return q, fmt.Errorf("请选择本层级中有效的学科")
	}
	if len(q.Disciplines) > MaxSeries {
		return q, fmt.Errorf("最多选择%d个学科", MaxSeries)
	}
	q.Discipline = ""
	if len(q.Disciplines) > 0 {
		q.Discipline = q.Disciplines[0]
	}
	if q.Metric != "pci" {
		seen := map[string]bool{}
		for _, c := range strings.Split(params.Get("countries"), ",") {
			if c == "" {
				continue
			}
			if !d.countrySet[c] {
				return q, fmt.Errorf("未知国家／地区代码：%s", c)
			}
			if !seen[c] {
				q.Countries = append(q.Countries, c)
				seen[c] = true
			}
		}
		limit := 12
		if q.Metric == "eci" {
			limit = len(d.countries)
		}
		if len(q.Countries) == 0 || len(q.Countries) > limit {
			return q, fmt.Errorf("请选择1到%d个国家／地区", limit)
		}
	}
	curves := len(q.Countries) * len(q.Disciplines)
	if q.Metric == "eci" {
		curves = len(q.Countries)
	}
	if q.Metric == "pci" {
		curves = len(q.Disciplines)
	}
	if q.Metric != "eci" && curves > MaxSeries {
		return q, fmt.Errorf("当前选择产生%d条曲线，最多%d条；请减少国家或学科", curves, MaxSeries)
	}
	for key, target := range map[string]*int{"start": &q.Start, "end": &q.End} {
		if raw := params.Get(key); raw != "" {
			year, err := strconv.Atoi(raw)
			if err != nil {
				return q, fmt.Errorf("无效年份：%s", key)
			}
			*target = year
		}
	}
	if q.Start < max(2000, d.start) || q.End > d.end || q.End < q.Start || q.End-q.Start > 200 {
		return q, fmt.Errorf("年份必须位于%d—%d", max(2000, d.start), d.end)
	}
	return q, nil
}

func (d *dataset) query(q Query) Response {
	if q.Method == "" {
		q.Method = DefaultMethod
	}
	r := Response{Query: q, Attribution: "country_and_discipline_of_published_work", Series: []Series{}}
	r.Method, _ = d.method(q.Method)
	r.MethodApplies = q.Metric == "eci" || q.Metric == "pci"
	if q.Metric == "citations" || ((q.Metric == "share" || q.Metric == "rca") && q.Basis == "citations") {
		r.Attribution = "country_and_discipline_of_cited_work"
	}
	if q.Metric == "eci" {
		r.Attribution = "country_over_all_disciplines_in_layer"
		r.Definition = "国家ECI由当前分类层级的全部学科计算；所选学科不参与筛选。无效模型以缺口表示。"
	} else if q.Metric == "pci" {
		r.Attribution = "global_discipline"
		r.Definition = "学科PCI是每年一个全球值，不按国家生成不同数值。无效模型以缺口表示。"
	} else if q.Metric == "share" {
		r.Definition = "所选国家在该学科的计数占同年该学科已知国家计数之比；UNKNOWN不进入分母。"
	} else if q.Metric == "rca" {
		r.Definition = "RCA=(本国该学科/本国全部已知学科)/(已知国家该学科/已知国家全部已知学科)。"
	} else if q.Metric == "citations" {
		r.Definition = "仅统计施引与被引文献都在圈定集合内的引用，按施引发表年计时，归属到被引文献。"
	} else {
		r.Attribution = "country_and_discipline_of_published_work"
		r.Definition = "圈定范围内当年发表的文献量，按当前场景的国家及学科权重计数。"
	}
	var countingDefinition string
	r.Counting, countingDefinition = d.counting(q)
	r.Definition += " " + countingDefinition
	if r.MethodApplies {
		r.Definition += " 知识复杂度方法：" + r.Method.DisplayName + "。"
		if r.Method.CountryIterations > 0 {
			r.Definition += fmt.Sprintf("国家 %d 轮、学科 %d 轮；%s 标准化，共用同一 RCA 优势网络。", r.Method.CountryIterations, r.Method.DisciplineIterations, r.Method.Normalization)
		}
	}
	countries := q.Countries
	disciplines := q.Disciplines
	if len(disciplines) == 0 && q.Discipline != "" {
		disciplines = []string{q.Discipline}
	}
	if q.Metric == "eci" {
		disciplines = []string{""}
	}
	if q.Metric == "pci" {
		countries = []string{"GLOBAL"}
	}
	for _, country := range countries {
		for _, discipline := range disciplines {
			name := country
			for _, c := range d.countries {
				if c.ID == country {
					name = c.DisplayName
					break
				}
			}
			if country == "GLOBAL" {
				name = "全球学科 PCI"
			}
			disciplineName := discipline
			for _, layer := range d.layers {
				if layer.Scheme == q.Scheme && layer.Level == q.Level {
					for _, label := range layer.Labels {
						if label.ID == discipline {
							disciplineName = label.DisplayName
							break
						}
					}
				}
			}
			s := Series{ID: country, Name: name, Country: country, CountryName: name, Discipline: discipline, DisciplineName: disciplineName, Points: []Point{}}
			if len(disciplines) > 1 {
				s.ID = country + "::" + discipline
				s.Name = name + " · " + disciplineName
			}
			for y := q.Start; y <= q.End; y++ {
				p := Point{Year: y, Status: "ok", Partial: d.partialYears[y]}
				if q.Metric == "eci" || q.Metric == "pci" {
					model, ok := d.methodModels[q.Method][modelKey{q.Scenario, q.Scheme, q.Level, q.Basis, y}]
					if !ok {
						p.Status = "model_not_available"
					} else if model.Status != "ok" {
						p.Status = model.Status
					} else {
						_, p.RankUniverse = modelRank(model, 0, q.Metric)
						value, found := modelScore(model, country, discipline, q.Metric)
						if found && !math.IsNaN(value) && !math.IsInf(value, 0) {
							p.Value = &value
							rank, _ := modelRank(model, value, q.Metric)
							p.Rank = &rank
							if model.Unstable {
								p.Status = "unstable"
							}
						} else {
							p.Status = "inactive"
						}
					}
				} else {
					v := d.rows[pointKey{q.Scenario, q.Scheme, q.Level, discipline, country, y}]
					value := v.get(q.Metric)
					if q.Metric == "share" || q.Metric == "rca" {
						value = v.get(q.Basis)
						col := d.disciplineMargins[marginKey{q.Scenario, q.Scheme, q.Level, discipline, y}].get(q.Basis)
						if col <= 0 {
							p.Status = "no_denominator"
						} else if q.Metric == "share" {
							value /= col
						} else {
							row := d.countryMargins[marginKey{q.Scenario, q.Scheme, q.Level, country, y}].get(q.Basis)
							total := d.totalMargins[marginKey{q.Scenario, q.Scheme, q.Level, "", y}].get(q.Basis)
							if row <= 0 || total <= 0 {
								p.Status = "no_denominator"
							} else {
								value = (value / row) / (col / total)
							}
						}
					}
					if p.Status == "ok" {
						p.Value = &value
					}
				}
				s.Points = append(s.Points, p)
			}
			r.Series = append(r.Series, s)
		}
	}
	return r
}

// modelRank is global to every valid active entity in this model, independent
// of the selected countries. Equal scores share competition ranks.
func modelRank(model eci.Result, value float64, metric string) (rank, universe int) {
	scores, active, labels := model.ECI, model.ActiveCountries, model.Countries
	if metric == "pci" {
		scores, active, labels = model.PCI, model.ActiveDisciplines, model.Disciplines
	}
	rank = 1
	for i, ordinal := range active {
		if i >= len(scores) || ordinal < 0 || ordinal >= len(labels) || math.IsNaN(scores[i]) || math.IsInf(scores[i], 0) {
			continue
		}
		universe++
		if scores[i] > value {
			rank++
		}
	}
	return
}

func modelScore(model eci.Result, country, discipline, metric string) (float64, bool) {
	if metric == "eci" {
		for i, ordinal := range model.ActiveCountries {
			if ordinal >= 0 && ordinal < len(model.Countries) && model.Countries[ordinal] == country && i < len(model.ECI) {
				return model.ECI[i], true
			}
		}
	} else {
		for i, ordinal := range model.ActiveDisciplines {
			if ordinal >= 0 && ordinal < len(model.Disciplines) && model.Disciplines[ordinal] == discipline && i < len(model.PCI) {
				return model.PCI[i], true
			}
		}
	}
	return 0, false
}
