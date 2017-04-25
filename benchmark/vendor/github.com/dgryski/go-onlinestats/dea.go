package onlinestats

import (
	"math"
)

// http://www.drdobbs.com/tools/discontiguous-exponential-averaging/184410671
type DEA struct {
	sumOfWeights            float64
	sumOfData               float64
	sumOfSquaredData        float64
	previousTime            float64
	alpha                   float64
	newDataWeightUpperBound float64
}

func NewDEA(alpha float64, maxDt float64) *DEA {
	return &DEA{
		alpha: alpha,
		newDataWeightUpperBound: 1 - math.Pow(alpha, float64(maxDt)),
	}
}

func (ew *DEA) Update(newData float64, t float64) {
	weightReductionFactor := math.Pow(ew.alpha, float64(t-ew.previousTime))
	newDataWeight := minf(1-weightReductionFactor, ew.newDataWeightUpperBound)
	ew.sumOfWeights = weightReductionFactor*ew.sumOfWeights + newDataWeight
	ew.sumOfData = weightReductionFactor*ew.sumOfData + newDataWeight*newData
	ew.sumOfSquaredData = weightReductionFactor*ew.sumOfSquaredData + (newDataWeight * (newData * newData))
	ew.previousTime = t
}

func (ew *DEA) CompletenessFraction(t float64) float64 {
	return math.Pow(ew.alpha, float64(t-ew.previousTime)*ew.sumOfWeights)
}
func (ew *DEA) Mean() float64 {
	return (ew.sumOfData / ew.sumOfWeights)
}

func (ew *DEA) Var() float64 {
	m := ew.Mean()
	return (ew.sumOfSquaredData/ew.sumOfWeights - m*m)
}

func (ew *DEA) Stddev() float64 {
	return math.Sqrt(ew.Var())
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
