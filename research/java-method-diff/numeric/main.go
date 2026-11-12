// This read-only numerical comparison translates only the arithmetic in
// FzxByCtry.DdFzx_bl_year_jg and eciPci_new_jg. It never executes Java or reads
// the former application's inputs, databases, or configuration.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	"openalexstats/internal/eci"
)

type iteration struct {
	N                 int       `json:"n"`
	Country           []float64 `json:"country_raw"`
	Discipline        []float64 `json:"discipline_raw"`
	JavaCountry       []float64 `json:"java_centered_l2_country"`
	JavaDiscipline    []float64 `json:"java_centered_l2_discipline"`
	PopulationCountry []float64 `json:"population_z_country"`
}

func normalize(v []float64, population bool) []float64 {
	m := 0.0
	for _, x := range v {
		m += x
	}
	m /= float64(len(v))
	sq := 0.0
	for _, x := range v {
		sq += (x - m) * (x - m)
	}
	if population {
		sq /= float64(len(v))
	}
	s := math.Sqrt(sq)
	if s == 0 {
		return nil
	}
	r := make([]float64, len(v))
	for i, x := range v {
		r[i] = (x - m) / s
	}
	return r
}

func main() {
	x := eci.Matrix{Countries: []string{"A", "B", "C"}, Disciplines: []string{"x", "y", "z"}, Values: [][]float64{{10, 1, 1}, {5, 5, 1}, {1, 5, 10}}}
	r, err := eci.Compute(x)
	if err != nil || r.Status != "ok" {
		panic(fmt.Sprintf("%+v %v", r, err))
	}
	kc, kp := make([]float64, 3), make([]float64, 3)
	for i := range kc {
		for j := range kp {
			if r.Advantage[i][j] {
				kc[i]++
				kp[j]++
			}
		}
	}
	c, p := append([]float64(nil), kc...), append([]float64(nil), kp...)
	its := []iteration{}
	for n := 0; n <= 16; n++ {
		its = append(its, iteration{n, c, p, normalize(c, false), normalize(p, false), normalize(c, true)})
		nc, np := make([]float64, 3), make([]float64, 3)
		for i := range c {
			for j := range p {
				if r.Advantage[i][j] {
					nc[i] += p[j] / kc[i]
					np[j] += c[i] / kp[j]
				}
			}
		}
		c, p = nc, np
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(struct {
		Input      eci.Matrix  `json:"input"`
		Go         eci.Result  `json:"go_spectral"`
		Iterations []iteration `json:"java_reflections"`
	}{x, r, its}); err != nil {
		panic(err)
	}
}
