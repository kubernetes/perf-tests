/*
Copyright 2022 The Kubernetes Authors.

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

package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func init() {
	flag.Usage = func() {
		prog := os.Args[0]
		fmt.Printf("Usage: %s [DURATION]\n", prog)
		fmt.Println("DURATION is a sequence of decimal numbers, each with optional fraction and a unit suffix.")
		fmt.Println("Valid time units are ns, us (or µs), ms, s, m, h.")
	}
}

func main() {
	flag.Parse()
	input := flag.Arg(0)
	var duration time.Duration
	if input != "" {
		var err error
		duration, err = time.ParseDuration(input)
		if err != nil {
			fmt.Println(err)
			flag.Usage()
			os.Exit(1)
		}
	}
	time.Sleep(duration)
}
