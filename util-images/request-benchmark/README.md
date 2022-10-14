# Request benchmark

This tool generates inflight for a specific URI against Kubernetes control
plane. For example:

``` sh
# The following command assumes that kubeconfig is already set
$ go run . --inflight=1 --uri=/api/v1/pods
# 2022/10/11 12:54:58 Sending requests to '/api/v1/pods' with inflight 1. Press Ctrl+C to stop...
# 2022/10/11 12:54:59 Got response of 90571 bytes in 424.810402ms
# 2022/10/11 12:54:59 Got response of 90571 bytes in 145.033023ms
# 2022/10/11 12:54:59 Got response of 90571 bytes in 137.122805ms
# ...
```

## Releasing

1.  Increment the `TAG` in the `Makefile`.
2.  Run `make all` (or `make build` and then `make push`).
