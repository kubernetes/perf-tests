module k8s.io/perf-tests/perfdash

go 1.17

require (
	cloud.google.com/go v0.100.2 // indirect
	cloud.google.com/go/storage v1.10.0
	github.com/aws/aws-sdk-go v1.49.16
	github.com/ghodss/yaml v1.0.0
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	go.opencensus.io v0.23.0 // indirect
	google.golang.org/api v0.70.0
	gopkg.in/yaml.v2 v2.2.8
	k8s.io/klog v0.3.1
	k8s.io/kubernetes v1.15.0
)

require (
	cloud.google.com/go/compute v1.3.0 // indirect
	cloud.google.com/go/iam v0.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/gogo/protobuf v0.0.0-20171007142547-342cbe0a0415 // indirect
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/gofuzz v0.0.0-20170612174753-24818f796faf // indirect
	github.com/googleapis/gax-go/v2 v2.1.1 // indirect
	github.com/googleapis/gnostic v0.0.0-20170729233727-0c5108395e2d // indirect
	github.com/jmespath/go-jmespath v0.4.0 // indirect
	github.com/json-iterator/go v0.0.0-20180701071628-ab8a2e0c74be // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_model v0.0.0-20190812154241-14fe0d1b01d4 // indirect
	github.com/prometheus/common v0.0.0-20181126121408-4724e9255275 // indirect
	golang.org/x/crypto v0.14.0 // indirect
	golang.org/x/net v0.17.0 // indirect
	golang.org/x/oauth2 v0.0.0-20211104180415-d3ed0bb246c8 // indirect
	golang.org/x/sys v0.13.0 // indirect
	golang.org/x/term v0.13.0 // indirect
	golang.org/x/text v0.13.0 // indirect
	golang.org/x/time v0.0.0-20191024005414-555d28b269f0 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20220222213610-43724f9ea8cf // indirect
	google.golang.org/grpc v1.44.0 // indirect
	google.golang.org/protobuf v1.27.1 // indirect
	gopkg.in/inf.v0 v0.9.0 // indirect
	gopkg.in/yaml.v3 v3.0.0-20200313102051-9f266ea9e77c // indirect
	k8s.io/api v0.0.0 // indirect
	k8s.io/apimachinery v0.0.0 // indirect
	k8s.io/client-go v0.0.0 // indirect
	k8s.io/utils v0.0.0-20190221042446-c2654d5206da // indirect
	sigs.k8s.io/yaml v1.1.0 // indirect
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
