# API Streaming

Utility image used in [testing the API streaming feature](https://github.com/kubernetes/perf-tests/pull/2255).

Starts X number of secret informers to the target namespaces and waits until they are fully synchronised.
Then the test is repeated until specified timeout has elapsed.

## Building and Releasing

1. Increment the `TAG` in the Makefile.
2. `make build`
3. Test changes with `docker run gcr.io/k8s-staging-perf-tests/watch-list:latest -- ...`
4. Release with `make push`