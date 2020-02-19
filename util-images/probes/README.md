# Probes

Probes are simple jobs running inside kubernetes cluster measuring SLIs that could not be measured through other means.
## Probe Types
  
### Ping Client

This probe exports the `probes_in_cluster_network_latency_seconds` metric.
It implements the [Network Latency Scalability SLI]

#### Running locally

```
go run cmd/main.go --mode=ping-client --metric-bind-address=:8071 --ping-server-address=127.0.0.1:8081 --stderrthreshold=INFO
```

### Ping Server

This probe doesn't export any metrics, it's needed for the **Ping Client** to work. 

#### Running locally
```
go run cmd/main.go --mode=ping-server --metric-bind-address=:8070 --ping-server-bind-address=0.0.0.0:8081 --stderrthreshold=INFO
```


## Building and Releasing

1. Increment the `TAG` in the Makefile.
2. `make build`
3. Test changes with `docker run gcr.io/k8s-testimages/probes:latest -- ...`
4. Release with `make push`


## Go Modules

This project uses [Go Modules] to manage the external dependencies. 


[Go Modules]: https://github.com/golang/go/wiki/Modules
[Network Latency Scalability SLI]: https://github.com/kubernetes/community/blob/master/sig-scalability/slos/network_latency.md
