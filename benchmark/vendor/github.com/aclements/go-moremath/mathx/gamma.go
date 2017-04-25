// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mathx

import "math"

// GammaInc returns the value of the incomplete gamma function (also
// known as the regularized gamma function):
//
//   P(a, x) = 1 / Γ(a) * ∫₀ˣ exp(-t) t**(a-1) dt
func GammaInc(a, x float64) float64 {
	// Based on Numerical Recipes in C, section 6.2.

	if a <= 0 || x < 0 || math.IsNaN(a) || math.IsNaN(x) {
		return math.NaN()
	}

	if x < a+1 {
		// Use the series representation, which converges more
		// rapidly in this range.
		return gammaIncSeries(a, x)
	} else {
		// Use the continued fraction representation.
		return 1 - gammaIncCF(a, x)
	}
}

// GammaIncComp returns the complement of the incomplete gamma
// function 1 - GammaInc(a, x). This is more numerically stable for
// values near 0.
func GammaIncComp(a, x float64) float64 {
	if a <= 0 || x < 0 || math.IsNaN(a) || math.IsNaN(x) {
		return math.NaN()
	}

	if x < a+1 {
		return 1 - gammaIncSeries(a, x)
	} else {
		return gammaIncCF(a, x)
	}
}

func gammaIncSeries(a, x float64) float64 {
	const maxIterations = 200
	const epsilon = 3e-14

	if x == 0 {
		return 0
	}

	ap := a
	del := 1 / a
	sum := del
	for n := 0; n < maxIterations; n++ {
		ap++
		del *= x / ap
		sum += del
		if math.Abs(del) < math.Abs(sum)*epsilon {
			return sum * math.Exp(-x+a*math.Log(x)-lgamma(a))
		}
	}
	panic("a too large; failed to converge")
}

func gammaIncCF(a, x float64) float64 {
	const maxIterations = 200
	const epsilon = 3e-14

	raiseZero := func(z float64) float64 {
		if math.Abs(z) < math.SmallestNonzeroFloat64 {
			return math.SmallestNonzeroFloat64
		}
		return z
	}

	b := x + 1 - a
	c := math.MaxFloat64
	d := 1 / b
	h := d

	for i := 1; i <= maxIterations; i++ {
		an := -float64(i) * (float64(i) - a)
		b += 2
		d = raiseZero(an*d + b)
		c = raiseZero(b + an/c)
		d = 1 / d
		del := d * c
		h *= del
		if math.Abs(del-1) < epsilon {
			return math.Exp(-x+a*math.Log(x)-lgamma(a)) * h
		}
	}
	panic("a too large; failed to converge")
}
