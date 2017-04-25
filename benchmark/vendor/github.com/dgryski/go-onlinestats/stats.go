// Package onlinestats provides online, one-pass algorithms for descriptive statistics.
/*

The implementation is based on the public domain code available at http://www.johndcook.com/skewness_kurtosis.html .

The linear regression code is from http://www.johndcook.com/running_regression.html .

*/
package onlinestats

import "math"

type Running struct {
	n              int
	m1, m2, m3, m4 float64
}

func NewRunning() *Running {
	return &Running{}
}

func (r *Running) Push(x float64) {

	n1 := float64(r.n)
	r.n++
	delta := x - r.m1
	delta_n := delta / float64(r.n)
	delta_n2 := delta_n * delta_n
	term1 := delta * delta_n * n1
	r.m1 += delta_n
	r.m4 += term1*delta_n2*float64(r.n*r.n-3*r.n+3) + 6*delta_n2*r.m2 - 4*delta_n*r.m3
	r.m3 += term1*delta_n*float64(r.n-2) - 3*delta_n*r.m2
	r.m2 += term1
}

func (r *Running) Len() int {
	return r.n
}

func (r *Running) Mean() float64 {
	return r.m1
}

func (r *Running) Var() float64 {
	return r.m2 / float64(r.n-1)
}

func (r *Running) Stddev() float64 {
	return math.Sqrt(r.Var())
}

func (r *Running) Skewness() float64 {
	return math.Sqrt(float64(r.n)) * r.m3 / math.Pow(r.m2, 1.5)
}

func (r *Running) Kurtosis() float64 {
	return float64(r.n)*r.m4/(r.m2*r.m2) - 3.0
}

func CombineRunning(a, b *Running) *Running {

	var combined Running

	an := float64(a.n)
	bn := float64(b.n)
	cn := float64(an + bn)

	combined.n = a.n + b.n

	delta := b.m1 - a.m1
	delta2 := delta * delta
	delta3 := delta * delta2
	delta4 := delta2 * delta2

	combined.m1 = (an*a.m1 + bn*b.m1) / cn

	combined.m2 = a.m2 + b.m2 + delta2*an*bn/cn

	combined.m3 = a.m3 + b.m3 + delta3*an*bn*(an-bn)/(cn*cn)
	combined.m3 += 3.0 * delta * (an*b.m2 - bn*a.m2) / cn

	combined.m4 = a.m4 + b.m4 + delta4*an*bn*(an*an-an*bn+bn*bn)/(cn*cn*cn)
	combined.m4 += 6.0*delta2*(an*an*b.m2+bn*bn*a.m2)/(cn*cn) + 4.0*delta*(an*b.m3-bn*a.m3)/cn

	return &combined
}

type Regression struct {
	xstats Running
	ystats Running
	sxy    float64
	n      int
}

func NewRegression() *Regression {
	return &Regression{}
}

func (r *Regression) Push(x, y float64) {
	r.sxy += (r.xstats.Mean() - x) * (r.ystats.Mean() - y) * float64(r.n) / float64(r.n+1)

	r.xstats.Push(x)
	r.ystats.Push(y)
	r.n++
}

func (r *Regression) Len() int {
	return r.n
}

func (r *Regression) Slope() float64 {
	sxx := r.xstats.Var() * float64(r.n-1)
	return r.sxy / sxx
}

func (r *Regression) Intercept() float64 {
	return r.ystats.Mean() - r.Slope()*r.xstats.Mean()
}

func (r *Regression) Correlation() float64 {
	t := r.xstats.Stddev() * r.ystats.Stddev()
	return r.sxy / (float64(r.n-1) * t)
}

func CombineRegressions(a, b Regression) *Regression {
	var combined Regression

	combined.xstats = *CombineRunning(&a.xstats, &b.xstats)
	combined.ystats = *CombineRunning(&a.ystats, &b.ystats)
	combined.n = a.n + b.n

	delta_x := b.xstats.Mean() - a.xstats.Mean()
	delta_y := b.ystats.Mean() - a.ystats.Mean()
	combined.sxy = a.sxy + b.sxy + float64(a.n*b.n)*delta_x*delta_y/float64(combined.n)

	return &combined
}
