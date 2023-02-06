# Network policy enforcement latency

Golang client applications that run in pods inside a K8s cluster, and measures
network policy enforcement latency for changes to pods and network policies. The
generated metrics reflect the programming latency of network policies.

## Test client applications

There are two applications `pod-creation-reachability-latency`
and `policy-creation-enforcement-latency`, that will cover network policy
enforcement latency for podand policy creation. They run a test based on the
specified configuration, produce metrics, and then goes into an idle
state. A test client pod has to be recreated to rerun the test.

### Pod creation reachability latency

A test client pod should be running before deploying target pods.

1. Watch create events for pods that match namespace and label selector and
   register them to measure their startup latency.
2. Watch update events for the new pods to identify when they have an IP address
   assigned.
3. Start sending traffic to target pods as soon as they get an IP address
   assigned.
4. Record the first request with a successful (non-error) response. CURL command
   returns errors for requests denied by a policy, or when the IP is
   unreachable.
5. Produce metrics for:
    1. Latency for pods to get IPs assigned
       (`pod_ip_address_assigned` - `pod_creation_time`)
    2. Reachability latency after pod IP is assigned
       (`successful_respose_time` - `pod_ip_address_assigned`)

Measures the metrics by connecting to the target pod:

* **IP assigned latency** (`pod_ip_address_assigned_latency_seconds`) - The
  time between pod creation timestamp and pod IP address assigned (received
  through Pod API object update).
* **Pod creation reachability
  latency** (`pod_creation_reachability_latency_seconds`) - The time
  between pod's IP assigned and a test client successfully connecting to the
  pod. The latency to reach new pods that are affected by network policies,
  during a high pod churn rate (e.g. 100 per second). The sender (test client)
  pods watch target pods, and start sending traffic to them as soon as they have
  IPs assigned. They keep sending traffic to all target pods in
  parallel at 1 second intervals, and record the first successful response for
  every target pod separately.

### Policy creation enforcement latency

A test client pod should be created after all target pods are deployed. It
requires correct setup of network policies in the cluster, where one policy that
denies traffic exists before traffic to the pod starts, and a new policy that
allows it is created during the traffic.

1. List pods that match the provided namespace and label selector.
2. Start sending traffic to target pods.
3. Record the first request with a successful (non-error) response. CURL command
   returns errors for requests denied by a policy, or when the IP is
   unreachable.
4. Produce a metric for latency to reach a pod, based on a new network
   policy. (`successful_respose_time` - `policy_create_time`)
    1. `successful_respose_time`: Once the network policy is enforced, the test
       client pod records the time of the first response to a curl request that
       doesn't return an error (requests denied by the policy return errors).
    2. `policy_create_time`: Taken from the network policy's ObjectMeta field
       CreationTimestamp. The test client pod gets the network policy that
       allows egress from the kube-apiserver, based on the
       namespace `testClientNamespace` and name `allowPolicyName` specified in
       the test client config.

Measures the metrics by connecting to the target pods:

* **Policy creation enforcement
  latency** (`policy_enforcement_latency_policy_creation_seconds`) - The
  time between policy creation that allows test clients to connect to the
  pod, and a test client successfully connecting to the pod.

## Configuration

| Name                | Description                                                                              | Default value | Mandatory                                    |
|---------------------|------------------------------------------------------------------------------------------|---------------|----------------------------------------------|
| testClientNamespace | The namespace of test client pods                                                        | ""            | X                                            |
| targetLabelSelector | The label selector of target pods to send requests to                                    | ""            | X                                            |
| targetNamespace     | The namespace of target pods to send requests to                                         | ""            | X                                            |
| targetPort          | The port number of target pods to send requests to                                       | 0             | X                                            |
| maxTargets          | The maximum number of target pods to send requests to                                    | 100           |                                              |
| metricsPort         | The container port number where Prometheus metrics are exposed                           | 9154          |                                              |
| allowPolicyName     | The name of the egress policy that allows traffic from test client pods to target pods   | ""            | Only for policy creation enforcement latency |

