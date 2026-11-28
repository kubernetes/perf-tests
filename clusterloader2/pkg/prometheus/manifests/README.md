# Prometheus Manifests

Images are pulled directly from their upstream registries (`quay.io/prometheus`,
`quay.io/prometheus-operator`, `docker.io/grafana`, `registry.k8s.io`).

## Upgrading the Prometheus operator

The `0prometheus-operator-*` manifests come from the
[prometheus-operator](https://github.com/prometheus-operator/prometheus-operator) release tag:

- `0prometheus-operator-0*CustomResourceDefinition.yaml` are unmodified copies of
  `example/prometheus-operator-crd/monitoring.coreos.com_*.yaml`.
- `0prometheus-operator-clusterRole.yaml` is an unmodified copy of
  `example/rbac/prometheus-operator/prometheus-operator-cluster-role.yaml`.
- The other `0prometheus-operator-*.yaml` files are based on the files in
  `example/rbac/prometheus-operator/`, with the namespace set to `monitoring` and, for the
  deployment, ClusterLoader2 template parameters added.

Upgrade the CRDs, the operator image and the `--prometheus-config-reloader` image together.

## Upgrading Prometheus

The Prometheus image is set in `prometheus-prometheus.yaml` and can be overridden with
`CL2_PROMETHEUS_BASE_IMAGE` and `CL2_PROMETHEUS_VERSION`.
