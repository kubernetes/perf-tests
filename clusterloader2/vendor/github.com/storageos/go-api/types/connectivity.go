package types

type TestName string

const (
	APIConnectivity  TestName = "api"
	NatsConnectivity TestName = "nats"
	EtcdConnectivity TestName = "etcd"
)

// ConnectivityResult capture's a node connectivity report to a given target.
type ConnectivityResult struct {
	Target *Node `json:"target"`

	Connectivity map[TestName]bool `json:"connectivity"`

	Timeout bool    `json:"timeout"` // true iff the test timed out before completion
	Errors  []error `json:"errors"`  // errors encountered during testing
}

// Passes returns true iff all tests passed
func (r ConnectivityResult) Passes() bool {
	passes := len(r.Connectivity) > 0

	for _, v := range r.Connectivity {
		passes = passes && v
	}

	return passes
}
