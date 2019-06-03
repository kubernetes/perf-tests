# Logviewer

### Bazel and gazelle
1. To regeneate BUILD.bazel files please run `bazel run //:gazelle`.
2. This will work as long as imports in `.proto` files are relative to WORKSPACE file and imports in `.go` files start with `k8s.io/perf-tests/logviewer/`.


### Folder name
This directory is named '_logviewer' to exclude this file from k8s.io/perf-tests package (it doesn't build as a package without bazel's help).