### Scrape metrics

A port name can be specified for the container port, allow it to easily scrape
the metrics based on the port name.

Example port name `npdelaymetrics`:

```
  containers:
  - name: netpol-enforcement-latency
    ports:
    - containerPort: 9160
      name: npdelaymetrics
      protocol: TCP
```

Example PodMonitor that scrapes Prometheus metrics based on port name:

```
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  labels:
    k8s-app: net-policy-client-pods
  name: net-policy-client-pods
  namespace: monitoring
spec:
  podMetricsEndpoints:
  - interval: 30s
    port: npdelaymetrics
  jobLabel: k8s-app
  selector:
     matchLabels:
       test: np-test-client
  namespaceSelector:
    any: true
```

## Build and Push the Images

```
make push_pod_creation

or

make push_policy_creation
```

The project `k8s-staging-perf-tests` can be used for pushing the image used by
CI runs.

## Example test

First get the IP address of kube-apiserver, to allow Egress from the test client
pods to it.\
This is needed because there will be Egress network policies affecting test
client pods, which means all connections that are not specified in network
policies will be denied.\
Replace the `{{.kubeAPIServerIP}}` field in `permissions.yaml` file with the
endpoint's IP address.

```
kubectl get endpoints --namespace default kubernetes

Example output:
NAME         ENDPOINTS       AGE
kubernetes   10.26.0.2:443   1d
```

### Pod creation reachability latency

Use the example manifests to run a pod creation reachability latency test.

Apply the example manifests.

```
path="pod-creation-reachability-latency/example-manifests"

kubectl apply -f ${path}/permissions.yaml
kubectl apply -f ${path}/policy-allow-egress-to-target-pods.yaml
kubectl apply -f ${path}/test-client-deployment.yaml

# Wait for all test client pods to be in the running state.

kubectl apply -f ${path}/target-deployment.yaml
```

Check test client logs

```
kubectl get pods
kubectl logs <test-client-pod-name>

Example output:
I0123 18:41:04.152852       1 main.go:29] Starting pod creation reachability latency client
I0123 18:41:04.153398       1 utils.go:125] --HostNamespace=default
I0123 18:41:04.153423       1 utils.go:125] --MaxTargets=20
I0123 18:41:04.153430       1 utils.go:125] --MetricsPort=9160
I0123 18:41:04.153434       1 utils.go:125] --TargetLabelSelector=target=test-policy
I0123 18:41:04.153438       1 utils.go:125] --TargetNamespace=test-policy
I0123 18:41:04.153441       1 utils.go:125] --TargetPort=80
I0123 18:41:04.153593       1 client.go:86] Verifying test client configuration
I0123 18:41:04.153686       1 metrics.go:87] Starting HTTP server on ":9160".
I0123 18:41:04.153778       1 client.go:110] Starting to measure pod creation reachability latency
I0123 18:41:04.153846       1 client.go:113] Starting 10 workers for parallel processing of pod Add and Update events
I0123 18:41:04.153950       1 client.go:125] Creating PodWatcher for namespace "test-policy" and labelSelector "target=test-policy"
I0123 18:41:04.654125       1 utils.go:287] Going into idle state
I0123 18:41:37.339764       1 client.go:255] Test client got pod "target-dep-65568dcbcd-57lb6" with assigned IP "10.40.0.110", 6.337975588s after pod creation
I0123 18:41:37.348059       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.110:80"
I0123 18:41:37.361886       1 client.go:255] Test client got pod "target-dep-65568dcbcd-rvzrf" with assigned IP "10.40.0.111", 6.361863566s after pod creation
I0123 18:41:37.362765       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.111:80"
I0123 18:41:37.842565       1 client.go:264] Pod "target-dep-65568dcbcd-rvzrf" in namespace "test-policy" reached 480.684213ms after pod IP was assigned
I0123 18:41:37.886092       1 client.go:264] Pod "target-dep-65568dcbcd-57lb6" in namespace "test-policy" reached 548.096898ms after pod IP was assigned
I0123 18:41:38.348000       1 client.go:255] Test client got pod "target-dep-65568dcbcd-m46x9" with assigned IP "10.40.0.113", 7.34797546s after pod creation
I0123 18:41:38.348121       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.113:80"
I0123 18:41:38.358011       1 client.go:264] Pod "target-dep-65568dcbcd-m46x9" in namespace "test-policy" reached 10.024329ms after pod IP was assigned
I0123 18:41:38.390730       1 client.go:255] Test client got pod "target-dep-65568dcbcd-m4zvn" with assigned IP "10.40.0.114", 7.390697837s after pod creation
I0123 18:41:38.390786       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.114:80"
I0123 18:41:38.402522       1 client.go:264] Pod "target-dep-65568dcbcd-m4zvn" in namespace "test-policy" reached 11.805729ms after pod IP was assigned
...
```

