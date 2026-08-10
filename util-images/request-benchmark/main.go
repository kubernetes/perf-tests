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
	"fmt"
	"log"
	"os"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprint(os.Stderr, `Usage: request-benchmark <subcommand> [flags]

Subcommands:
  http      Send HTTP requests to the apiserver (default when no subcommand given)
  informer  Start informers and measure sync time
  patch     Send Strategic Merge Patch requests to target Pods
  watch     Start continuous watches/informers and measure event delivery and lag

Run 'request-benchmark <subcommand> --help' for subcommand-specific flags.
`)
		os.Exit(0)
	}
	mode := "http"
	// if the first arg doesn't start with "-" it's a subcommand name
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		mode = args[0]
		args = args[1:]
	}
	switch mode {
	case "http":
		if err := runHTTP(args); err != nil {
			log.Fatal(err)
		}
	case "informer":
		if err := runInformer(args); err != nil {
			log.Fatal(err)
		}
	case "patch":
		if err := runPatch(args); err != nil {
			log.Fatal(err)
		}
	case "watch":
		if err := runWatch(args); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown subcommand %q, valid: http, informer, patch, watch", mode)
	}
}
