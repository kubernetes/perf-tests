# API Streaming

Utility image used in [testing the API streaming feature](https://github.com/kubernetes/perf-tests/pull/2255).

Starts X number of informers for the target resource in the target namespace and waits until they are fully synchronised.
Then the test is repeated until specified timeout has elapsed.

Example usage:

```
./watch-list --api-version=apps/v1 --resource=deployments --namespace=default
```

## Building and Releasing

1. Increment the `TAG` in the Makefile.
2. `make build`
3. Test changes with `docker run gcr.io/k8s-staging-perf-tests/watch-list:latest -- ...`
4. Release with `make push`