### Policy creation enforcement latency

Use the example manifests to run a network policy enforcement latency test.

Apply the example manifests.

```
path="policy-creation-enforcement-latency/example-manifests"

kubectl apply -f ${path}/permissions.yaml
kubectl apply -f ${path}/policy-deny-egress-to-target-pods.yaml
kubectl apply -f ${path}/target-deployment.yaml

# Wait for all target pods to be in the running state.

kubectl apply -f ${path}/test-client-deployment.yaml

# Wait for all test client pods to be in the running state.

kubectl apply -f ${path}/policy-allow-egress-to-target-pods.yaml
```

Check test client logs

```
I0123 20:10:01.529745       1 main.go:29] Starting policy creation enforcement latency client
I0123 20:10:01.530861       1 utils.go:125] --AllowPolicyName=allow-egress-to-target
I0123 20:10:01.530883       1 utils.go:125] --HostNamespace=default
I0123 20:10:01.530890       1 utils.go:125] --MaxTargets=20
I0123 20:10:01.530896       1 utils.go:125] --MetricsPort=9160
I0123 20:10:01.530900       1 utils.go:125] --TargetLabelSelector=target=test-policy
I0123 20:10:01.530904       1 utils.go:125] --TargetNamespace=test-policy
I0123 20:10:01.530908       1 utils.go:125] --TargetPort=80
I0123 20:10:01.531376       1 client.go:84] Verifying test client configuration
I0123 20:10:01.531515       1 client.go:108] Starting to measure network policy enforcement latency on network policy creation
I0123 20:10:01.532044       1 metrics.go:87] Starting HTTP server on ":9160".
I0123 20:10:01.552950       1 client.go:113] 20 pods listed
I0123 20:10:01.553176       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.134:80"
I0123 20:10:01.553299       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.132:80"
I0123 20:10:01.553492       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.1.129:80"
I0123 20:10:01.553652       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.129:80"
I0123 20:10:01.553742       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.130:80"
I0123 20:10:01.553833       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.3.169:80"
I0123 20:10:01.553897       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.1.134:80"
I0123 20:10:01.554002       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.1.131:80"
I0123 20:10:01.554080       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.1.130:80"
I0123 20:10:01.554211       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.1.133:80"
I0123 20:10:01.554297       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.3.165:80"
I0123 20:10:01.554392       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.3.168:80"
I0123 20:10:01.562139       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.3.166:80"
I0123 20:10:01.562312       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.1.132:80"
I0123 20:10:01.562520       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.133:80"
I0123 20:10:01.569612       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.3.167:80"
I0123 20:10:01.562279       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.127:80"
I0123 20:10:01.578456       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.1.128:80"
I0123 20:10:01.578726       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.131:80"
I0123 20:10:01.553178       1 utils.go:194] Sending requests "curl --connect-timeout 1 10.40.0.128:80"
I0123 20:10:18.706375       1 client.go:184] Pod "target-dep-65568dcbcd-ms4bt" in namespace "test-policy" with IP "10.40.3.168" reached 673.263657ms after policy creation
I0123 20:10:18.706499       1 client.go:184] Pod "target-dep-65568dcbcd-vt4hm" in namespace "test-policy" with IP "10.40.3.166" reached 676.158747ms after policy creation
I0123 20:10:18.706530       1 client.go:184] Pod "target-dep-65568dcbcd-tq26b" in namespace "test-policy" with IP "10.40.0.133" reached 684.548426ms after policy creation
I0123 20:10:18.707511       1 client.go:184] Pod "target-dep-65568dcbcd-6tb6b" in namespace "test-policy" with IP "10.40.1.129" reached 689.828066ms after policy creation
I0123 20:10:18.707532       1 client.go:184] Pod "target-dep-65568dcbcd-x55q9" in namespace "test-policy" with IP "10.40.0.134" reached 689.842866ms after policy creation
I0123 20:10:18.707542       1 client.go:184] Pod "target-dep-65568dcbcd-6cgkx" in namespace "test-policy" with IP "10.40.0.132" reached 695.183466ms after policy creation
I0123 20:10:18.707551       1 client.go:184] Pod "target-dep-65568dcbcd-p6mqm" in namespace "test-policy" with IP "10.40.0.127" reached 698.138845ms after policy creation
I0123 20:10:18.707559       1 client.go:184] Pod "target-dep-65568dcbcd-w54hj" in namespace "test-policy" with IP "10.40.3.167" reached 702.108475ms after policy creation
I0123 20:10:18.716142       1 client.go:184] Pod "target-dep-65568dcbcd-bh9wq" in namespace "test-policy" with IP "10.40.0.130" reached 716.129884ms after policy creation
I0123 20:10:18.716729       1 client.go:184] Pod "target-dep-65568dcbcd-q5l8z" in namespace "test-policy" with IP "10.40.1.132" reached 716.721784ms after policy creation
I0123 20:10:18.725684       1 client.go:184] Pod "target-dep-65568dcbcd-dgwdw" in namespace "test-policy" with IP "10.40.1.134" reached 725.671293ms after policy creation
I0123 20:10:18.727037       1 client.go:184] Pod "target-dep-65568dcbcd-7sfqw" in namespace "test-policy" with IP "10.40.0.129" reached 727.030083ms after policy creation
I0123 20:10:18.731291       1 client.go:184] Pod "target-dep-65568dcbcd-fcn44" in namespace "test-policy" with IP "10.40.1.133" reached 731.282832ms after policy creation
I0123 20:10:18.740188       1 client.go:184] Pod "target-dep-65568dcbcd-vk5x4" in namespace "test-policy" with IP "10.40.3.165" reached 740.171632ms after policy creation
I0123 20:10:18.742345       1 client.go:184] Pod "target-dep-65568dcbcd-w7fn9" in namespace "test-policy" with IP "10.40.1.128" reached 742.336012ms after policy creation
I0123 20:10:18.744223       1 client.go:184] Pod "target-dep-65568dcbcd-d5b7n" in namespace "test-policy" with IP "10.40.3.169" reached 744.215222ms after policy creation
I0123 20:10:18.747077       1 client.go:184] Pod "target-dep-65568dcbcd-fc8lx" in namespace "test-policy" with IP "10.40.1.130" reached 747.069352ms after policy creation
I0123 20:10:18.751370       1 client.go:184] Pod "target-dep-65568dcbcd-fvlbl" in namespace "test-policy" with IP "10.40.0.128" reached 751.361591ms after policy creation
I0123 20:10:18.752857       1 client.go:184] Pod "target-dep-65568dcbcd-wx5pv" in namespace "test-policy" with IP "10.40.0.131" reached 752.849461ms after policy creation
I0123 20:10:18.758094       1 client.go:184] Pod "target-dep-65568dcbcd-dgzbg" in namespace "test-policy" with IP "10.40.1.131" reached 758.0731ms after policy creation
I0123 20:10:18.758141       1 utils.go:287] Going into idle state
```