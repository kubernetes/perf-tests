# dnsperfgo

A golang client for stress-testing Kubernetes DNS.
The client program can be configured to send DNS queries at a custom rate, with
intervals in order to mimic bursts of traffic followed by idle time. It also
searchpath-expands the hostnames provided as input, along with sending A and
AAAA in parallel on Alpine-base image. This attempts to recreate the common
issues seen with Kubernetes DNS, especially at scale.

This client can be used to benchmark different DNS solutions.

## Metrics

This client pushes up the following metrics:

`dns_errors_total` - Count of DNS lookup errors(including timeouts).
`dns_timeouts_total` - Count of DNS lookup timeouts.
`dns_lookups_total` - Count of DNS lookups.
`dns_lookup_latency` - Latency distribution of DNS lookups.

## Build and Push Image

```
make push

```

The project "k8s-staging-perf-tests" can be used for pushing the image used by CI runs.

## Spin up a test deployment on an existing cluster

```
kubectl create -f queries-cm.yaml
kubectl create -f deployment.yaml
```

Note that the dns client counts NXDOMAIN responses as errors, so the configmap needs to contain names that are expected
to resolve. Otherwise, non-zero error metric is expected.
