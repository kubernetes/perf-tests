# scratch

Utility image for padding kubemark hosts to achieve more realistic node object sizes. Refer to issue https://github.com/kubernetes/kubernetes/issues/90833.

## Releasing

1. Configure the number of images to build / push.
1. Build with `make build`
1. Release with `make push`
