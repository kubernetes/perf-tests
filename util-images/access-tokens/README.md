# Access-tokens

Utility image used in [access token (explanation in issue)](https://github.com/kubernetes/kubernetes/issues/83375) testing.

Generates X number of requests per second to kube-apiserver using Y access tokens where both X, Y are configurable by flags.


## Testing and Usage

1. Build an image with `PROJECT=<TEST-PROJECT> make build`
1. Apply example yaml to your cluster
    * `PROJECT=<TEST-PROJECT> cat example/example.yaml.template | envsubst | kubectl apply -f -`


## Releasing

1. If required, test with steps from `Testing and Usage`
1. Increment the `TAG` in the Makefile
1. Build with `make build`
1. Release with `make push`


## Go Modules

This project uses [Go Modules] to manage the external dependencies.

[Go Modules]: https://github.com/golang/go/wiki/Modules
