package onlinestats

import "sort"

// Tukey returns the slice (sorted) with all the 1.5 IQR outliers removed
func Tukey(data []float64) []float64 {

	sort.Float64s(data)

	first := int(0.25 * float64(len(data)))
	third := int(0.75 * float64(len(data)))

	iqr := data[third] - data[first]

	min := data[first] - 1.5*iqr
	max := data[third] + 1.5*iqr

	// TODO(dgryski): our data is sorted; we should be able to trim these elements faster than O(n)
	result := make([]float64, 0, len(data))
	for _, r := range data {
		if r >= min && r <= max {
			result = append(result, r)
		}
	}

	return result
}
