# Cluster loader vision

Author: wojtek-t

Last update time: 1st Aug 2018

## Background

As of 31/03/2018, all our scalability tests, are regular e2e tests written in Go.
It makes them really unfriendly for developers not working on scalability who just
want to load test Kubernetes features they are working on. Doing so in many
situations requires understanding how those tests really work, modify their code
to test the new feature and only then run and debug tests. Alternatively, they
may create a dedicated load test for their particular feature on their own, which
may be easier, but on the other hand may not exercise important metrics that our
performance tests check. This workflow is far from optimal.

That said, long time ago we came up with the idea that users should be able to
just bring their own objects definitions in JSON format, potentially annotate them
with some metadata to describe how the load should be generated from these and
testing infrastructure should do everything else automatically.

In early 2017 the prototype of “Cluster Loader” has been created. It proved that
configuring tests with json/yaml files is possible. But its functionality is very
limited and it is very far from enabling migration any of existing scalability
tests to that framework.

We would like to get back to that and build fully functional Cluster Loader and
use it as a framework to run all our scalability tests. This doc is describing
high-level vision of how this will work.


## Vision

At the high level, a single test will consist of a number of steps. In each of
those steps we will be creating, updating and/or deleting a number of different
objects.
Additionally, we will introduce a set of predefined operations (that user will be
able to use as phases). They will allow users to monitor/measure performance impact
of user-defined phases.
The following subsections will describe this in a bit more detail.

### Config

A single test scenario will be defined by a `Config`. Its schema will be as following:

```
struct Config {
	// Number of namespaces automanaged by ClusterLoader.
	Namespaces int32
	// Steps of the test.
	Steps []Step
	// Tuning sets that are used by steps.
	TuningSets []TuningSet
}
```

With a test being defined by a single json/yaml file, it should be pretty simple
to modify scenarios, and fork them to new ones.

Note that before running any steps, ClusterLoader will create all the requested
namespaces and after running all of them will delete them (together with all
objects that remained undeleted after running the test). Namespaces are described
in more details in the later part of this document.

### Step

Each step will consist of a number of create, update and delete operations (potentially
on many different object types) or a number of monitoring/measurement-related actions.
A single step is defined as following:

```
struct Step {
	// Only one of these can be non-empty.
	Phases       []Phase
	Measurements []string
	Name         string
}
```

We make `Phases` and `Measurements` separate concepts to ensure correct ordering
between those two types of actions. It's very important to ensure that proper
measurements are started before we start given actions and they are stopped when
all actions are done.

Also note that all `Phases` and `Measurements` within a single `Step` will be
run in parallel - a `Step` ends when all its `Phases` or `Measurements` finish.
That also means, that individual steps run in serial.

Step has optional `Name`. If step is named, a timer will be fired
for that step automatically.

### Phase

Phase declaratively defines state of objects we should reach in the underlying
Kubernetes cluster. A single declaration may result in a number of create, update
and delete operations depending on the current state of the cluster.

We define the phase as following:

```
struct Phase {
	// Set of namespaces in which objects should be reconciled.
	// If null, objects are assumed to be cluster scoped.
	NamespaceRange *NamespaceRange
	// Number of instances of a given object to exist in each
	// of referenced namespaces.
	ReplicasPerNamespace int32
	// Name of TuningSet to be used.
	TuningSet string
	// A set of objects that should be reconciled.
	Objects []Object
}
```

```
struct Object {
	// Type definition for a given object.
	ObjectType ObjectType
	// Base name from which names of objects will be created.
	// Names of objects will be "basename-0", "basename-1", ...
	Basename string
	// A file path to object definition.
	ObjectTemplatePath string
}
```

The semantic of the above structure will be as following:
- `Phases` within a single `Step` will be run in parallel (to recall,
  individual `Steps` run in serial).
- `Objects` within a single `Phase` will be reconciled in serial for a given
  (namespace, replica number) pair. For different (namespace, replica number)
  pairs they will be spread using a given tuning set.

