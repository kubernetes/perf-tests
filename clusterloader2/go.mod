module k8s.io/perf-tests/clusterloader2

// go 1.15+ is required by k8s 1.20 we use as dependency.
go 1.22.4

replace (
	k8s.io/api => k8s.io/api v0.30.3
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.30.3
	k8s.io/apimachinery => k8s.io/apimachinery v0.30.3
	k8s.io/apiserver => k8s.io/apiserver v0.30.3
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.30.3
	k8s.io/client-go => k8s.io/client-go v0.30.3
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.30.3
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.30.3
	k8s.io/code-generator => k8s.io/code-generator v0.30.3
	k8s.io/component-base => k8s.io/component-base v0.30.3
	k8s.io/component-helpers => k8s.io/component-helpers v0.30.3
	k8s.io/controller-manager => k8s.io/controller-manager v0.30.3
	k8s.io/cri-api => k8s.io/cri-api v0.30.3
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.30.3
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.30.3
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.30.3
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.30.3
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.30.3
	k8s.io/kubectl => k8s.io/kubectl v0.30.3
	k8s.io/kubelet => k8s.io/kubelet v0.30.3
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.30.3
	k8s.io/metrics => k8s.io/metrics v0.30.3
	k8s.io/mount-utils => k8s.io/mount-utils v0.30.3
	k8s.io/node-api => k8s.io/node-api v0.30.3
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.30.3
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.30.3
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.30.3
	k8s.io/sample-controller => k8s.io/sample-controller v0.30.3
)

require (
	github.com/go-errors/errors v1.5.1
	github.com/google/go-cmp v0.5.9
	github.com/google/safetext v0.0.0-20230106111101-7156a760e523
	github.com/montanaflynn/stats v0.7.1
	github.com/onsi/ginkgo v1.16.5
	github.com/prometheus/client_model v0.6.0
	github.com/prometheus/common v0.26.0
	github.com/prometheus/prometheus v1.8.2-0.20210331101223-3cafc58827d1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.9.0
	golang.org/x/oauth2 v0.21.0
	golang.org/x/sync v0.7.0
	golang.org/x/time v0.5.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.22.15
	k8s.io/apimachinery v0.22.15
	k8s.io/client-go v0.22.15
	k8s.io/component-base v0.22.15
	k8s.io/component-helpers v0.22.15
	k8s.io/klog/v2 v2.130.1
	k8s.io/kubelet v0.22.15
	k8s.io/kubernetes v1.22.15
)

require (
	cloud.google.com/go v0.79.0 // indirect
	github.com/Azure/go-autorest v14.2.0+incompatible // indirect
	github.com/Azure/go-autorest/autorest v0.11.18 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.9.13 // indirect
	github.com/Azure/go-autorest/autorest/date v0.3.0 // indirect
	github.com/Azure/go-autorest/logger v0.2.1 // indirect
	github.com/Azure/go-autorest/tracing v0.6.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver v3.5.1+incompatible // indirect
	github.com/cespare/xxhash/v2 v2.1.1 // indirect
	github.com/cyphar/filepath-securejoin v0.2.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/docker/distribution v2.7.1+incompatible // indirect
	github.com/edsrzf/mmap-go v1.0.0 // indirect
	github.com/evanphx/json-patch v4.11.0+incompatible // indirect
	github.com/felixge/httpsnoop v1.0.1 // indirect
	github.com/form3tech-oss/jwt-go v3.2.3+incompatible // indirect
	github.com/fsnotify/fsnotify v1.4.9 // indirect
	github.com/go-kit/kit v0.10.0 // indirect
	github.com/go-logfmt/logfmt v0.5.0 // indirect
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/golang/snappy v0.0.3 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/uuid v1.1.2 // indirect
	github.com/googleapis/gnostic v0.5.5 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.16.0 // indirect
	github.com/imdario/mergo v0.3.5 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/moby/spdystream v0.2.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/nxadm/tail v1.4.8 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/onsi/gomega v1.10.1 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/runc v1.0.2 // indirect
	github.com/opentracing/opentracing-go v1.2.0 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.11.0 // indirect
	github.com/prometheus/procfs v0.6.0 // indirect
	github.com/spf13/cobra v1.1.3 // indirect
	github.com/uber/jaeger-client-go v2.25.0+incompatible // indirect
	github.com/uber/jaeger-lib v2.4.0+incompatible // indirect
	go.opentelemetry.io/contrib v0.20.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.20.0 // indirect
	go.opentelemetry.io/otel v0.20.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp v0.20.0 // indirect
	go.opentelemetry.io/otel/metric v0.20.0 // indirect
	go.opentelemetry.io/otel/sdk v0.20.0 // indirect
	go.opentelemetry.io/otel/sdk/export/metric v0.20.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v0.20.0 // indirect
	go.opentelemetry.io/otel/trace v0.20.0 // indirect
	go.opentelemetry.io/proto/otlp v0.7.0 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/goleak v1.1.10 // indirect
	golang.org/x/crypto v0.17.0 // indirect
	golang.org/x/lint v0.0.0-20210508222113-6edffad5e616 // indirect
	golang.org/x/net v0.10.0 // indirect
	golang.org/x/sys v0.15.0 // indirect
	golang.org/x/term v0.15.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/tools v0.6.0 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/genproto v0.0.0-20210602131652-f16073e35f0c // indirect
	google.golang.org/grpc v1.38.0 // indirect
	google.golang.org/protobuf v1.32.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiserver v0.22.15 // indirect
	k8s.io/kube-openapi v0.0.0-20211109043538-20434351676c // indirect
	k8s.io/kubectl v0.0.0 // indirect
	k8s.io/utils v0.0.0-20211116205334-6203023598ed // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.0.30 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.1 // indirect
	sigs.k8s.io/yaml v1.2.0 // indirect
)
