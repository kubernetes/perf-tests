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
	"math/rand"
	"text/template"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// GetFuncs returns map of names to functions, that are supported by template provider.
func GetFuncs() template.FuncMap {
	return template.FuncMap{
		"RandInt":      randInt,
		"RandIntRange": randIntRange,
	}
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
