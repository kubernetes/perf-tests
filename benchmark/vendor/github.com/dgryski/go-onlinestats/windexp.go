package onlinestats

import "math"

// WindExp maintains a window of values, followed by a exponentially-weighted
// region for the items that have left the window.  This is similar to the
// Average Loss Interval from "Equation-Based Congestion Control for Unicast
// Applications" ( http://www.icir.org/tfrc/tcp-friendly.pdf ), but with lower
// update cost.  The advantage is that this maintains more local history, but
// doesn't have the large changes when items leave the window.
type WindExp struct {
	n   int
	w   *Windowed
	exp *ExpWeight
}

// NewWindExp returns WindExp with the specified window capacity and exponential alpha
func NewWindExp(capacity int, alpha float64) *WindExp {
	return &WindExp{
		w:   NewWindowed(capacity),
		exp: NewExpWeight(alpha),
	}
}

func (we *WindExp) Push(n float64) {

	old := we.w.Push(n)

	if we.n >= len(we.w.data) {
		we.exp.Push(old)
	}

	we.n++
}

func (we *WindExp) Len() int {
	return we.n
}

func (we *WindExp) Mean() float64 {
	// The math says this should be correct

	if we.n <= len(we.w.data) {
		return we.w.Mean()
	}

	return (we.w.sum + we.exp.Mean()) / float64(we.w.Len()+1)

}

func (we *WindExp) Var() float64 {
	// http://www.emathzone.com/tutorials/basic-statistics/combined-variance.html

	cmean := we.Mean()

	n1 := float64(we.w.Len())
	s1 := we.w.Var()
	x1xc := we.w.Mean() - cmean

	n2 := float64(we.exp.Len())
	s2 := we.exp.Var()
	x2xc := we.exp.Mean() - cmean

	cvar := (n1*(s1+x1xc*x1xc) + n2*(s2+x2xc*x2xc)) / (n1 + n2)

	return cvar

}

func (we *WindExp) Stddev() float64 {
	return math.Sqrt(we.Var())
}
