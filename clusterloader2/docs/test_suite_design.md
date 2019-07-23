# Cluster loader - TestSuite

## Background

Since 2019 cluster loader become main tool for kubernetes e2e performance tests.
It's very convenient for running single test case, but currently it's impossible
to run the same test with two different sets of parameters in a single job run.
This forces developers to create separate job configs for almost each test.

## TestSuite


```
type TestSuite []TestScenario

type TestScenario struct {
	// Identifier is a unique test scenario name across test suite.
	Identifier string
	// ConfigPath defines path to the config file that satisfy already
	// existing cluster loader API.
	ConfigPath string
	// OverridePaths defines what override files should be applied
	// to the config specified by the ConfigPath.
	OverridePaths []string
}
```

Test scenario level overrides benefits:
- Ability to use the same template with multiple overrides in a single job.
- Less error prone - variables with same names across different configs
could have been accidentally overwritten before.

To make using same test configs in one test suite more user-friendly,
the Identifier field was introduced. This identifier will be used to match
run results to the corresponding test.

## Other

Path to the test suite yaml file will be passed to cluster loader as a flag.
After TestSuite is supported by cluster loader, passing single test config paths
and overrides using flags will be disabled.
