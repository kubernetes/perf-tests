// Copyright 2015 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mathx

// Sign returns the sign of x: -1 if x < 0, 0 if x == 0, 1 if x > 0.
// If x is NaN, it returns NaN.
func Sign(x float64) float64 {
	if x == 0 {
		return 0
	} else if x < 0 {
		return -1
	} else if x > 0 {
		return 1
	}
	return nan
}
