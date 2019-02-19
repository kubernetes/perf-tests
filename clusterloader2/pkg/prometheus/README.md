# Prometheus

Prometheus stack in ClusterLoader2 framework.


## How to connect to Prometheus UI

1. ``kubectl --namespace monitoring port-forward svc/prometheus-k8s 9090 --address=0.0.0.0``
2. Visit http://localhost:9090

## How to connect to Grafana UI

1. ``kubectl --namespace monitoring port-forward svc/grafana 3000 --address=0.0.0.0``
2. Visit http://localhost:3000
3. Login/password: admin/admin