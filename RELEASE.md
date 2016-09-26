# Perf-tests release process

Performance tests are released around main Kubernetes release dates, to keep tests which given release went through. The process is as follows:

1. Kubernetes minor release vX.X is published
1. Issue for cutting release vX.X for perf tests is created
1. All [OWNERS](OWNERS) lgtm the release
1. An OWNER creates the release branch from the master
1. Ann announcement email is sent to `kubernetes-dev@googlegroups.com`
