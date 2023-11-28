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
	"io/fs"
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
	"k8s.io/klog/v2"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// GetFuncs returns map of names to functions, that are supported by template provider.
func GetFuncs(fsys fs.FS) template.FuncMap {
	return template.FuncMap{
		"AddFloat":         addFloat,
		"AddInt":           addInt,
		"Ceil":             ceil,
		"DefaultParam":     defaultParam,
		"DivideFloat":      divideFloat,
		"DivideInt":        divideInt,
		"ExpandEnv":        expandEnv,
		"IfThenElse":       ifThenElse,
		"IncludeFile":      includeFile,
		"IncludeEmbedFile": includeEmbedFile(fsys),
		"Loop":             loop,
		"Lowercase":        strings.ToLower,
		"MaxFloat":         maxFloat,
		"MaxInt":           maxInt,
		"MinFloat":         minFloat,
		"MinInt":           minInt,
		"Mod":              mod,
		"MultiplyFloat":    multiplyFloat,
		"MultiplyInt":      multiplyInt,
		"RandData":         randData,
		"RandInt":          randInt,
		"RandIntRange":     randIntRange,
		"Seq":              seq,
		"SliceOfZeros":     sliceOfZeros,
		"StringSplit":      stringSplit,
		"SubtractFloat":    subtractFloat,
		"SubtractInt":      subtractInt,
		"YamlQuote":        yamlQuote,
	}
}

// seq returns a slice of size 'size' filled with zeros.
// Deprecated: Naming generates confusion. Please use 'SliceOfZeros' for explicit zero values or 'Loop' for incremental integer generation.
func seq(size interface{}) []int {
	klog.Warningf("Seq is deprecated. Instead please use 'SliceOfZeros' to replicate current behaviour or 'Loop' for simple incremential integer generation.")
	return sliceOfZeros(size)
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

// randData returns pseudo-random string of i length.
func randData(i interface{}) string {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	typedI := int(toFloat64(i))
	b := make([]byte, typedI)
	for i := range b {
		b[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(b)
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
	if typedJ == 0 {
		panic("division by zero")
	}
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

// includeFile reads file. If the path is not absolute, 'file' is relative to
// clusterloader source file path "$GOPATH/src/k8s.io/perf-tests/clusterloader2"
func includeFile(file interface{}) (string, error) {
	fileStr, ok := file.(string)
	if !ok {
		return "", fmt.Errorf("incorrect argument type: got: %T want: string", file)
	}
	if !filepath.IsAbs(fileStr) {
		defaultIncludeFilepath, err := filepath.Abs(filepath.Dir(os.Args[0]))
		if err == nil {
			fileStr = filepath.Join(defaultIncludeFilepath, fileStr)
		}
	}
	fileStr = os.ExpandEnv(fileStr)
	data, err := ioutil.ReadFile(fileStr)
	if err != nil {
		return "", fmt.Errorf("unable to read file: %v", err)
	}
	return string(data), nil
}

// includeEmbedFile reads file from the embed filesystem. The meaning of the "embed filesystem"
// depends on the context, e.g.:
// * in CL2 built-in manifests it's a filesystem with other manifests.
// * in test config, it's a directory with a test config with all subdirs.
func includeEmbedFile(fsys fs.FS) func(file interface{}) (string, error) {
	return func(file interface{}) (string, error) {
		fileStr, ok := file.(string)
		if !ok {
			return "", fmt.Errorf("incorrect argument type: got: %T want: string", file)
		}
		data, err := fs.ReadFile(fsys, fileStr)
		if err != nil {
			return "", fmt.Errorf("unable to read file: %v", err)
		}
		return string(data), nil
	}
}

func expandEnv(param interface{}) string {
	paramStr, ok := param.(string)
	if !ok {
		panic("expandEnv parameter is not a string")
	}
	return os.ExpandEnv(paramStr)
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

func ifThenElse(conditionVal interface{}, thenVal interface{}, elseVal interface{}) (interface{}, error) {
	condition, ok := conditionVal.(bool)
	if !ok {
		return nil, fmt.Errorf("incorrect argument type: got: %T want: bool", conditionVal)
	}
	if condition {
		return thenVal, nil
	}
	return elseVal, nil
}

// sliceOfZeros returns a slice of a constant size all filed with zero.
//
// In-place replacement for deprecated 'Seq'.
func sliceOfZeros(size interface{}) []int {
	return make([]int, int(toFloat64(size)))
}

// stringSplit splits a string by commas.
func stringSplit(s string) []string {
	return strings.Split(s, ",")
}

// loop returns a slice with incremential values starting from zero.
func loop(size interface{}) []int {
	sizeInt := int(toFloat64(size))
	slice := make([]int, sizeInt)
	for i := range slice {
		slice[i] = i
	}
	return slice
}

func ceil(i interface{}) float64 {
	return math.Ceil(toFloat64(i))
}
