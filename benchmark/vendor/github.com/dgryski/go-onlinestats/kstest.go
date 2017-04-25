package onlinestats

// https://en.wikipedia.org/wiki/Kolmogorov%E2%80%93Smirnov_test
// http://www.itl.nist.gov/div898/handbook/eda/section3/eda35g.htm

import (
	"math"
	"sort"
)

// From NR

// KS performs a Kolmogorov-Smirnov test for the two datasets, and returns the
// p-value for the null hypothesis that the two sets come from the same distribution.
func KS(data1, data2 []float64) float64 {

	sort.Float64s(data1)
	sort.Float64s(data2)

	for math.IsNaN(data1[0]) {
		data1 = data1[1:]
	}

	for math.IsNaN(data2[0]) {
		data2 = data2[1:]
	}

	n1, n2 := len(data1), len(data2)
	en1, en2 := float64(n1), float64(n2)

	var d float64
	var fn1, fn2 float64

	j1, j2 := 0, 0
	for j1 < n1 && j2 < n2 {
		d1 := data1[j1]
		d2 := data2[j2]

		if d1 <= d2 {
			for j1 < n1 && d1 == data1[j1] {
				j1++
				fn1 = float64(j1) / en1
			}
		}

		if d2 <= d1 {
			for j2 < n2 && d2 == data2[j2] {
				j2++
				fn2 = float64(j2) / en2
			}
		}

		if dt := math.Abs(fn2 - fn1); dt > d {
			d = dt
		}

	}
	en := math.Sqrt((en1 * en2) / (en1 + en2))
	// R and Octave don't use this approximation that NR does
	//return qks((en + 0.12 + 0.11/en) * d)
	return qks(en * d)
}

func qks(z float64) float64 {

	if z < 0. {
		panic("bad z in qks")
	}

	if z == 0. {
		return 1.
	}
	if z < 1.18 {
		return 1. - pks(z)
	}
	x := math.Exp(-2. * (z * z))
	return 2. * (x - math.Pow(x, 4) + math.Pow(x, 9))
}

func pks(z float64) float64 {

	if z < 0. {
		panic("bad z in KSdist")
	}
	if z == 0. {
		return 0.
	}
	if z < 1.18 {
		y := math.Exp(-1.23370055013616983 / (z * z))
		return 2.25675833419102515 * math.Sqrt(-math.Log(y)) * (y + math.Pow(y, 9) + math.Pow(y, 25) + math.Pow(y, 49))
	}

	x := math.Exp(-2. * (z * z))
	return 1. - 2.*(x-math.Pow(x, 4)+math.Pow(x, 9))
}
