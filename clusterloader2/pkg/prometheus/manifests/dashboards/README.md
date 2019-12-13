# Grafana dashboards

## Intro
This directory contains graphana dashboard definitions.
We are using [grafanalib](https://github.com/weaveworks/grafanalib) to store dashboards as a python code instead of json blob.

## Regenerating `.json` files
Don't modify json files manually – modify respective `.dashboard.py` file and call `./regenerate.sh` script to regenerate all `.json` files in the directory, e.g.


```bash
$ # modify master-dashboard.dashboard.py file

$ ./regenerate.sh
Collecting grafanalib
  Using cached https://files.pythonhosted.org/packages/da/a8/ae814759bc99786f0ed33fae0accc177a513519dcdc0afdd931a89416401/grafanalib-0.5.3-py2-none-any.whl
Collecting attrs (from grafanalib)
  Using cached https://files.pythonhosted.org/packages/23/96/d828354fa2dbdf216eaa7b7de0db692f12c234f7ef888cc14980ef40d1d2/attrs-19.1.0-py2.py3-none-any.whl
Installing collected packages: attrs, grafanalib
Successfully installed attrs-19.1.0 grafanalib-0.5.3
Processing master-dashboard.dashboard.py

$ # Done! You can create PR now!
```

## TODO work
1. Migrate network-programming-latency to grafanalib.
2. Add travis presubmit to verify that `./regenerate.sh` has been called.
