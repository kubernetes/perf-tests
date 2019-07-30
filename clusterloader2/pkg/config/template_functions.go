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
	"io/ioutil"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
	"time"

	"gopkg.in/yaml.v2"
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
		"MaxInt":        maxInt,
		"MinInt":        minInt,
		"MaxFloat":      maxFloat,
		"MinFloat":      minFloat,
		"Mod":           mod,
		"DefaultParam":  defaultParam,
		"IncludeFile":   includeFile,
		"YamlQuote":     yamlQuote,
		"Seq":           seq,
	}
}

func seq(size interface{}) []int {
	return make([]int, int(toFloat64(size)))
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
func randInt(i interface{}) int {
	typedI := int(toFloat64(i))
	return rand.Intn(typedI + 1)
}

// randIntRange returns pseudo-random int in [i, j].
// If i >= j then i is returned.
func randIntRange(i, j interface{}) int {
	typedI := int(toFloat64(i))
	typedJ := int(toFloat64(j))
	if typedI >= typedJ {
		return typedI
	}
	return typedI + rand.Intn(typedJ-typedI+1)
}

func addInt(numbers ...interface{}) int {
	return int(addFloat(numbers...))
}

func subtractInt(i, j interface{}) int {
	return int(subtractFloat(i, j))
}

func multiplyInt(numbers ...interface{}) int {
	return int(multiplyFloat(numbers...))
}

func divideInt(i, j interface{}) int {
	return int(divideFloat(i, j))
}

func addFloat(numbers ...interface{}) float64 {
	sum := 0.0
	for _, number := range numbers {
		sum += toFloat64(number)
	}
	return sum
}

func subtractFloat(i, j interface{}) float64 {
	typedI := toFloat64(i)
	typedJ := toFloat64(j)
	return typedI - typedJ
}

func multiplyFloat(numbers ...interface{}) float64 {
	product := 1.0
	for _, number := range numbers {
		product *= toFloat64(number)
	}
	return product
}

func divideFloat(i, j interface{}) float64 {
	typedI := toFloat64(i)
	typedJ := toFloat64(j)
	return typedI / typedJ
}

func maxInt(numbers ...interface{}) int {
	return int(maxFloat(numbers...))
}

func minInt(numbers ...interface{}) int {
	return int(minFloat(numbers...))
}

func maxFloat(numbers ...interface{}) float64 {
	if len(numbers) == 0 {
		panic("maximum undefined")
	}
	max := toFloat64(numbers[0])
	for _, number := range numbers {
		max = math.Max(max, toFloat64(number))
	}
	return max
}

func minFloat(numbers ...interface{}) float64 {
	if len(numbers) == 0 {
		panic("minimum undefined")
	}
	min := toFloat64(numbers[0])
	for _, number := range numbers {
		min = math.Min(min, toFloat64(number))
	}
	return min
}

func mod(a interface{}, b interface{}) int {
	return int(toFloat64(a)) % int(toFloat64(b))
}

func defaultParam(param, defaultValue interface{}) interface{} {
	if param == nil {
		return defaultValue
	}
	return param
}

// includeFile reads file. 'file' is relative to ./clusterloader2 binary.
func includeFile(file interface{}) (string, error) {
	fileStr, ok := file.(string)
	if !ok {
		return "", fmt.Errorf("incorrect argument type: got: %T want: string", file)
	}

	ex, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("unable to determine executable path: %v", err)
	}

	path := filepath.Join(filepath.Dir(ex), fileStr)
	data, err := ioutil.ReadFile(path)
	return string(data), nil
}

// yamlQuote quotes yaml string aligning each output lin by tabsInt.
func yamlQuote(strInt interface{}, tabsInt interface{}) (string, error) {
	str, ok := strInt.(string)
	if !ok {
		return "", fmt.Errorf("incorrect argument type: got: %T want: string", strInt)
	}
	tabs, ok := tabsInt.(int)
	if !ok {
		return "", fmt.Errorf("incorrect argument type: got: %T want: int", tabsInt)
	}
	tabsStr := strings.Repeat("  ", tabs)
	b, err := yaml.Marshal(&str)
	// TODO(oxddr): change back to strings.ReplaceAll once we figure out how to compile clusterloader2
	// with newer versions of go for tests against stable version of K8s
	return strings.Replace(string(b), "\n", "\n"+tabsStr, -1), err
}
