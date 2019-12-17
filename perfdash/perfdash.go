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
	"k8s.io/klog"
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

var options = &GoogleGCSDownloaderOptions{}

var (
	addr   = pflag.String("address", ":8080", "The address to serve web data on")
	www    = pflag.Bool("www", false, "If true, start a web-server to server performance data")
	wwwDir = pflag.String("dir", "www", "If non-empty, add a file server for this directory at the root of the web server")

	storageUrl = pflag.String("storageUrl", "https://prow.k8s.io/view/gcs", "Name of the data bucket")

	globalConfig = make(map[string]string)
)

func initGoogleDownloaderOptions() {
	pflag.BoolVar(&options.OverrideBuildCount, "force-builds", false, "Whether to enforce number of builds to process as passed via --builds flag. "+
		"This would override values defined by \"perfDashBuildsCount\" label on prow job")
	pflag.IntVar(&options.DefaultBuildsCount, "builds", maxBuilds, "Total builds number")
	pflag.StringArrayVar(&options.ConfigPaths, "configPath", []string{}, "Paths/urls to the prow config")
	pflag.StringArrayVar(&options.GithubConfigDirs, "githubConfigDir", []string{}, "Github API url to the prow config directory, all configs from this dir will be used."+
		"To specify more than one dir, this arg shall be specified multiple times, one time for each dir.")
	pflag.StringVar(&options.CredentialPath, "credentialPath", "", "Path to the gcs credential json")
	pflag.StringVar(&options.LogsBucket, "logsBucket", "kubernetes-jenkins", "Name of the data bucket")
	pflag.StringVar(&options.LogsPath, "logsPath", "logs", "Path to the logs inside the logs bucket")
}

func main() {
	klog.InitFlags(nil)
	klog.Infof("Starting perfdash...")
	if err := run(); err != nil {
		klog.Error(err)
		os.Exit(1)
	}
}

func run() error {
	initGoogleDownloaderOptions()
	pflag.Parse()
	initGlobalConfig()

	if options.DefaultBuildsCount > maxBuilds || options.DefaultBuildsCount < 0 {
		klog.Infof("Invalid number of builds: %d, setting to %d", options.DefaultBuildsCount, maxBuilds)
		options.DefaultBuildsCount = maxBuilds
	}

	downloader, err := NewGoogleGCSDownloader(options)
	if err != nil {
		panic(err)
	}
	result := make(JobToCategoryData)

	if !*www {
		result, err = downloader.getData()
		if err != nil {
			return fmt.Errorf("fetching data failed: %v", err)
		}
		prettyResult, err := json.MarshalIndent(result, "", " ")
		if err != nil {
			return fmt.Errorf("formatting data failed: %v", err)
		}
		klog.Infof("Result: %v", string(prettyResult))
		return nil
	}

	go func() {
		for {
			klog.Infof("Fetching new data...")
			result, err = downloader.getData()
			if err != nil {
				klog.Errorf("Error fetching data: %v", err)
				time.Sleep(errorDelay)
				continue
			}
			klog.Infof("Data fetched, sleeping %v...", pollDuration)
			time.Sleep(pollDuration)
		}
	}()

	klog.Infof("Starting server...")
	http.Handle("/", http.FileServer(http.Dir(*wwwDir)))
	http.HandleFunc("/jobnames", result.ServeJobNames)
	http.HandleFunc("/metriccategorynames", result.ServeCategoryNames)
	http.HandleFunc("/metricnames", result.ServeMetricNames)
	http.HandleFunc("/buildsdata", result.ServeBuildsData)
	http.HandleFunc("/config", serveConfig)
	return http.ListenAndServe(*addr, nil)
}

func initGlobalConfig() {
	globalConfig["logsBucket"] = options.LogsBucket
	globalConfig["logsPath"] = options.LogsPath
	globalConfig["storageUrl"] = *storageUrl
}

func serveConfig(res http.ResponseWriter, req *http.Request) {
	serveHTTPObject(res, req, &globalConfig)
}
