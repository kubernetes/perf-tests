# Cluster Loader

Cluster Loader is a tool we use to deploy large numbers of various Kubernetes objects to a cluster. 

Currently, Cluster Loader has only been tested against local clusters.

## Building
In order to vendor in e2e/framework & deps we have used [glide](https://github.com/Masterminds/glide).
After installing glide just run:
```
cd perf-tests/clusterloader/
glide install -v
go test -c -o e2e.test
```

## Running
Cluster Loader uses e2e/framework as a library so the execution may be familiar. This command is used to test against a local cluster:
```
./e2e.test --host="127.0.0.1:8080" --ginkgo.v=true --ginkgo.focus="Cluster Loader" --kubeconifg=<path to your kubeconfig> --viper-config=config/test
```
`viper-config` does not need the extension of the config file, viper will automatically detect it.


## Config

The config file is a basic yaml file that follows the following structure:

```
# e2e specific vars
provider: local
# delete namespace after exec 
deletenamespace: true
# Cluster loader specific part
ClusterLoader:
  projects:
    - num: 2
      basename: nginx-explorer
      tuning: default
      templates:
        - num: 10
          file: nginx.yaml
        - num: 1
          file: explorer-pod.yaml
    - num: 2
      basename: es-guestbook
      templates:
        - num: 1
          file: guestbook-aio.yaml
        - num: 5
          file: elasticsearch.yaml
    - num: 1
      basename: clusterproject
      tuning: default
      pods:
        - num: 50
          image: gcr.io/google_containers/pause-amd64:3.0
          basename: pausepods
          file: pod-pause.json
  tuningsets:
    - name: default
      pods:
        stepping:
          stepsize: 5
          pause: 3      # seconds
          timeout: 300  # seconds
        ratelimit:
          delay: 100    # milliseconds
      templates:
        stepping:
          stepsize: 2
          pause: 2      # seconds
          timeout: 120  # seconds
        ratelimit:
          delay: 200    # milliseconds
```

We can create multiple namespaces (projects), that each contain multiple 'templates'/AIO k8s files or multiple pods.

The tuning sets allow stepping as well as rate limiting. The stepping will pause for M seconds after each N objects are created. Rate limiting will wait M milliseconds between creation of objects.

The configuration files for Cluster Loader are found in the config/ subdirectory, and the pod files and template files referenced in these configs (as above) are found in the content/ subdirectory.
