# Logging

The logging template creates workload generator pods.

## Running 

```
./e2e.test --host="https://192.168.99.100:8443" --ginkgo.v=true --ginkgo.focus="Cluster Loader" --kubeconfig=/home/rlourenc/.kube/config --viper-config=config/logger
```

## Config

The config file follows the standard structure:

```
provider: local
ClusterLoader:
  delete: false
  projects:
    - num: 1
      basename: clusterproject
      tuning: default
      pods:
        - num: 20
          image: gcr.io/google_containers/busybox:1.24
          basename: busybox
          file: pod-logger.json
  tuningsets:
    - name: default
      pods:
        stepping:
          stepsize: 10
          pause: 60
        ratelimit:
          delay: 100
```

## Sample output
(verbose)

```
• [SLOW TEST:55.708 seconds]
[k8s.io] Cluster Loader [Feature:ManualPerformance]
/home/rlourenc/workspace/go/src/github.com/kubernetes/perf-tests/clusterloader/vendor/k8s.io/kubernetes/test/e2e/framework/framework.go:826
  running config file
  /home/rlourenc/workspace/go/src/github.com/kubernetes/perf-tests/clusterloader/cluster_loader.go:93
------------------------------
Override:
map[string]interface {}{}
PFlags:
map[string]viper.FlagValue{}
Env:
map[string]string{}
Key/Value Store:
map[string]interface {}{}
Config:
map[string]interface {}{"provider":"local", "clusterloader":map[interface {}]interface {}{"delete":false, "projects":[]interface {}{map[interface {}]interface {}{"num":1, "basename":"clusterproject", "tuning":"default", "pods":[]interface {}{map[interface {}]interface {}{"num":15, "image":"gcr.io/google_containers/busybox:1.24", "basename":"busybox", "file":"pod-logger.json"}}}}, "tuningsets":[]interface {}{map[interface {}]interface {}{"name":"default", "pods":map[interface {}]interface {}{"stepping":map[interface {}]interface {}{"stepsize":10, "pause":60}, "ratelimit":map[interface {}]interface {}{"delay":100}}}}}}
Defaults:
map[string]interface {}{}
I0206 16:19:45.676020   28369 e2e.go:187] Starting e2e run "b109c1db-ec7f-11e6-81c1-68f728fb771d" on Ginkgo node 1
Running Suite: Kubernetes e2e suite
===================================
Random Seed: 1486394385 - Will randomize all specs
Will run 1 of 100 specs

Feb  6 16:19:45.680: INFO: >>> kubeConfig: /home/rlourenc/.kube/config

Feb  6 16:19:45.683: INFO: Waiting up to 4h0m0s for all (but 0) nodes to be schedulable
Feb  6 16:19:45.717: INFO: Waiting up to 10m0s for all pods (need at least 0) in namespace 'kube-system' to be running and ready
Feb  6 16:19:45.721: INFO: Waiting for pods to enter Success, but no pods in "kube-system" match label map[name:e2e-image-puller]
Feb  6 16:19:45.725: INFO: 3 / 3 pods in namespace 'kube-system' are running and ready (0 seconds elapsed)
Feb  6 16:19:45.725: INFO: expected 2 pod replicas in namespace 'kube-system', 2 are Running and Ready.
Feb  6 16:19:45.725: INFO: No cloud config support.
SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS
------------------------------
[k8s.io] Cluster Loader [Feature:ManualPerformance]
  running config file
  /home/rlourenc/workspace/go/src/github.com/kubernetes/perf-tests/clusterloader/cluster_loader.go:93
[BeforeEach] [k8s.io] Cluster Loader [Feature:ManualPerformance]
  /home/rlourenc/workspace/go/src/github.com/kubernetes/perf-tests/clusterloader/vendor/k8s.io/kubernetes/test/e2e/framework/framework.go:141
STEP: Creating a kubernetes client
Feb  6 16:19:45.725: INFO: >>> kubeConfig: /home/rlourenc/.kube/config

STEP: Building a namespace api object
STEP: Waiting for a default service account to be provisioned in namespace
[BeforeEach] [k8s.io] Cluster Loader [Feature:ManualPerformance]
  /home/rlourenc/workspace/go/src/github.com/kubernetes/perf-tests/clusterloader/cluster_loader.go:46
[It] running config file
  /home/rlourenc/workspace/go/src/github.com/kubernetes/perf-tests/clusterloader/cluster_loader.go:93
Feb  6 16:19:45.750: INFO: Our tuning set is: &{default {{10 60ns 0s} {100ns}} {{0 0s 0s} {0s}}}
Feb  6 16:19:45.767: INFO: 1/1 : Created new namespace: clusterproject0
Feb  6 16:19:45.767: INFO: The loaded config file is: [{Name:logger-busybox Image:gcr.io/google_containers/busybox:1.24 Command:[/bin/sh -c while true; do logger -s LOGGER_POD; sleep 1; done] Args:[] WorkingDir: Ports:[{Name: HostPort:0 ContainerPort:8080 Protocol:TCP HostIP:}] Env:[] Resources:{Limits:map[] Requests:map[]} VolumeMounts:[{Name:dev-log ReadOnly:false MountPath:/dev/log SubPath:}] LivenessProbe:nil ReadinessProbe:nil Lifecycle:nil TerminationMessagePath:/dev/termination-log ImagePullPolicy:IfNotPresent SecurityContext:&SecurityContext{Capabilities:&Capabilities{Add:[],Drop:[],},Privileged:*true,SELinuxOptions:nil,RunAsUser:nil,RunAsNonRoot:nil,ReadOnlyRootFilesystem:nil,} Stdin:false StdinOnce:false TTY:false}]
Feb  6 16:19:45.767: INFO: 1/15 : Creating pod
Feb  6 16:19:45.771: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:19:45.871: INFO: 2/15 : Creating pod
Feb  6 16:19:45.874: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:19:45.975: INFO: 3/15 : Creating pod
Feb  6 16:19:45.978: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:19:46.324: INFO: 6/15 : Creating pod
Feb  6 16:19:46.331: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:19:46.431: INFO: 7/15 : Creating pod
Feb  6 16:19:46.435: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:19:46.535: INFO: 8/15 : Creating pod
Feb  6 16:19:46.603: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:19:46.703: INFO: 9/15 : Creating pod
Feb  6 16:19:46.757: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:19:46.857: INFO: 10/15 : Creating pod
Feb  6 16:19:46.943: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:19:48.047: INFO: Selector matched 10 pods for map[purpose:test]
Feb  6 16:19:48.047: INFO: Found 0 / 10
Feb  6 16:19:49.050: INFO: Selector matched 10 pods for map[purpose:test]
Feb  6 16:19:49.050: INFO: Found 0 / 10
Feb  6 16:19:50.046: INFO: Selector matched 10 pods for map[purpose:test]
Feb  6 16:19:50.046: INFO: Found 0 / 10
Feb  6 16:19:51.047: INFO: Selector matched 10 pods for map[purpose:test]
Feb  6 16:19:51.047: INFO: Found 0 / 10
Feb  6 16:19:52.047: INFO: Selector matched 10 pods for map[purpose:test]
Feb  6 16:19:52.047: INFO: Found 0 / 10
Feb  6 16:19:53.047: INFO: Selector matched 10 pods for map[purpose:test]
Feb  6 16:19:53.047: INFO: Found 0 / 10
Feb  6 16:19:54.047: INFO: Selector matched 10 pods for map[purpose:test]
Feb  6 16:19:54.047: INFO: Found 2 / 10
Feb  6 16:19:55.046: INFO: Selector matched 10 pods for map[purpose:test]
Feb  6 16:19:55.047: INFO: Found 4 / 10
Feb  6 16:19:56.047: INFO: Selector matched 10 pods for map[purpose:test]
Feb  6 16:19:56.047: INFO: Found 7 / 10
Feb  6 16:19:57.047: INFO: Selector matched 10 pods for map[purpose:test]
Feb  6 16:19:57.047: INFO: Found 9 / 10
Feb  6 16:19:58.048: INFO: Selector matched 10 pods for map[purpose:test]
Feb  6 16:19:58.048: INFO: Found 10 / 10
Feb  6 16:19:58.048: INFO: WaitFor completed with timeout 0s.  Pods found = 10 out of 10
Feb  6 16:19:58.048: INFO: We have created 10 pods and are now sleeping for 60 seconds
Feb  6 16:20:58.048: INFO: 11/15 : Creating pod
Feb  6 16:20:58.053: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:20:58.153: INFO: 12/15 : Creating pod
Feb  6 16:20:58.157: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:20:58.257: INFO: 13/15 : Creating pod
Feb  6 16:20:58.262: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:19:58.048: INFO: We have created 10 pods and are now sleeping for 60 seconds
Feb  6 16:20:58.048: INFO: 11/15 : Creating pod
Feb  6 16:20:58.053: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:20:58.153: INFO: 12/15 : Creating pod
Feb  6 16:20:58.157: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:20:58.257: INFO: 13/15 : Creating pod
Feb  6 16:20:58.262: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:20:58.363: INFO: 14/15 : Creating pod
Feb  6 16:20:58.366: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:20:58.466: INFO: 15/15 : Creating pod
Feb  6 16:20:58.473: INFO: Sleeping 100 ms between podcreation.
Feb  6 16:21:03.573: INFO: All pods running in namespace e2e-tests-clusterproject0-t3zh8.
[AfterEach] [k8s.io] Cluster Loader [Feature:ManualPerformance]
  /home/rlourenc/workspace/go/src/github.com/kubernetes/perf-tests/clusterloader/vendor/k8s.io/kubernetes/test/e2e/framework/framework.go:142
Feb  6 16:21:03.573: INFO: Waiting up to 3m0s for all (but 0) nodes to be ready
STEP: Destroying namespace "e2e-tests-cluster-loader-d7043" for this suite.
Feb  6 16:21:13.610: INFO: namespace: e2e-tests-cluster-loader-d7043, resource: bindings, ignored listing per whitelist
STEP: Destroying namespace "e2e-tests-clusterproject0-t3zh8" for this suite.
Feb  6 16:22:03.684: INFO: namespace: e2e-tests-clusterproject0-t3zh8, resource: bindings, ignored listing per whitelist

• [SLOW TEST:137.978 seconds]
[k8s.io] Cluster Loader [Feature:ManualPerformance]
/home/rlourenc/workspace/go/src/github.com/kubernetes/perf-tests/clusterloader/vendor/k8s.io/kubernetes/test/e2e/framework/framework.go:826
  running config file
  /home/rlourenc/workspace/go/src/github.com/kubernetes/perf-tests/clusterloader/cluster_loader.go:93
------------------------------
SSSSSSSSSSSSSSSSSSSSSSSSSSSSSSSS
Ran 1 of 100 Specs in 138.024 seconds
SUCCESS! -- 1 Passed | 0 Failed | 0 Pending | 99 Skipped PASS
```
