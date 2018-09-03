/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"fmt"
	"math/rand"
	"strconv"
	"text/template"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// GetFuncs returns map of names to functions, that are supported by template provider.
func GetFuncs() template.FuncMap {
	return template.FuncMap{
		"RandInt":       randInt,
		"RandIntRange":  randIntRange,
		"AddInt":        addInt,
		"SubtractInt":   subtractInt,
		"MultiplyInt":   multiplyInt,
		"DivideInt":     divideInt,
		"AddFloat":      addFloat,
		"SubtractFloat": subtractFloat,
		"MultiplyFloat": multiplyFloat,
		"DivideFloat":   divideFloat,
	}
}

func toFloat64(val interface{}) float64 {
	switch i := val.(type) {
	case float64:
		return i
	case float32:
		return float64(i)
	case int64:
		return float64(i)
	case int32:
		return float64(i)
	case int:
		return float64(i)
	case uint64:
		return float64(i)
	case uint32:
		return float64(i)
	case uint:
		return float64(i)
	case string:
		f, err := strconv.ParseFloat(i, 64)
		if err == nil {
			return f
		}
	}
	panic(fmt.Sprintf("cannot cast %v to float64", val))
}

// randInt returns pseudo-random int in [0, i].
func randInt(i int) int {
	return rand.Intn(i + 1)
}

// randIntRange returns pseudo-random int in [i, j].
// If i >= j then i is returned.
func randIntRange(i, j int) int {
	if i >= j {
		return i
	}
	return i + rand.Intn(j-i+1)
}

func addInt(i, j interface{}) int {
	return int(addFloat(i, j))
}

func subtractInt(i, j interface{}) int {
	return int(subtractFloat(i, j))
}

func multiplyInt(i, j interface{}) int {
	return int(multiplyFloat(i, j))
}

func divideInt(i, j interface{}) int {
	return int(divideFloat(i, j))
}

func addFloat(i, j interface{}) float64 {
	typedI := toFloat64(i)
	typedJ := toFloat64(j)
	return typedI + typedJ
}

func subtractFloat(i, j interface{}) float64 {
	typedI := toFloat64(i)
	typedJ := toFloat64(j)
	return typedI - typedJ
}

func multiplyFloat(i, j interface{}) float64 {
	typedI := toFloat64(i)
	typedJ := toFloat64(j)
	return typedI * typedJ
}

func divideFloat(i, j interface{}) float64 {
	typedI := toFloat64(i)
	typedJ := toFloat64(j)
	return typedI / typedJ
}
