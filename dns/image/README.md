# dnsperf image

This directory contains the build for the dnsperf image used for dns
performance testing.

* `make` will create the image.
* `make push` will push the image to the container registry.

`REGISTRY` and `TAG` environment variables will change the registry and tag the
image is built with.
