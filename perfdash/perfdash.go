/*
Copyright 2015 The Kubernetes Authors.

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
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/pflag"
)

const (
	pollDuration = 10 * time.Minute
	errorDelay   = 10 * time.Second
	maxBuilds    = 100
)

var (
	addr        = pflag.String("address", ":8080", "The address to serve web data on")
	www         = pflag.Bool("www", false, "If true, start a web-server to server performance data")
	wwwDir      = pflag.String("dir", "www", "If non-empty, add a file server for this directory at the root of the web server")
	builds      = pflag.Int("builds", maxBuilds, "Total builds number")
	configPaths = pflag.StringArray("configPath", []string{}, "Paths/urls to the prow config")
)

func main() {
	fmt.Println("Starting perfdash...")
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	pflag.Parse()
	fmt.Printf("config paths - %d\n", len(*configPaths))
	for i := 0; i < len(*configPaths); i++ {
		fmt.Printf("config path %d: %s\n", i+1, (*configPaths)[i])
	}

	if *builds > maxBuilds || *builds < 0 {
		fmt.Printf("Invalid number of builds: %d, setting to %d\n", *builds, maxBuilds)
		*builds = maxBuilds
	}

	downloader := NewGoogleGCSDownloader(*configPaths, *builds)
	result := make(JobToCategoryData)
	var err error

	if !*www {
		result, err = downloader.getData()
		if err != nil {
			return fmt.Errorf("fetching data failed: %v", err)
		}
		prettyResult, err := json.MarshalIndent(result, "", " ")
		if err != nil {
			return fmt.Errorf("formatting data failed: %v", err)
		}
		fmt.Printf("Result: %v\n", string(prettyResult))
		return nil
	}

	go func() {
		for {
			fmt.Printf("Fetching new data...\n")
			result, err = downloader.getData()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error fetching data: %v\n", err)
				time.Sleep(errorDelay)
				continue
			}
			fmt.Printf("Data fetched, sleeping %v...\n", pollDuration)
			time.Sleep(pollDuration)
		}
	}()

	fmt.Println("Starting server")
	http.Handle("/", http.FileServer(http.Dir(*wwwDir)))
	http.HandleFunc("/jobnames", result.ServeJobNames)
	http.HandleFunc("/metriccategorynames", result.ServeCategoryNames)
	http.HandleFunc("/metricnames", result.ServeMetricNames)
	http.HandleFunc("/buildsdata", result.ServeBuildsData)
	return http.ListenAndServe(*addr, nil)
}
