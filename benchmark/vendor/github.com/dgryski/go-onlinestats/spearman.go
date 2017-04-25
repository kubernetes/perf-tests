package onlinestats

import (
	"math"
	"sort"

	"github.com/aclements/go-moremath/mathx"
)

type sort2 struct {
	x []float64
	y []float64
}

func (s sort2) Len() int           { return len(s.x) }
func (s sort2) Less(i, j int) bool { return s.x[i] < s.x[j] }
func (s sort2) Swap(i, j int) {
	s.x[i], s.x[j] = s.x[j], s.x[i]
	s.y[i], s.y[j] = s.y[j], s.y[i]
}

// crank overwrites the entries in with their ranks
func crank(w []float64) float64 {
	j, ji, jt, n := 1, 0, 0, len(w)
	var rank float64

	var s float64

	for j < n {
		if w[j] != w[j-1] {
			w[j-1] = float64(j)
			j++
		} else {
			for jt = j + 1; jt <= n && w[jt-1] == w[j-1]; jt++ {
				// empty
			}
			rank = 0.5 * (float64(j) + float64(jt) - 1)
			for ji = j; ji <= (jt - 1); ji++ {
				w[ji-1] = rank
			}
			t := float64(jt - j)
			s += (t*t*t - t)
			j = jt
		}
	}
	if j == n {
		w[n-1] = float64(n)
	}
	return s
}

// Spearman returns the rank correlation coefficient between data1 and data2, and the associated p-value
func Spearman(data1, data2 []float64) (rs float64, p float64) {
	n := len(data1)
	wksp1, wksp2 := make([]float64, n), make([]float64, n)
	copy(wksp1, data1)
	copy(wksp2, data2)

	sort.Sort(sort2{wksp1, wksp2})
	sf := crank(wksp1)
	sort.Sort(sort2{wksp2, wksp1})
	sg := crank(wksp2)
	d := 0.0
	for j := 0; j < n; j++ {
		sq := wksp1[j] - wksp2[j]
		d += (sq * sq)
	}

	en := float64(n)
	en3n := en*en*en - en

	fac := (1.0 - sf/en3n) * (1.0 - sg/en3n)
	rs = (1.0 - (6.0/en3n)*(d+(sf+sg)/12.0)) / math.Sqrt(fac)

	if fac = (rs + 1.0) * (1.0 - rs); fac > 0 {
		t := rs * math.Sqrt((en-2.0)/fac)
		df := en - 2.0
		p = mathx.BetaInc(df/(df+t*t), 0.5*df, 0.5)
	}

	return rs, p
}

func sqr(x float64) float64 { return x * x }
