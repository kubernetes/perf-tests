module k8s.io/perf-tests/clusterloader2

go 1.24.1

replace (
	k8s.io/api => k8s.io/api v0.34.0
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.34.0
	k8s.io/apimachinery => k8s.io/apimachinery v0.34.0
	k8s.io/apiserver => k8s.io/apiserver v0.34.0
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.34.0
	k8s.io/client-go => k8s.io/client-go v0.34.0
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.34.0
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.34.0
	k8s.io/code-generator => k8s.io/code-generator v0.34.0
	k8s.io/component-base => k8s.io/component-base v0.34.0
	k8s.io/component-helpers => k8s.io/component-helpers v0.34.0
	k8s.io/controller-manager => k8s.io/controller-manager v0.34.0
	k8s.io/cri-api => k8s.io/cri-api v0.34.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.34.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.34.0
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.34.0
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.34.0
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.34.0
	k8s.io/kubectl => k8s.io/kubectl v0.34.0
	k8s.io/kubelet => k8s.io/kubelet v0.34.0
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.34.0
	k8s.io/metrics => k8s.io/metrics v0.34.0
	k8s.io/mount-utils => k8s.io/mount-utils v0.34.0
	k8s.io/node-api => k8s.io/node-api v0.34.0
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.34.0
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.34.0
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.34.0
	k8s.io/sample-controller => k8s.io/sample-controller v0.34.0
)

require (
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.19.0
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.11.0
	github.com/go-errors/errors v1.5.1
	github.com/google/go-cmp v0.7.0
	github.com/google/safetext v0.0.0-20230106111101-7156a760e523
	github.com/montanaflynn/stats v0.7.1
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.38.2
	github.com/prometheus/client_model v0.6.2
	github.com/prometheus/common v0.66.1
	github.com/prometheus/prometheus v1.8.2-0.20210331101223-3cafc58827d1
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.11.1
	golang.org/x/crypto v0.42.0
	golang.org/x/oauth2 v0.31.0
	golang.org/x/sync v0.17.0
	golang.org/x/time v0.13.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.34.0
	k8s.io/apimachinery v0.34.0
	k8s.io/client-go v0.34.0
	k8s.io/component-base v0.34.0
	k8s.io/component-helpers v0.34.0
	k8s.io/gengo v0.0.0-20230829151522-9cce18d56c01
	k8s.io/klog/v2 v2.130.1
	k8s.io/kubelet v0.34.0
)

require (
	cloud.google.com/go/compute/metadata v0.3.0 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.11.2 // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.4.2 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/edsrzf/mmap-go v1.0.0 // indirect
	github.com/emicklei/go-restful/v3 v3.12.2 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-kit/kit v0.10.0 // indirect
	github.com/go-logfmt/logfmt v0.5.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.0 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/moby/spdystream v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_golang v1.22.0 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/uber/jaeger-client-go v2.25.0+incompatible // indirect
	github.com/uber/jaeger-lib v2.4.0+incompatible // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.opentelemetry.io/otel v1.35.0 // indirect
	go.opentelemetry.io/otel/trace v1.35.0 // indirect
	go.uber.org/atomic v1.10.0 // indirect
	go.uber.org/goleak v1.3.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/net v0.43.0 // indirect
	golang.org/x/sys v0.36.0 // indirect
	golang.org/x/term v0.35.0 // indirect
	golang.org/x/text v0.29.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/kube-openapi v0.0.0-20250710124328-f3f2b991d03b // indirect
	k8s.io/utils v0.0.0-20250604170112-4c0f3b243397 // indirect
	sigs.k8s.io/json v0.0.0-20241014173422-cfa47c3a1cc8 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.0 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)
