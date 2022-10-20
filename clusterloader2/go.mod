module k8s.io/perf-tests/clusterloader2

// go 1.15+ is required by k8s 1.20 we use as dependency.
go 1.16

replace (
	k8s.io/api => k8s.io/api v0.22.15
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.22.15
	k8s.io/apimachinery => k8s.io/apimachinery v0.22.15
	k8s.io/apiserver => k8s.io/apiserver v0.22.15
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.22.15
	k8s.io/client-go => k8s.io/client-go v0.22.15
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.22.15
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.22.15
	k8s.io/code-generator => k8s.io/code-generator v0.22.15
	k8s.io/component-base => k8s.io/component-base v0.22.15
	k8s.io/component-helpers => k8s.io/component-helpers v0.22.15
	k8s.io/controller-manager => k8s.io/controller-manager v0.22.15
	k8s.io/cri-api => k8s.io/cri-api v0.22.15
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.22.15
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.22.15
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.22.15
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.22.15
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.22.15
	k8s.io/kubectl => k8s.io/kubectl v0.22.15
	k8s.io/kubelet => k8s.io/kubelet v0.22.15
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.22.15
	k8s.io/metrics => k8s.io/metrics v0.22.15
	k8s.io/mount-utils => k8s.io/mount-utils v0.22.15
	k8s.io/node-api => k8s.io/node-api v0.22.15
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.22.15
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.22.15
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.22.15
	k8s.io/sample-controller => k8s.io/sample-controller v0.22.15
)

require (
	github.com/go-errors/errors v1.0.1
	github.com/google/go-cmp v0.5.6
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/onsi/ginkgo v1.14.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.26.0
	github.com/prometheus/prometheus v1.8.2-0.20210331101223-3cafc58827d1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	golang.org/x/oauth2 v0.0.0-20210323180902-22b0adad7558
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c
	golang.org/x/text v0.3.7 // indirect
	golang.org/x/time v0.0.0-20210723032227-1f47c861a9ac
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.22.15
	k8s.io/apimachinery v0.22.15
	k8s.io/client-go v0.22.15
	k8s.io/component-base v0.22.15
	k8s.io/component-helpers v0.22.15
	k8s.io/klog v1.0.0
	k8s.io/klog/v2 v2.30.0 // indirect
	k8s.io/kubelet v0.22.15
	k8s.io/kubernetes v1.22.15
)
