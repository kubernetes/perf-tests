/*
Copyright The Kubernetes Authors.

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

package initialpods

import (
	"bytes"
	_ "embed"
	"testing"
	"text/template"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer/protobuf"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/perf-tests/clusterloader2/pkg/config"
	"sigs.k8s.io/yaml"
)

//go:embed pod.yaml
var podTemplate string

func TestPodSize(t *testing.T) {
	tmpl, err := template.New("pod").Funcs(config.GetFuncs(nil)).Parse(podTemplate)
	require.NoError(t, err)

	var rendered bytes.Buffer
	err = tmpl.Execute(&rendered, map[string]interface{}{
		"Name":          "bench-pod-0",
		"ImageRegistry": "registry.k8s.io",
	})
	require.NoError(t, err)

	var strictPod corev1.Pod
	err = yaml.UnmarshalStrict(rendered.Bytes(), &strictPod)
	require.NoError(t, err, "YAML contains unknown or unparsed fields")

	decoder := scheme.Codecs.UniversalDeserializer()
	obj, _, err := decoder.Decode(rendered.Bytes(), nil, nil)
	require.NoError(t, err)

	pod, ok := obj.(*corev1.Pod)
	require.True(t, ok, "decoded object must be *corev1.Pod")

	protoSerializer := protobuf.NewSerializer(scheme.Scheme, scheme.Scheme)
	var buf bytes.Buffer
	err = protoSerializer.Encode(pod, &buf)
	require.NoError(t, err)

	require.Equal(t, 2068, buf.Len())
}
