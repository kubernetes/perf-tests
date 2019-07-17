/*
Copyright 2015 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

var app = angular.module('PerfDashApp', ['ngMaterial', 'ngRoute', 'chart.js']);

var PerfDashApp = function(http, scope, route) {
    this.http = http;
    this.scope = scope;
    this.route = route;
    this.jobNames = [];
    this.metricCategoryNames = [];
    this.metricNames = [];
    this.selectedLabels = {};
    this.onClick = this.onClickInternal_.bind(this);
    this.cap = 0;
    this.currentCall = 0;
    this.scope.$on('$routeChangeSuccess', this.routeChanged.bind(this));
    this.lastCall = {jobname: "", metriccategoryname: "", metricname: "", time: Date.now()};
};


PerfDashApp.prototype.onClickInternal_ = function(data, evt, chart) {
    console.log(this, data, evt, chart);
    if (evt.ctrlKey) {
      this.cap = (chart.scale.min + chart.scale.max) / 2;
      this.labelChanged();
      return;
    }

    this.setURLParameters();
    this.http.get("config")
            .success(function(config) {
                     window.open(config["storageUrl"] + "/" +
                              config["logsBucket"]  + "/" +
                              config["logsPath"] + "/" +
                              this.job + "/" +
                              data[0].label + "/",
                              "_blank");
            }.bind(this))
    .error(function(config) {
        console.log("error fetching result");
        console.log(config);
    });
};

// Fetch data from the server and update the data to display
PerfDashApp.prototype.refresh = function() {
    this.http.get("jobnames")
            .success(function(data) {
                this.jobNames = data;
                //init jobName only if needed
                if (this.jobName == undefined || this.jobNames.indexOf(this.jobName) == -1) {
                    this.jobName = this.jobNames[0];
                }
                this.jobNameChanged();
            }.bind(this))
    .error(function(data) {
        console.log("error fetching result");
        console.log(data);
    });
};

// Update the select drop-downs based on the query params.
PerfDashApp.prototype.routeChanged = function(event, data) {
    var app = this;
    angular.forEach(this.route.current.params, function(value, name) {
        switch (name) {
            case "jobname":
                if (app.jobName !== value) {
                    app.jobName = value;
                    app.jobNameChanged();
                }
                break;
            case "metriccategoryname":
                if (app.metricCategoryName !== value) {
                    app.metricCategoryName = value;
                    app.metricCategoryNameChanged();
                }
                break;
            case "metricname":
                if (app.metricName !== value) {
                    app.metricName = value;
                    app.metricNameChanged();
                }
                break;
            default:
                app.selectedLabels[name] = value;
        }
    });
    this.labelChanged();
}

// Update the data to graph, using the selected jobName
PerfDashApp.prototype.jobNameChanged = function() {
    this.setURLParameters();
    this.http.get("metriccategorynames", {params: {jobname: this.jobName}})
            .success(function(data) {
                    this.metricCategoryNames = data;
                    if (this.metricCategoryName == undefined || this.metricCategoryNames.indexOf(this.metricCategoryName) == -1) {
                         this.metricCategoryName = this.metricCategoryNames[0]
                    }
                    this.metricCategoryNameChanged();
            }.bind(this))
    .error(function(data) {
        console.log("error fetching result");
        console.log(data);
    });
};

// Update the data to graph, using the selected jobName
PerfDashApp.prototype.metricCategoryNameChanged = function() {
    this.setURLParameters();
    this.http.get("metricnames", {params: {jobname: this.jobName, metriccategoryname: this.metricCategoryName}})
            .success(function(data) {
                    this.metricNames = data;
                    if (this.metricName == undefined ||  this.metricNames.indexOf(this.metricName) == -1) {
                         this.metricName = this.metricNames[0]
                    }
                    this.metricNameChanged();
            }.bind(this))
    .error(function(data) {
        console.log("error fetching result");
        console.log(data);
    });
};

// Update the data to graph, using the selected metricName
PerfDashApp.prototype.metricNameChanged = function() {
    this.setURLParameters();
    if (
        this.lastCall.jobname == this.jobName &&
        this.lastCall.metriccategoryname == this.metricCategoryName &&
        this.lastCall.metricname == this.metricName &&
        Date.now() < this.lastCall.time
    ) {
        // Preventing initialization calls spamming.
        return;
    }
    var callId = ++this.currentCall;
    this.lastCall = {jobname: this.jobName, metriccategoryname: this.metricCategoryName, metricname: this.metricName, time: Date.now() + 1000};
    this.http.get("buildsdata", {params: {jobname: this.jobName, metriccategoryname: this.metricCategoryName, metricname: this.metricName}})
            .success(function(data) {
                    if (this.currentCall != callId) {
                            return;
                    }
                    this.data = data.builds;
                    this.job = data.job;
                    this.builds = this.getBuilds();
                    this.labels = this.getLabels();
                    this.labelChanged();
            }.bind(this))
    .error(function(data) {
        console.log("error fetching result");
        console.log(data);
    });
};

// Update the data to graph, using selected labels
PerfDashApp.prototype.labelChanged = function() {
    this.setURLParameters();
    this.seriesData = [];
    this.series = [];
    result = this.getData(this.selectedLabels);
    this.options = null;
    var seriesLabels = new Array();
    var a = 0;
    for (; a < result.length; a++) {
        if ("unit" in result[a] && "data" in result[a] && result[a].data != {}) {
            // All the unit should be the same
            this.options = {scaleLabel: "<%=value%> "+result[a].unit, animation: false};
            // Start with higher percentiles, since their values are usually strictly higher
            // than lower percentiles, which avoids obscuring graph data. It also orders data
            // in the onHover labels more naturally.
            seriesLabels = seriesLabels.concat(Object.keys(result[a].data));
        }
    }
    seriesLabels = [...new Set(seriesLabels)]
    seriesLabels.sort();
    seriesLabels.reverse();
    if(this.options == null) {
        return;
    }
    angular.forEach(seriesLabels, function(name) {
        this.seriesData.push(this.getStream(result, name));
        this.series.push(name);
    }, this);
    this.cap = 0;
};

// Overwrite the URL query params with the current drop-down selections.
PerfDashApp.prototype.setURLParameters = function() {
    var newParams = {};
    // By setting all existing params to null in newParams, we will delete label
    // parameters that do not apply to the current selection.
    angular.forEach(this.route.current.params, function(ignore, name) {
        newParams[name] = null;
    });
    newParams["jobname"] = this.jobName;
    newParams["metriccategoryname"] = this.metricCategoryName;
    newParams["metricname"] = this.metricName;
    angular.forEach(this.selectedLabels, function(value, name) {
        newParams[name] = value;
    });
    this.route.updateParams(newParams);
}

// Get all of the builds for the data set (e.g. build numbers)
PerfDashApp.prototype.getBuilds = function() {
    return Object.keys(this.data)
};

// Verify if selected labels are in label set.
function verifySelectedLabels(selectedLabels, allLabels) {
    if (selectedLabels == undefined || allLabels == undefined) {
        return false;
    }

    if (Object.keys(selectedLabels).length != Object.keys(allLabels).length) {
        return false;
    }

    var result = true;
    angular.forEach(selectedLabels, function(value, key) {
        if (!(key in allLabels) || !(value in allLabels[key])) {
            result = false;
        }
    });

   return result;
}

function isEmptySet(obj) {
    var count = 0;
    for (k in obj) {
        if (obj.hasOwnProperty(k)) {
            count++;
        }
    }
    if (count == 0) {
         return true
    }
    if (count == 1) {
         if (obj.hasOwnProperty("")) {
             return true
         }
    }
    return false
}

// Get the set of all labels (e.g. 'resources', 'verbs') in the data set
PerfDashApp.prototype.getLabels = function() {
    // Set is a map of every label name to a set of every label value.
    var set = {};
    angular.forEach(this.data, function(items, build) {
        angular.forEach(items, function(item) {
            angular.forEach(item.labels, function(label, name) {
                if (set[name] == undefined) {
                    set[name] = {}
                }
                set[name][label] = true
            });
        });
    });

    angular.forEach(set, function(labels, name) {
        if (isEmptySet(labels)) {
            delete set[name]
        }
    });
    if (!verifySelectedLabels(this.selectedLabels, set)) {
        this.selectedLabels = {}
    }
    var labels = {};
    angular.forEach(set, function(items, name) {
        labels[name] = [];
        angular.forEach(items, function(ignore, item) {
            if (this.selectedLabels[name] == undefined) {
              this.selectedLabels[name] = item;
            }
            labels[name].push(item)
        }, this);
        labels[name].sort()
    }, this);
    return labels;
};

// Extract a time series of data for specific labels
PerfDashApp.prototype.getData = function(labels) {
    var result = [];
    angular.forEach(this.data, function(items, build) {
        var hasAnyResult = false;
        angular.forEach(items, function(item) {
            var match = true;
            angular.forEach(labels, function(label, name) {
                if (item.labels == undefined || item.labels[name] != label) {
                    match = false;
                }
            });
            if (match) {
                result.push(item);
                hasAnyResult = true;
            }
        });
        if (!hasAnyResult) {
            // We need to add empty object so result series will still correspond to build series
            result.push({});
        }
    });
    return result;
};

// Given a slice of data, turn it into a time series of numbers
// 'data' is an array of APICallLatency objects
// 'stream' is a selector for latency data, (e.g. 'Perc50')
PerfDashApp.prototype.getStream = function(data, stream) {
    var result = [];
    angular.forEach(data, function(value) {
        var x = undefined
        if ("data" in value) {
            x = value.data[stream];
        }
        //This is a handling for undefined values which cause chart.js to not display plots
        //TODO(krzysied): Check whether new version of chart.js has support for this case
        if (x == undefined) {
            x = 0;
        }
        if (this.cap != 0 && x > this.cap) {
            x = this.cap;
        }
        result.push(x);
    }, this);
    return result;
};

app.controller('AppCtrl', ['$scope', '$http', '$interval', '$route', function($scope, $http, $interval, $route) {
    $scope.controller = new PerfDashApp($http, $scope, $route);
    $scope.controller.refresh();

    // Refresh every 10 min.  The data only refreshes every 10 minutes on the server
    $interval($scope.controller.refresh.bind($scope.controller), 600000)
}]);

// Add a dummy route so that we can manipulate URL params.
app.config(function($routeProvider) {
    $routeProvider.when('/', {})
});
