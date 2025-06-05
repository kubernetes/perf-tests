/*
Copyright 2025 The Kubernetes Authors.

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

package scalers

import "context"

// NodeScaler defines interface for scaling nodes in a cluster
type NodeScaler interface {
	// ScaleNodes scales the cluster to the target number of nodes at the specified rate
	// batchSize: number of nodes to add or remove per interval
	// intervalSeconds: seconds to wait between scaling operations
	// targetNodes: desired final number of nodes
	ScaleNodes(ctx context.Context, batchSize, intervalSeconds, targetNodes int) error
}
