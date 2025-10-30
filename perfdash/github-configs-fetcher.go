/*
Copyright 2019 The Kubernetes Authors.

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
	"io"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v2"

	"k8s.io/klog"
)

type githubDirContent struct {
	Name        string `yaml:"name"`
	Path        string `yaml:"path"`
	DownloadURL string `yaml:"download_url"`
	Type        string `yaml:"type"`
	URL         string `yaml:"url"`
}

// GetConfigsFromGithub gets config paths from github directory. It uses github API,
// which is documented here: https://developer.github.com/v3/repos/contents/
//
// Example url: https://api.github.com/repos/kubernetes/test-infra/contents/config/jobs/kubernetes/sig-release/release-branch-jobs
//
// Different branch can be specified by appending "?ref=branch-name" parameter at the end
// of the url.
func GetConfigsFromGithub(url string) ([]string, error) {
	var result []string
	contents, err := getGithubDirContents(url)
	if err != nil {
		return nil, err
	}
	for _, c := range contents {
		// Dirs and non-yaml files are ignored; this means that there is no
		// recursive search, it should be good enough for now.
		if c.Type == "file" && strings.HasSuffix(c.Name, ".yaml") {
			result = append(result, c.DownloadURL)
		}
	}
	return result, nil
}

func getGithubDirContents(url string) ([]githubDirContent, error) {
	klog.Infof("Downloading github spec from %v", url)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if token := os.Getenv("GITHUB_TOKEN"); len(token) != 0 {
		req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", token))
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error calling github API %s: %v", url, err)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading github response %s: %v", url, err)
	}
	var decoded []githubDirContent
	err = yaml.Unmarshal(b, &decoded)
	if err != nil {
		return nil, fmt.Errorf("error unmarshall github response %s: %v", string(b), err)
	}
	return decoded, nil
}
