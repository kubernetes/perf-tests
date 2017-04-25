package onlinestats

import (
	"math"
	"math/rand"
)

type Reservoir struct {
	data []float64

	n   int
	sum float64
}

func NewReservoir(capacity int) *Reservoir {
	return &Reservoir{
		data: make([]float64, 0, capacity),
	}
}

func (r *Reservoir) Push(n float64) {

	index := r.n

	r.n++

	// not enough samples yet -- add it
	if index < cap(r.data) {
		r.data = append(r.data, n)
		return
	}

	ridx := rand.Intn(r.n) // == index+1, so we're 0..index inclusive

	if ridx >= len(r.data) {
		// ignore this one
		return
	}

	// add to our sample
	old := r.data[ridx]

	r.data[ridx] = n

	r.sum -= old
	r.sum += n
}

func (r *Reservoir) Len() int {
	return len(r.data)
}

func (r *Reservoir) Mean() float64 {
	return r.sum / float64(r.Len())
}

func (r *Reservoir) Var() float64 {
	n := float64(r.Len())
	mean := r.Mean()
	l := r.Len()

	sum1 := 0.0
	sum2 := 0.0

	for i := 0; i < l; i++ {
		xm := r.data[i] - mean
		sum1 += xm * xm
		sum2 += xm
	}

	return (sum1 - (sum2*sum2)/n) / (n - 1)
}

func (r *Reservoir) Stddev() float64 {
	return math.Sqrt(r.Var())
}
