# Sleep

Simple program that sleeps for a provided duration.

## Building and Releasing

1. Increment the `TAG` in the Makefile.
2. `make build`
3. Test changes with `docker run gcr.io/k8s-staging-perf-tests/sleep:latest -- ...`
4. Release with `make push`
