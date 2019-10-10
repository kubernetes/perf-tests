# Kubernetes Perfdash

Kubernetes Perfdash (performance dashboard) is a web UI that collects and displays performance metrics. Performance metrics are created based on performance test results for different nodes numbers, platform types and platform versions.

Perfdash is available at http://perf-dash.k8s.io/

## Supported metrics

* Responsiveness
* Resources
* PodStartup
* TestPhaseTimer
* RequestCount
* RequestCountByClient

Metrics above are available for all kinds of tests divided into load and density subtypes.

## Application server
Application server runs as a deployment on kubernetes cluster. It is hosted on *mungegithub* cluster in *k8s-mungegithub* project.

## Application images
Images are stored in *gcr.io/k8s-testimages* project container registry.
