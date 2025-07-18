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

package aws

import (
	"context"
	"fmt"
	"math"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling"
	"github.com/aws/aws-sdk-go-v2/service/autoscaling/types"
	"k8s.io/klog/v2"
	"k8s.io/perf-tests/clusterloader2/pkg/provider/scalers"
)

type nodeScaler struct {
	region      string
	clusterName string
	asgClient   *autoscaling.Client
}

// NewNodeScaler creates a new AWS specific NodeScaler implementation
func NewNodeScaler(region, clusterName string) (scalers.NodeScaler, error) {
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
	)
	if err != nil {
		return nil, fmt.Errorf("unable to load AWS SDK config: %v", err)
	}

	client := autoscaling.NewFromConfig(cfg)

	return &nodeScaler{
		region:      region,
		clusterName: clusterName,
		asgClient:   client,
	}, nil
}

func (s *nodeScaler) ScaleNodes(ctx context.Context, batchSize, intervalSeconds, targetNodes int) error {
	// Get fresh ASG information
	asgs, err := s.getClusterASGs(ctx)
	if err != nil {
		return err
	}

	if len(asgs) == 0 {
		return fmt.Errorf("no ASGs found for cluster %s", s.clusterName)
	}

	intervalDuration := time.Duration(intervalSeconds) * time.Second

	for {
		// Refresh ASG information
		asgs, err = s.getClusterASGs(ctx)
		if err != nil {
			return fmt.Errorf("failed to refresh ASG information: %v", err)
		}

		// Calculate total current nodes
		var totalCurrentNodes int32
		for _, asg := range asgs {
			totalCurrentNodes += *asg.DesiredCapacity
		}

		klog.Infof("Current total nodes: %d, target nodes: %d", totalCurrentNodes, targetNodes)

		if totalCurrentNodes == int32(targetNodes) {
			klog.Infof("Target node count reached: current (%d) == target (%d)", totalCurrentNodes, targetNodes)
			return nil
		}

		scalingStartTime := time.Now()

		// Find ASG with least/most nodes depending on direction
		var selectedASG *types.AutoScalingGroup
		var selectedSize int32
		if totalCurrentNodes < int32(targetNodes) {
			// Scale out: pick ASG with least nodes
			selectedSize = int32(math.MaxInt32)
			for _, asg := range asgs {
				currentSize := *asg.DesiredCapacity
				if currentSize < selectedSize {
					selectedASG = &asg
					selectedSize = currentSize
				}
			}
		} else {
			// Scale in: pick ASG with most nodes
			selectedSize = int32(math.MinInt32)
			for _, asg := range asgs {
				currentSize := *asg.DesiredCapacity
				if currentSize > selectedSize {
					selectedASG = &asg
					selectedSize = currentSize
				}
			}
		}

		if selectedASG == nil {
			return fmt.Errorf("no ASGs found to scale")
		}

		var nodesToChange int32
		if totalCurrentNodes < int32(targetNodes) {
			// Scale out
			nodesToChange = int32(batchSize)
			remainingToTarget := int32(targetNodes) - totalCurrentNodes
			if nodesToChange > remainingToTarget {
				nodesToChange = remainingToTarget
			}
			selectedSize += nodesToChange
			klog.Infof("Scaling OUT ASG %s to %d nodes (adding %d nodes)", *selectedASG.AutoScalingGroupName, selectedSize, nodesToChange)
		} else {
			// Scale in
			nodesToChange = int32(batchSize)
			remainingToTarget := totalCurrentNodes - int32(targetNodes)
			if nodesToChange > remainingToTarget {
				nodesToChange = remainingToTarget
			}
			if selectedSize-nodesToChange < 0 {
				// Don't go below zero
				nodesToChange = selectedSize
			}
			selectedSize -= nodesToChange
			klog.Infof("Scaling IN ASG %s to %d nodes (removing %d nodes)", *selectedASG.AutoScalingGroupName, selectedSize, nodesToChange)
		}

		input := &autoscaling.UpdateAutoScalingGroupInput{
			AutoScalingGroupName: selectedASG.AutoScalingGroupName,
			DesiredCapacity:      awssdk.Int32(selectedSize),
			MinSize:              awssdk.Int32(selectedSize),
			MaxSize:              awssdk.Int32(selectedSize),
		}

		_, err = s.asgClient.UpdateAutoScalingGroup(ctx, input)
		if err != nil {
			return fmt.Errorf("failed to update ASG %s capacity: %v", *selectedASG.AutoScalingGroupName, err)
		}

		// Wait for ASG to reach desired capacity with interval as timeout
		if err := s.waitForASGCapacity(ctx, *selectedASG.AutoScalingGroupName, selectedSize, intervalDuration); err != nil {
			return fmt.Errorf("failed to scale at requested batch size: %v", err)
		}

		scalingDuration := time.Since(scalingStartTime)
		if scalingDuration > intervalDuration {
			return fmt.Errorf("scaling operation took %v which exceeds the interval of %v - cannot maintain requested batch size of %d nodes per %d seconds",
				scalingDuration, intervalDuration, batchSize, intervalSeconds)
		}

		// Wait for the remainder of the interval before next scaling operation
		if totalCurrentNodes != int32(targetNodes) {
			remainingTime := intervalDuration - scalingDuration
			if remainingTime > 0 {
				klog.Infof("Scaling operation completed in %v. Waiting %v before next operation.",
					scalingDuration, remainingTime)
				time.Sleep(remainingTime)
			}
		}
	}
}

