# ClusterLoader

### Running ClusterLoader

To run ClusterLoader type:
```
go run cmd/clusterloader.go --kubeconfig=kubeConfig.yaml --testconfig=config.json
```
Flags kubeconfig and testconfig are necessary.

#### Flags

 - kubeconfig - path to the kubeconfig file.
 - testconfig - path to the test config file.

### Vendor

Vendor is created using [govendor].


[govendor]: https://github.com/kardianos/govendor
