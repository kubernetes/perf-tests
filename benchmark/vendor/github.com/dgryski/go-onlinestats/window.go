package onlinestats

// add http://www.itl.nist.gov/div898/handbook/eda/section3/eda35b.htm

import "math"

type Windowed struct {
	data []float64
	head int

	length int
	sum    float64
}

func NewWindowed(capacity int) *Windowed {
	return &Windowed{
		data: make([]float64, capacity),
	}
}

func (w *Windowed) Push(n float64) float64 {
	old := w.data[w.head]

	w.length++

	w.data[w.head] = n
	w.head++
	if w.head >= len(w.data) {
		w.head = 0
	}

	w.sum -= old
	w.sum += n

	return old
}

func (w *Windowed) Len() int {
	if w.length < len(w.data) {
		return w.length
	}

	return len(w.data)
}

func (w *Windowed) Mean() float64 {
	return w.sum / float64(w.Len())
}

func (w *Windowed) Var() float64 {
	n := float64(w.Len())
	mean := w.Mean()
	l := w.Len()

	sum1 := 0.0
	sum2 := 0.0

	for i := 0; i < l; i++ {
		xm := w.data[i] - mean
		sum1 += xm * xm
		sum2 += xm
	}

	return (sum1 - (sum2*sum2)/n) / (n - 1)
}

func (w *Windowed) Stddev() float64 {
	return math.Sqrt(w.Var())
}

// Set sets the current windowed data.  The length is reset, and Push() will start overwriting the first element of the array.
func (w *Windowed) Set(data []float64) {
	if w.data != nil {
		w.data = w.data[:0]
	}

	w.data = append(w.data, data...)

	w.sum = 0
	for _, v := range w.data {
		w.sum += v
	}

	w.head = 0
	w.length = len(data)
}
