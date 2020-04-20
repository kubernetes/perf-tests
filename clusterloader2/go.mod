module k8s.io/perf-tests/clusterloader2

go 1.12

replace k8s.io/api => k8s.io/api v0.16.4

replace k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.16.4

replace k8s.io/apimachinery => k8s.io/apimachinery v0.16.4

replace k8s.io/apiserver => k8s.io/apiserver v0.16.4

replace k8s.io/cli-runtime => k8s.io/cli-runtime v0.16.4

replace k8s.io/client-go => k8s.io/client-go v0.16.4

replace k8s.io/cloud-provider => k8s.io/cloud-provider v0.16.4

replace k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.16.4

replace k8s.io/code-generator => k8s.io/code-generator v0.16.4

replace k8s.io/component-base => k8s.io/component-base v0.18.0

replace k8s.io/cri-api => k8s.io/cri-api v0.16.4

replace k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.16.4

replace k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.16.4

replace k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.16.4

replace k8s.io/kube-proxy => k8s.io/kube-proxy v0.16.4

replace k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.16.4

replace k8s.io/kubectl => k8s.io/kubectl v0.16.4

replace k8s.io/kubelet => k8s.io/kubelet v0.16.4

replace k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.16.4

replace k8s.io/metrics => k8s.io/metrics v0.16.4

replace k8s.io/node-api => k8s.io/node-api v0.16.4

replace k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.16.4

replace k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.16.4

replace k8s.io/sample-controller => k8s.io/sample-controller v0.16.4

replace k8s.io/kubernetes => k8s.io/kubernetes v1.16.4

require (
	github.com/go-errors/errors v1.0.1
	github.com/onsi/ginkgo v1.8.0
	github.com/prometheus/client_model v0.2.0
	github.com/prometheus/common v0.4.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.4.0
	golang.org/x/crypto v0.0.0-20190611184440-5c40567a22f8
	golang.org/x/sync v0.0.0-20190423024810-112230192c58
	golang.org/x/time v0.0.0-20181108054448-85acf8d2951c
	gopkg.in/yaml.v2 v2.2.4
	k8s.io/api v0.16.4
	k8s.io/apimachinery v1.16.4
	k8s.io/client-go v0.18.0
	k8s.io/component-base v0.18.0
	k8s.io/klog v1.0.0
	k8s.io/kubernetes v0.0.0-00010101000000-000000000000
)
