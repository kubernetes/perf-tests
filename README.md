# Kubernetes perf-tests

[![Build Status](https://travis-ci.org/kubernetes/perf-tests.svg?branch=master)](https://travis-ci.org/kubernetes/perf-tests)  [![Go Report Card](https://goreportcard.com/badge/github.com/kubernetes/perf-tests)](https://goreportcard.com/report/github.com/kubernetes/perf-tests)


This repo is dedicated for storing various Kubernetes-related performance test related tools. If you want to add your own load-test, benchmark, framework or other tool please contact with one of the Owners.

Because in general tools are independent and have their own ways of being configured or run, each subdirectory needs a separate README.md file with the description of its contents.

## Repository setup

To run all verify* scripts before pushing to remote branch (useful for catching problems quickly) execute:

```
cp _hook/pre-push .git/hooks/pre-push
```
