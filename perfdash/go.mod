module k8s.io/perf-tests/perfdash

go 1.13

require (
	cloud.google.com/go v0.34.0
	github.com/aws/aws-sdk-go v1.16.26
	github.com/ghodss/yaml v0.0.0-20180820084758-c7ce16629ff4
	github.com/google/martian v2.1.0+incompatible // indirect
	github.com/googleapis/gax-go v1.0.3 // indirect
	github.com/spf13/pflag v1.0.1
	go.opencensus.io v0.22.3 // indirect
	google.golang.org/api v0.0.0-20181220000619-583d854617af
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/klog v0.3.1
	k8s.io/kubernetes v1.15.0
)

replace (
	k8s.io/api => k8s.io/api v0.0.0-20200131232428-e3a917c59b04
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.0.0-20200131234631-01db4fb12417
	k8s.io/apimachinery => k8s.io/apimachinery v0.15.11-beta.0
	k8s.io/apiserver => k8s.io/apiserver v0.0.0-20200202071520-d961f93e70e6
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.0.0-20200131235029-0f09337e3ee3
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190805141520-2fe0317bcee0
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.0.0-20200201000119-7b8ede55b43a
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.0.0-20200131235947-54ca054444e5
	k8s.io/code-generator => k8s.io/code-generator v0.15.11-beta.0
	k8s.io/component-base => k8s.io/component-base v0.0.0-20200131233309-6d0e514d4f25
	k8s.io/cri-api => k8s.io/cri-api v0.15.11-beta.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.0.0-20200201000253-f077c5e42bdf
	k8s.io/gengo => k8s.io/gengo v0.0.0-20190822140433-26a664648505
	k8s.io/heapster => k8s.io/heapster v1.2.0-beta.1
	k8s.io/klog => k8s.io/klog v0.4.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.0.0-20200131233918-43c2dfdafb33
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.0.0-20200131235815-457bb3d8fe5e
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20190816220812-743ec37842bf
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.0.0-20200131235339-5546c8498043
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.0.0-20200131235642-b7da85a749b8
	k8s.io/kubectl => k8s.io/kubectl v0.15.11-beta.0.0.20190801031749-f16387a69211
	k8s.io/kubelet => k8s.io/kubelet v0.0.0-20200131235511-d154b7ae367e
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.0.0-20200221174755-677dada92f96
	k8s.io/metrics => k8s.io/metrics v0.0.0-20200131234843-bbc417b03523
	k8s.io/node-api => k8s.io/node-api v0.0.0-20200201000600-929379277843
	k8s.io/repo-infra => k8s.io/repo-infra v0.0.0-20181204233714-00fe14e3d1a3
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.0.0-20200131234123-3090b0ae21bf
)
