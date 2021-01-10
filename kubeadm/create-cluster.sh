#!/bin/bash

# Copyright 2021 The Kubernetes Authors.
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

# this script creates a kubeadm cluster on GCE
# to run this locally you need to do the following:
# - create a GCE project
# - create a service account in the project and add a key for it
# - download the key file locally
# - install the Google cloud SDK
# - call "gcloud auth activate-service-account <email> --key-file <key-file>"
# - "export CLOUDSDK_CORE_PROJECT=<project-name>"
# - "export CLOUDSDK_COMPUTE_REGION=<region>"
# - "export CLOUDSDK_COMPUTE_ZONE=<zone>"

set -o errexit
set -o nounset
set -o pipefail
set -o xtrace

# general settings
NUM_WORKERS=${NUM_WORKERS:-2}
IMAGE_FAMILY=${IMAGE_FAMILY:-"ubuntu-2004-lts"}
IMAGE_PROJECT=${IMAGE_PROJECT:-"ubuntu-os-cloud"}
CLUSTER_NAME=${CLUSTER_NAME:-"kubeadm-scale-test"}
CP_NODE_NAME="$CLUSTER_NAME-control-plane"
TMP_DIR=$(mktemp -d)
export KUBECONFIG="$TMP_DIR/kubeconfig.yaml"
MACHINE_TYPE_CP=n1-standard-2
MACHINE_TYPE_WORKER=e2-small
TIMEOUT_CP_READY=600
TIMEOUT_ALL_NODES_JOIN=600
TIMEOUT_ALL_NODES_READY=600s

# disable gcloud prompts
export CLOUDSDK_CORE_DISABLE_PROMPTS=1

# cleanup
cleanup() {
	set +o errexit
	echo "Cleaning up..."
	gcloud compute instance-groups managed delete "$CLUSTER_NAME-worker-group"
	gcloud compute instance-templates delete "$CLUSTER_NAME-worker-template"
	gcloud compute instances delete "$CP_NODE_NAME"
	gcloud compute firewall-rules list --filter network="$CLUSTER_NAME" --format "value(selfLink.basename())" | \
	  xargs gcloud compute firewall-rules delete
	gcloud compute networks delete "$CLUSTER_NAME"
	rm -rf "$TMP_DIR"
}
SKIP_CLEANUP=${SKIP_CLEANUP:-""}
if [[ -z "${SKIP_CLEANUP}" ]]; then
	trap cleanup EXIT
fi

# print the gcloud version
echo "Using gcloud version:"
gcloud -v

# setup the networking
gcloud compute networks create "$CLUSTER_NAME"
gcloud compute firewall-rules create "$CLUSTER_NAME-default-internal" --network "$CLUSTER_NAME" --allow tcp,udp,icmp,sctp --source-ranges "10.0.0.0/8"
gcloud compute firewall-rules create "$CLUSTER_NAME-default-external" --network "$CLUSTER_NAME" --allow "tcp:22,tcp:3389,tcp:6443" --source-ranges "0.0.0.0/0"

# prepare control plane startup script
SETUP_STARTUP_CP=$TMP_DIR/setup-startup-cp.sh
cat setup.sh >> "$SETUP_STARTUP_CP"
cat startup-cp >> "$SETUP_STARTUP_CP"
chmod +x "$SETUP_STARTUP_CP"

# create CP instance and run the startup script on it
# to debug the startup script use:
#   sudo google_metadata_script_runner --script-type startup --debug
# to view the startup logs see:
#   /var/log/syslog
CP_COMMAND=$(gcloud compute instances create "$CP_NODE_NAME" \
	--metadata-from-file startup-script="$SETUP_STARTUP_CP" \
	--image-family "$IMAGE_FAMILY" \
	--image-project "$IMAGE_PROJECT" \
	--machine-type "$MACHINE_TYPE_CP" \
	--network "$CLUSTER_NAME" \
	--labels=cluster="$CLUSTER_NAME" \
	--tags kube-control-plane \
	--format "json(networkInterfaces[0].networkIP, networkInterfaces[0].accessConfigs[0].natIP)")

# wait for kubeadm init
SECONDS=0
set +o errexit
while true; do
	timeout 30 gcloud compute ssh "root@${CP_NODE_NAME}" --command "KUBECONFIG=/etc/kubernetes/admin.conf kubectl version"
	# shellcheck disable=SC2181
	if [[ $? -eq 0 ]]; then
		gcloud compute scp "root@${CP_NODE_NAME}:/etc/kubernetes/admin.conf" "$KUBECONFIG"
		break
	else
		if [[ $SECONDS -ge $TIMEOUT_CP_READY ]]; then
			echo "Error: Failed waiting for control plane to become ready. Dumping its /var/log/syslog:"
			timeout 30 gcloud compute ssh "root@${CP_NODE_NAME}" --command "sudo cat /var/log/syslog"
			exit 1
		fi
		sleep 10
	fi
done
set -o errexit

# replace the private with the public IP in the kubeconfig
PRIVATE_IP=$(echo "$CP_COMMAND" | jq -r '.[] | .networkInterfaces[0].networkIP')
PUBLIC_IP=$(echo "$CP_COMMAND" | jq -r '.[] | .networkInterfaces[0].accessConfigs[0].natIP')
sed -i "s/${PRIVATE_IP}/${PUBLIC_IP}/g" "$KUBECONFIG"

# prepare a join command for workers to join the cluster
set +o xtrace
JOIN_CMD="$(gcloud compute ssh root@"$CP_NODE_NAME" --command "kubeadm token create --print-join-command")"
JOIN_CMD="sudo $JOIN_CMD"

# prepare worker startup script
SETUP_STARTUP_WORKER=$TMP_DIR/setup-startup-worker.sh
cat setup.sh >> "$SETUP_STARTUP_WORKER"
cat startup-worker >> "$SETUP_STARTUP_WORKER"
chmod +x "$SETUP_STARTUP_WORKER"

# start worker VMs and run the startup script that includes the join command
gcloud compute instance-templates create "$CLUSTER_NAME-worker-template" \
	--metadata-from-file startup-script="$SETUP_STARTUP_WORKER" \
	--metadata join-cmd="$JOIN_CMD" \
	--image-family "$IMAGE_FAMILY" \
	--image-project "$IMAGE_PROJECT" \
	--machine-type "$MACHINE_TYPE_WORKER" \
	--network "$CLUSTER_NAME" \
	--labels=cluster="$CLUSTER_NAME" \
	--tags kube-worker
set -o xtrace

gcloud compute instance-groups managed create "$CLUSTER_NAME-worker-group" \
    --base-instance-name "$CLUSTER_NAME-worker" \
    --size "$NUM_WORKERS" \
    --template "$CLUSTER_NAME-worker-template"

# wait for all the nodes to appear
TOTAL_NODES=$((NUM_WORKERS + 1))
SECONDS=0
while true; do
	COUNT=$(kubectl get nodes --no-headers | wc -l)
	if [[ "$COUNT" -eq "$TOTAL_NODES" ]]; then
		break
	else
		if [[ "$SECONDS" -ge "$TIMEOUT_ALL_NODES_JOIN" ]]; then
			echo "Error: Failed waiting for all nodes to appear"
			exit 1
		fi
		sleep 10
	fi
done

# wait for all nodes to be ready
kubectl wait --for=condition=Ready nodes --all --timeout="$TIMEOUT_ALL_NODES_READY"

# run tests
RUN_TESTS=${RUN_TESTS:-""}
if [[ -n "${RUN_TESTS}" ]]; then
	echo "running tests (TODO)..."
fi
