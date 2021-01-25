/*
Copyright 2021 The Kubernetes Authors.

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

package metadata

import (
	"encoding/json"
	"io/ioutil"

	"k8s.io/perf-tests/clusterloader2/pkg/framework"
)

// Dump writes file with data useful for debugging.
// kubetest --metadata-sources should point to that file so that
// it's merged into `finished.json` and available in prow and
// bigquery tables.
func Dump(f *framework.Framework, outputFile string) error {
	output := make(map[string]string)

	// Write any clusterloader-specific metadata here.

	providerMetadata, err := f.GetClusterConfig().Provider.Metadata(f.GetClientSets().GetClient())
	if err != nil {
		return err
	}

	for k, v := range providerMetadata {
		output[k] = v
	}

	return write(output, outputFile)
}

func write(obj map[string]string, outputFile string) error {
	out, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(outputFile, out, 0644)
}
