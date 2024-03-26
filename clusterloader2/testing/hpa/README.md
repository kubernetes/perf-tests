## HPA controller load test

Assumptions:
- Cluster master noded are accessible via SSH.
- The `/metrics` endpoint in the kube-controller-manager (KCM) can be accessed by anonymous users.
  `--authorization-always-allow-paths` can be useful to achieve this.

HPA load test perform the following steps:
- Create replicasets, services, HPAs in a set of namespaces.
  - The namespaces used here are managed by CL2.
  - The number of HPAs in each namespace can be specified using `CL2_HPA_PER_NAMESPACE`.
- Periodically measure the HPA controller workqueue depth in each KCM instance
  - In each scrape, we use the max value among them since the leader will have more items in the workqueue.
  - We keep tracking the smallest value over time.
- Wait 2 minutes to allow HPA controller to reconcile HPA objects multiple rounds given the default reconciliation
  interval for HPA controller is 15s.
- Compare the minimum workqueue backlog with `CL2_KCM_HPA_ALLOWED_BACKLOG`. If exceeding, the test will fail.
  If unspecified, the test will always pass. The metrics will output in json format.
