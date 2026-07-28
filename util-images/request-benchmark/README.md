# Request benchmark

A multi-mode benchmark tool for the Kubernetes API server.

## `http` subcommand

Sends HTTP requests to a given URI.

```sh
$ go run . http --inflight=1 --uri=/api/v1/pods
```

## `informer` subcommand

Starts N informers for a given resource, waits for them to fully sync, then repeats until a timeout elapses.

```sh
$ go run . informer --resource=pods --count=4 --timeout=5m
```

## Releasing

1.  Increment the `TAG` in the `Makefile`.
2.  Run `make all` (or `make build` and then `make push`).
