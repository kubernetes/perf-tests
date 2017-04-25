package onlinestats

import "math"

func SumSq(a []float64) float64 {
	var t float64
	for _, aa := range a {
		t += (aa * aa)
	}
	return t
}

func Sum(a []float64) float64 {
	var t float64
	for _, aa := range a {
		t += aa
	}
	return t
}

func Mean(a []float64) float64 {
	return Sum(a) / float64(len(a))
}

func variance(a []float64, sample bool) float64 {

	var adjustN float64

	if sample {
		adjustN = -1
	}

	m := Mean(a)
	var sum2 float64
	var sum3 float64
	for _, aa := range a {
		sum2 += (aa - m) * (aa - m)
		sum3 += (aa - m)
	}
	n := float64(len(a))
	return (sum2 - (sum3*sum3)/n) / (n + adjustN)
}

func Variance(a []float64) float64 {
	return variance(a, false)
}

func SampleVariance(a []float64) float64 {
	return variance(a, true)
}

func SampleStddev(a []float64) float64 {
	return math.Sqrt(SampleVariance(a))
}