The rationale for having such structure is the following:
- `Objects` represent a collection of Kubernetes objects that can be logically
  though of as a unit of workload (e.g. application comprised of a service,
  deployment and a volume). Conceptually, this collection is out unit of
  replication. Note that we process the `Objects` slice serially which allows
  ordering between objects of a unit (e.g. create a service before deployment).
  The replication itself is done according to `TuningSet` and
  `ReplicasPerNamespace` parameters of the `Phase`.
- Running multiple `Phases` in parallel allows to run different workloads at the
  same time. As an example, it allows to create two different types of
  applications in parallel (possibly using different tuning sets).
- Running `Steps` in serial allow you to synchronize between `Phases` (and
  for example block finishing measurement on all phases from previous step
  to be finished).

Within an single `Phase` we make an explicit assumption that if `ReplicasPerNamespace`
changes, any of `ObjectTemplatePath` cannot change at the same time (assuming it
already exists for a given set of objects). That basically means that within a
single `Phase` operations for a given `Object` may only be of a single type
(create, update of delete).

All of the objects are assumed to be units of workload.
Therefore, if an object comes with dependents, all of its dependents will be affected
by the operation performed on this object. E.g. removing instance of a `ReplicationCotroller`
will also result with removing depended `Pods`.

To make it more explicit:
- if `ReplicasPerNamespace` is different than it previously was, we will create
  or delete a number of objects to reach expected cluster state
- if `ReplicasPerNamespace` is the same as it previously was, we will update all
  objects to the referenced template.

An appropriate validation will be added to cluster loader to ensure the above for
a given input config.

Note that (namespace number, object type, basename) tuple defines a set of
replicated objects.

All `Object` changes for a given (namespace, replica number) pair are treated as
a unit of action. Such units will be spread over time using a referenced tuning
set (described below).

The following definition makes the API declarative and thus a bit similar with
Kubernetes API.

Caveats:

- Note that even with such declarative approach, we may e.g. express a phase of
  randomly scaling a number of objects. This would be possible by expressing e.g.
  `spec.replicas: <3+RAND()%5>` in DeploymentSpec.
  This will require evaluating templates once for every object, but that should
  be fine.

### Multiple copies of the same workload

To fill-in large clusters, we would need to spread objects across different namespaces.
In many cases, it will be enough for users for many namespaces to contain the same
objects (or to be more specific: objects created from the same templates). Obviously,
we want the config for the test to be as small as possible.
As a result, we will introduce the following rules:
- In the top-level test definition, we will define the number of namespaces that will
  be automanaged by ClusterLoader.
- The automanaged namespaces will have names of the form “namespace-<number>” for
  number in range 1..N (where N is the number of namespaces in a test)

However, users may want to create their own namespaces (as part of `Phases`) and
create objects in them. That is perfectly valid usecase that will be supported.

To make it possible to reference a set of namespaces (both automanaged and user-created),
we introduce the following type:

```
struct NamespaceRange {
	Min int32
	Max int32
	Basename *string
}
```

The `NamespaceRange` would select all namespace `\<Basename\>-\<i\>` for `i` in the
range [Min, Max]. If `Basename` is unset, it will be default to the basename used for
automanaged namespaces (i.e. `namespace`).

#### Defining object type

In order to update or delete an object, users need to be able to define type of
object that this operation is about. Thus, we introduce  the following type for
this purpose:

```
struct ObjectType {
	APIGroup   string
	APIVersion string
	Kind       string
}
```

Using this will allow us to easily use dynamic client in most of the places
which may significantly simplify Cluster Loader itself.

#### Tuning Set

Since we would like to be able to fully load even very big clusters, we need to
be able to create a number of “similar” objects. A “Tuning Set” concept will allow
us to spread those operations across some time.
We define Tuning Set as following:

```
struct TuningSet {
	Name         string
	InitialDelay time.Duration
	// Exactly one of the following should be set.
	QpsLoad        *QpsLoad
	RandomizedLoad *RandomizedLoad
	SteppedLoad    *SteppedLoad
}

// QpsLoad defines a uniform load with a given QPS.
struct QpsLoad {
	Qps float
}

// RandomizedLoad defines a load that is spread randomly
// across a given total time.
struct RandomizedLoad {
	AverageQps float
}

// SteppedLoad defines a load that generates a burst of
// a given size every X seconds.
struct SteppedLoad {
	BurstSize int32
	StepDelay time.Duration
}
```

