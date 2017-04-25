package onlinestats

// https://en.wikipedia.org/wiki/Mann%E2%80%93Whitney_U

import (
	"math"
	"sort"
)

// MannWhitney performs a Matt-Whitney U test for the two samples xs and ys.
// It returns the two-tailed p-value for the null hypothesis that the medians
// of the two samples are the same.  This uses the normal approximation which
// is more accurate if the number of samples is >30.
func MannWhitney(xs, ys []float64) float64 {

	// floats in a map.. this feels dubious?
	data := make(map[float64][]int)

	for _, x := range xs {
		data[x] = append(data[x], 0)
	}

	for _, y := range ys {
		data[y] = append(data[y], 1)
	}

	floats := make([]float64, 0, len(data))

	for k := range data {
		floats = append(floats, k)
	}

	sort.Float64s(floats)

	var r [2]float64

	var idx = 1
	for _, f := range floats {
		dataf := data[f]
		l := len(dataf)
		var rank float64
		if l == 1 {
			rank = float64(idx)
		} else {
			rank = float64(idx) + float64(l-1)/2.0
		}
		for _, xy := range dataf {
			r[xy] += rank
		}
		idx += l
	}

	n1n2 := len(xs) * len(ys)

	idx = 0
	u := float64(n1n2+(len(xs)*(len(xs)+1))/2.0) - r[0]
	if u1 := float64(n1n2+(len(ys)*(len(ys)+1))/2.0) - r[1]; u > u1 {
		idx = 1
		u = u1
	}

	mu := float64(n1n2) / 2.0
	sigu := math.Sqrt(float64(n1n2*(len(xs)+len(ys)+1)) / 12.0)
	zu := math.Abs(float64(u)-mu) / sigu

	return 2 - 2*cdf(0, 1, zu)
}

func cdf(mean, stddev, x float64) float64 {
	return 0.5 + 0.5*math.Erf((x-mean)/(stddev*math.Sqrt2))
}
