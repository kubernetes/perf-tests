#!/bin/bash
#
# Copyright 2026 The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

if [[ -n "${KUBETEST2_RUN_DIR:-}" ]]; then
	export CLUSTER_NAME="${GKE_CLUSTER_NAMES}"
	export PROJECT="${GKE_CLUSTER_PROJECTS}"
	export CLUSTER_LOCATION="${GKE_CLUSTER_LOCATIONS}"
	export KUBE_GKE_NETWORK="${GKE_CLUSTER_NAMES}"

	if [[ "${CLUSTER_LOCATION}" =~ -[a-z]$ ]]; then
	  export ZONE="${CLUSTER_LOCATION}"
	else
	  export REGION="${CLUSTER_LOCATION}"
	fi
fi

echo "Installing agentic sandbox core manifest (latest release)"
kubectl apply -f https://github.com/kubernetes-sigs/agent-sandbox/releases/latest/download/manifest.yaml

echo "Installing agentic sandbox extensions (latest release)"
kubectl apply -f https://github.com/kubernetes-sigs/agent-sandbox/releases/latest/download/extensions.yaml

echo "Patching agent-sandbox-controller deployment with performance overrides"
kubectl patch deployment agent-sandbox-controller -n agent-sandbox-system --type=strategic --patch '
spec:
  template:
    spec:
      nodeSelector:
        cloud.google.com/gke-nodepool: controller-pool
      tolerations:
      - key: "controller-pool"
        operator: "Exists"
        effect: "NoSchedule"
      containers:
      - name: agent-sandbox-controller
        args:
        - --leader-elect=true
        - --extensions
        - --enable-pprof-debug
        - --zap-log-level=debug
        - --zap-encoder=json
        - --kube-api-qps=1000
        - --kube-api-burst=2000
        - --sandbox-concurrent-workers=400
        - --sandbox-claim-concurrent-workers=400
        - --sandbox-warm-pool-concurrent-workers=1
        resources:
          requests:
            memory: "12Gi"
            cpu: "12"
'

echo "Verifying patched deployment:"
kubectl get deployment agent-sandbox-controller -n agent-sandbox-system -o yaml

echo "Waiting for agent sandbox controller to be ready"
kubectl wait --for=condition=Ready pod -l app=agent-sandbox-controller -n agent-sandbox-system --timeout=5m || echo "WARNING: Timeout waiting for agent sandbox controller"

# Check b/507017172 for the context of override.
echo "Applying Cilium exclusion for Sandbox unique labels"
kubectl patch cm -n kube-system cilium-config-emergency-override --patch '
data:
  labels: "!agents.x-k8s.io/sandbox-name-hash !agents.x-k8s.io/claim-uid !agents.x-k8s.io/warm-pool-sandbox !agents.x-k8s.io/sandbox-pod-template-hash"
'

kubectl get cm -n kube-system cilium-config-emergency-override -oyaml

echo "Restart KCP"
cluster_location=${REGION:-${ZONE}}
gcloud container clusters upgrade "${CLUSTER_NAME}" \
    --location "${cluster_location}" \
    --project "${PROJECT}" \
    --cluster-version "$(gcloud container clusters describe "${CLUSTER_NAME}" --location "${cluster_location}" --project "${PROJECT}" --format="value(currentMasterVersion)")" \
    --master

echo "Restart anetd"
kubectl rollout restart daemonset anetd -n kube-system && \
kubectl rollout status daemonset anetd -n kube-system --timeout=10m || \
echo "WARNING: Timeout waiting for anetd daemonset restart"

echo "Pre-test: running perf-tests/run-pre-test.sh"
"${GOPATH}"/src/gke-internal.googlesource.com/test-infra/perf-tests/run-pre-test.sh "$@"

echo "Installing agent-sandbox pprof scraper config"
kubectl apply -f "${GOPATH}"/src/gke-internal.googlesource.com/test-infra/perf-tests/testing/agentic-sandbox/monitor/pprof-config.yaml
