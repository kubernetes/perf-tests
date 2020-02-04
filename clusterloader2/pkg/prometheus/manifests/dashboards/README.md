# Grafana dashboards

## Intro
This directory contains graphana dashboard definitions. We are using
[grafanalib](https://github.com/weaveworks/grafanalib) to store dashboards as a
python code instead of json blob.

## Regenerating `.json` files
Don't modify json files manually – modify respective `.dashboard.py` file and
call `make all` to regenerate all `.json` files in the directory.


## TODO work
2. Add travis presubmit to verify that `./regenerate.sh` has been called.