// waitForASGCapacity waits until the ASG reaches the desired capacity or times out
func (s *nodeScaler) waitForASGCapacity(ctx context.Context, asgName string, desiredCapacity int32, timeout time.Duration) error {
	klog.Infof("Waiting for ASG %s to reach capacity %d within interval of %v", asgName, desiredCapacity, timeout)

	startTime := time.Now()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled or timed out after %v while waiting for ASG to reach capacity: %w", time.Since(startTime), ctx.Err())
		case <-ticker.C:
			input := &autoscaling.DescribeAutoScalingGroupsInput{
				AutoScalingGroupNames: []string{asgName},
			}

			output, err := s.asgClient.DescribeAutoScalingGroups(ctx, input)
			if err != nil {
				return err
			}

			if len(output.AutoScalingGroups) == 0 {
				return fmt.Errorf("ASG %s not found", asgName)
			}

			asg := output.AutoScalingGroups[0]

			// Count instances that are InService
			inServiceCount := 0
			for _, instance := range asg.Instances {
				if instance.LifecycleState == types.LifecycleStateInService {
					inServiceCount++
				}
			}

			waitTime := time.Since(startTime)
			klog.V(2).Infof("ASG %s - Current InService: %d, Desired: %d (waiting for %v)",
				asgName, inServiceCount, desiredCapacity, waitTime)

			if int32(inServiceCount) >= desiredCapacity {
				klog.Infof("ASG %s reached desired capacity of %d after %v",
					asgName, desiredCapacity, waitTime)
				return nil
			}
		}
	}
}

func (s *nodeScaler) getClusterASGs(ctx context.Context) ([]types.AutoScalingGroup, error) {
	input := &autoscaling.DescribeAutoScalingGroupsInput{}
	var clusterASGs []types.AutoScalingGroup

	paginator := autoscaling.NewDescribeAutoScalingGroupsPaginator(s.asgClient, input)

	for paginator.HasMorePages() {
		output, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get ASGs: %v", err)
		}

		for _, asg := range output.AutoScalingGroups {
			// Look for ASG with tag "kubernetes.io/cluster/{cluster-name}"
			for _, tag := range asg.Tags {
				if *tag.Key == fmt.Sprintf("kubernetes.io/cluster/%s", s.clusterName) {
					clusterASGs = append(clusterASGs, asg)
					break
				}
			}
		}
	}

	return clusterASGs, nil
}
