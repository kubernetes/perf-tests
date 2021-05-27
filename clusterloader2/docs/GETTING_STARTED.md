# ClusterLoader2

In this tutorial, we will:
- Set-up perf-tests repository for local development
- Create single node cluster using [Kind]
- Implement a simple CL2 test and run it
- Run load test on 100 nodes cluster

## Clone perf-tests repository

Start with cloning perf-tests repository:
```bash
git clone git@github.com:kubernetes/perf-tests.git
cd perf-tests
```

## Install GVM
Follow instructions on [GVM install].
Install golang with specific version (1.15.12 was tested in this tutorial):
```bash
gvm install go1.15.12
gvm use go1.15.12
```
Next, add perf-tests repository to GOPATH:

```bash
gvm linkthis k8s.io/perf-tests
```

## Create cluster using kind
Follow the [kind installation][Kind install] guide.

Create cluster v1.21.1 with one master and one node:
```bash
kind create cluster --image=kindest/node:v1.21.1 --name=test-cluster --wait=5m
```

This command additionally generates cluster access credentials which are
 stored in `${HOME}/.kube/config` within a context named test-cluster.

Check that you can connect to cluster:
```bash
kubectl get nodes
```

## Prepare simple test to run
Let's prepare our first test config (config.yaml).
This test will:
- Create one namespace
- Create a single deployment with 10 pods inside that namespace
- Measure startup latency of these pod

We will create file `config.yaml` that describes this test.
First we need to start with defining test name:
```yaml
name: test
```
CL2 will create namespaces automatically, but we need to specify
how many namespaces we want:
```yaml
namespace:
  number: 1
```
Next, we need to specify TuningSets.
TuningSet describes how actions are executed.
In our case, we will have only 1 deployment
so there will be only 1 action to execute.
In this case tuningSet doesn't really affect transition between states.
```yaml
tuningSets:
- name: Uniform1qps
  qpsLoad:
    qps: 1
```
Test definition consists of a list of steps.
A step can be either collection of Phases or Measurements.
A Phase defines a state the cluster should reach.
A Measurement allows to measure something or wait for something.
You can find list of available measurements here [Measurements].

Our first step will be starting two measurements.
We want to start measuring pod startup latency
and also make measurement that will wait for all pods to be in running state.
Setting the action field to `start` begins execution of measurement.
For both measurements we need to specify labelSelectors
so they know which pods they should take into account.
PodStartupLatency also takes threshold. If 99th percentile of latency
will go over this threshold, test will fail.
```yaml
steps:
- name: Start measurements
  measurements:
  - Identifier: PodStartupLatency
    Method: PodStartupLatency
    Params:
      action: start
      labelSelector: group = test-pod
      threshold: 20s
  - Identifier: WaitForControlledPodsRunning
    Method: WaitForControlledPodsRunning
    Params:
      action: start
      apiVersion: apps/v1
      kind: Deployment
      labelSelector: group = test-deployment
      operationTimeout: 120s
```
Once we created these two measurements,
we can have next step that creates deployment.
We need to specify in which namespaces we want this deployment to be created,
how many of these deployments per namespace.
Also, we will need to specify template for our deployment,
which we will do later.
For now, let's assume that this template allows us
to specify number of replicas in deployment.
```yaml
- name: Create deployment
  phases:
  - namespaceRange:
      min: 1
      max: 1
    replicasPerNamespace: 1
    tuningSet: Uniform1qps
    objectBundle:
    - basename: test-deployment
      objectTemplatePath: "deployment.yaml"
      templateFillMap:
        Replicas: 10
```
Now, we need to wait for pods in this deployment to be in Running state:
```yaml
- name: Wait for pods to be running
  measurements:
  - Identifier: WaitForControlledPodsRunning
    Method: WaitForControlledPodsRunning
    Params:
      action: gather
```
Now we can gather results of PodStartupLatency in next step:
```yaml
- name: Measure pod startup latency
  measurements:
  - Identifier: PodStartupLatency
    Method: PodStartupLatency
    Params:
      action: gather
```
Whole `config.yaml` will look like this:
```yaml
name: test

namespace:
  number: 1

tuningSets:
- name: Uniform1qps
  qpsLoad:
    qps: 1

steps:
- name: Start measurements
  measurements:
  - Identifier: PodStartupLatency
    Method: PodStartupLatency
    Params:
      action: start
      labelSelector: group = test-pod
      threshold: 20s
  - Identifier: WaitForControlledPodsRunning
    Method: WaitForControlledPodsRunning
    Params:
      action: start
      apiVersion: apps/v1
      kind: Deployment
      labelSelector: group = test-deployment
      operationTimeout: 120s
- name: Create deployment
  phases:
  - namespaceRange:
      min: 1
      max: 1
    replicasPerNamespace: 1
    tuningSet: Uniform1qps
    objectBundle:
    - basename: test-deployment
      objectTemplatePath: "deployment.yaml"
      templateFillMap:
        Replicas: 10
- name: Wait for pods to be running
  measurements:
  - Identifier: WaitForControlledPodsRunning
    Method: WaitForControlledPodsRunning
    Params:
      action: gather
- name: Measure pod startup latency
  measurements:
  - Identifier: PodStartupLatency
    Method: PodStartupLatency
    Params:
      action: gather
```

