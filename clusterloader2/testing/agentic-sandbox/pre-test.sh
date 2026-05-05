#!/bin/bash
#

echo "Installing agentic sandbox core manifest"
kubectl apply -f "${GOPATH}"/src/k8s.io/perf-tests/clusterloader2/testing/agentic-sandbox/agent-controller/manifest.yaml

echo "Installing agentic sandbox extensions"
kubectl apply -f "${GOPATH}"/src/k8s.io/perf-tests/clusterloader2/testing/agentic-sandbox/agent-controller/extensions.yaml

echo "Waiting for agent sandbox controller to be ready"
kubectl wait --for=condition=Ready pod -l app=agent-sandbox-controller -n agent-sandbox-system --timeout=5m || echo "WARNING: Timeout waiting for agent sandbox controller"

echo "Installing agent-sandbox pprof scraper config"
kubectl apply -f "${GOPATH}"/src/k8s.io/perf-tests/clusterloader2/testing/agentic-sandbox/monitor/pprof-config.yaml
