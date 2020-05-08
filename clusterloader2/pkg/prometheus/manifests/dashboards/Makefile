SOURCES := $(wildcard *.dashboard.py)
DASHBOARDS := $(SOURCES:%.dashboard.py=%.json)

init:
	pip install -r requirements.txt

%.json: %.dashboard.py defaults.py
 	# Setting PYTHONPATH to $PWD is a workaround for https://github.com/weaveworks/grafanalib/issues/58
	PYTHONPATH="$$PYTHONPATH:$(shell pwd)" generate-dashboard  "$<" | python3 -m json.tool > "$@"

all: $(DASHBOARDS)
