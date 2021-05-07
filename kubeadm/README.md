## Scalability tests using kubeadm

Contains scripts for creating a kubeadm cluster on GCE and
running scalability tests on it.

To run these locally see the guide in `create-cluster.sh`.

### File summary

- `ci-e2e.sh`: should be run from the k8s CI. Sets up
[Boskos](https://github.com/kubernetes-sigs/boskos) and runs
`create-cluster.sh`.
- `*.py`: boskos helper scripts.
- `create-cluster.sh`: uses `gcloud` to create VM instances on GCE.
Optionally runs tests once the cluster is up.
- `setup.sh`: this is run on all VMs. Installs k8s DEB packages and
other dependencies like containerd.
- `startup-cp`: this is run after `setup.sh` on the control plane VM.
It creates a control plane node using `kubeadm init` and installs a CNI
plugin.
- `startup-worker`: this is run after `setup.sh` on the worker VMs.
It calls `kubeadm join` so that these machines can join the cluster.
