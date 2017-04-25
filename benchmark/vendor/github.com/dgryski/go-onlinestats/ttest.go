package onlinestats

import "math"

type Stats interface {
	Mean() float64
	Var() float64
	Len() int
}

// Welch implements a Welch's t-test.  Returns the probability that the difference in means is due to chance.
func Welch(xs, ys Stats) float64 {

	xvar := xs.Var()
	yvar := ys.Var()

	xn := float64(xs.Len())
	yn := float64(ys.Len())

	wvar := (xvar/xn + yvar/yn)

	vnum := wvar * wvar
	vdenom := (xvar*xvar)/(xn*xn*(xn-1)) + (yvar*yvar)/(yn*yn*(yn-1))

	// both have 0 variance, the difference is the difference of the means
	var v float64
	if vdenom == 0 {
		v = xn + yn - 2
	} else {
		v = vnum / vdenom
	}

	// if the variances are 0, we return if the means are different
	if wvar == 0 {
		if math.Abs(ys.Mean()-xs.Mean()) > 0.0000000001 {
			return 1
		} else {
			return 0
		}
	}

	t := (ys.Mean() - xs.Mean()) / math.Sqrt(wvar)

	return pt(t, v)
}

// translated from https://bitbucket.org/petermr/euclid-dev/src/9936542c1f30/src/main/java/org/xmlcml/euclid/Univariate.java

func pt(t, df float64) float64 {
	// ALGORITHM AS 3  APPL. STATIST. (1968) VOL.17, P.189
	// Computes P(T<t)
	var a, b, idf, im2, ioe, s, c, ks, fk, k float64
	const g1 = 0.3183098862 // 1/Pi
	if df < 1 {
		return 0
	}

	idf = df
	a = t / math.Sqrt(idf)
	b = idf / (idf + t*t)
	im2 = df - 2
	ioe = float64(int(idf) % 2)
	s = 1
	c = 1
	idf = 1
	ks = 2 + ioe
	fk = ks
	if im2 >= 2 {
		for k = ks; k <= im2; k += 2 {
			c = c * b * (fk - 1) / fk
			s += c
			if s != idf {
				idf = s
				fk += 2
			}
		}
	}
	if ioe != 1 {
		return 0.5 + 0.5*a*math.Sqrt(b)*s
	}
	if df == 1 {
		s = 0
	}
	return 0.5 + (a*b*s+math.Atan(a))*g1
}