By default, clusterloader will delete auto-created namespaces
so we don't need to worry with cleaning up cluster.

Now, in order to finish our first test, we need to specify deployment template.
You can think of it as regular kubernetes object, but with templating.
CL2 by default adds parameter `Name` that you can use in your template.
In our config, we also passed `Replicas` parameter.
We need to remember to set correct labels so PodStartupLatency
and WaitForControlledPodsRunning will watch correct pods.
So our template for deployment will look like this (`deployment.yaml` file):
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.Name}}
  labels:
    group: test-deployment
spec:
  replicas: {{.Replicas}}
  selector:
    matchLabels:
      group: test-pod
  template:
    metadata:
      labels:
        group: test-pod
    spec:
      containers:
      - image: k8s.gcr.io/pause:3.1
        name: {{.Name}}
```
## Execute test
Before running test, make sure that kubeconfig
current context points to kind cluster:
```bash
$ kubectl config current-context
> kind-test-cluster
```
To execute test, run:
```bash
cd clusterloader2/
go run cmd/clusterloader.go --testconfig=config.yaml --provider=kind --kubeconfig=${HOME}/.kube/config --v=2
```

At the end of clusterloader output you should see pod startup latency:
```json
{
  "data": {
    "Perc50": 7100.534796,
    "Perc90": 8702.523037,
    "Perc99": 9122.894555
  },
  "unit": "ms",
  "labels": {
    "Metric": "pod_startup"
  }
},
```
`pod_startup` measures time since pod was created
until it was observed via watch as running.

You should also see that test succeeded:
```
--------------------------------------------------------------------------------
Test Finished
Test: ./config.yaml
Status: Success
--------------------------------------------------------------------------------
```
As an exercise you can modify threshold for PodStartupLatency
below values you've observed in your run and check if test fails.

## Delete kind cluster
To delete kind cluster, run:
```bash
kind delete cluster --name test-cluster
```

## Running 100-node scale test

Here you can find general purpose [Load test].
This test is release-blocking test we use to evaluate scalability of kubernetes.
It consists of 3 main phases:
- Creating objects
- Scaling objects to size between (50%, 150%) of their original size
- Deleting objects

It can be used to test clusters starting from 100 nodes up to 5k nodes.
Load test will create, roughly 30 * nodes pod objects. It will create:
- deployments
- jobs
- statefulsets
- services
- secrets
- configmaps

There are small (5 pods), medium (30 pods) and big (250 pods) versions of
deployments, jobs and statefulsets.

First, you need to create 100-nodes cluster
then you can run cluster scale test with this command:
```bash
./run-e2e-with-prometheus-fw-rule.sh cluster-loader2 --testconfig=./testing/load/config.yaml --nodes=100 --provider=gke --enable-prometheus-server=true --kubeconfig=${HOME}/.kube/config --v=2
```

`--enable-prometheus-server=true` deploys prometheus server
using prometheus-operator.

There are various measurements that depend on prometheus metrics, for example:
- API responsiveness - measures the latency of requests to kube-apiserver
- Scheduling throughput
- NodeLocalDNS latency

[Kind]: https://kind.sigs.k8s.io/
[GVM install]: https://github.com/moovweb/gvm#installing
[Kind config]: https://kind.sigs.k8s.io/docs/user/quick-start/#advanced
[Kind install]: https://kind.sigs.k8s.io/docs/user/quick-start#installation
[Load test]: https://github.com/kubernetes/perf-tests/tree/master/clusterloader2/testing/load
[Measurements]: https://github.com/kubernetes/perf-tests/tree/master/clusterloader2#measurement
