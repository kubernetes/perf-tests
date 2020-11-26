## Ad-hoc scale tests

This directory contains a PoC framework allowing K8s community members to run 
ad-hoc e2e 100 node scale tests. 

## Prerequisites

**Before using this framework, please contact mm4tt@.** 

[TODO(mm44tt)]: <> (Create a mailing group with owners of this framework.)

## How to run an ad-hoc scale test

1. Implement your test in the `run-e2e-test.sh`. See comments in the file for
   instructions on how to connect to a cluster or persist the test results.
2. Send a PR with your changes.
3. In the PR trigger the test by posting the following comment: 
   ```
   /test pull-perf-tests-100-adhoc
   ```
4. You can trigger the test multiple times, but only one instance of a test will
   run at a given time. 
5. If you need to change your test, update the PR with your changes and trigger
   the test again.
6. If you need to change the way cluster is created (e.g. 
   kube-controller-manager qps settings) send a PR to modify the test definition
   in https://github.com/kubernetes/test-infra/blob/master/config/jobs/kubernetes/sig-scalability/sig-scalability-adhoc.yaml
   
## Contact

In case of any questions, reach out to mm4tt@.