More policies can be introduced in the future.

### Measurements

A critical part of Cluster Loader is an ability to check whether tests (defined by
configs) are satisfying set of Kubernetes performance SLOs.
Fortunately, for testing a specific functionality, we don’t really change the SLO.
We may want to, from time to time, tweak how do we measure existing SLOs or introduce
a new one, but it is fine to require changes to the framework to achieve that.

As a result, mechanisms to measure specific SLOs (or gather other types of metrics)
will be incorporated into Cluster Loader framework. We will expect that developers
trying to introduce a new SLO (or change how do we measure the existing one) will
be modifying that part of Cluster Loader codebase. Within the codebase, we will try
to provide a relatively easy framework to achieve it though.

At the high level, to implement gathering a given portion of data or measure a new
SLO, you will need to implement a very simple Go interface:

```
type Measurement interface {
	Execute(config *MeasurementConfig) error
}

// An instance of below struct would be constructed by clusterloader during runtime
// and passed to the Execute method.
struct MeasurementConfig {
	// Client to access the k8s api.
	Clientset *k8sclient.ClientSet
	// Interface to access the cloud-provider api (can be skipped for initial version).
	CloudProvider *cloudprovider.Interface
	// Params is a map of {name: value} pairs enabling for injection of arbitrary
	// config into the Execute method. This is copied over from the Params field
	// in the Measurement config (explained later) as it is.
	Params map[string]interface{}
}
```

Once you implement such an interface, registering it in the correct
place will allow you to use those as phases in your config.
As an example, consider gathering resource usage from system components.
It will be enough to implement something like the following:

```
struct ResourceGatherer {
	// Some fields that you need.
}

func (r *ResourceGatherer) Execute(c MeasurementConfig) error {
	if c.Params["start"] {
		// Initialize gatherer.
		// Start the gathering goroutines.
		return nil
	}
	if c.Params["stop"] {
		// Stop the gatherer goroutines.
		// Validate and/or save the results.
		return nil
	}
	// Handling of any other potential cases.
}
```

and registering this type in some factory, to enable use of `ResourceGatherer`
as a measurement 'Method' in your test. And finally, at the config level,
each `Measurement` is defined as:

```
struct Measurement {
	// The measurement method to be run.
	// Such method has to be registered in ClusterLoader factory.
	Method string
	// Identifier is a string for differentiating this measurement instance
	// from other instances of the same method.
	Identifier string
	// Params is a map of {name: value} pairs which will be passed to the
	// measurement method - allowing for injection of arbitrary parameters to it.
	Params map[string]interface{}
}
```

To begin with, we will provide few built-in measurement methods like:

```
	ResourceGatherer
	ProfileGatherer
	MetricsGatherer
	APICallLatencyValidator
	PodStartupLatencyValidator
```


## Future enhancements

This section contains future enhancements that will need to happen, but not
necessary at the early beginning.

1. Simple templating in json files.
   This would be extremely useful (necessary) feature to enable referencing
   objects from other objects. As an example, let's say that we want to reference
   secret number `i` from deployment number `i`.
   We would achieve that by providing very simple templating mechanism at the
   level of files with object definitions. The exact details are TBD, but the
   high-level proposal is to:
   - use the `{{ param }}` syntax for templates
   - support only very simply mathematical operations, and symbols:
     - `N` would mean the number of that object (as defined in `basename-<N>`)
     - `RAND` will be random integer
     - `%` (modulo) operation will be supported
     - `+` operation will be supported
   - though, only simple expressions (like {{ N+i%5 }} or {{ RAND%3+5 }} will
     be supported (at least initially).

2. Feedback loop from monitoring.
   Assume that we defined some SLO (that Cluster Loader is able to measure) and
   now we want to understand what conditions need to be satisfied to meet this
   SLO (e.g. what throughput we can support to meet the latency SLO).
   Providing a feedback loop from measurements to load generation tuning can
   solve that problem automatically for us.
   There are a number of details that needs to be figured out to do that, that's
   not needed for the initial version (or for migration existing scalability
   tests), but that should also happen once the framework is already usable.
