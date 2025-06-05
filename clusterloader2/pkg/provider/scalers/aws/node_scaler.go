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

// NewNodeScaler creates a new AWS-specific NodeScaler implementation
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

func (s *nodeScaler) ScaleNodes(rate, intervalMins, targetNodes int) error {
	ctx := context.Background()

	// Get fresh ASG information
	asgs, err := s.getClusterASGs(ctx)
	if err != nil {
		return err
	}

	if len(asgs) == 0 {
		return fmt.Errorf("no ASGs found for cluster %s", s.clusterName)
	}

	intervalDuration := time.Duration(intervalMins) * time.Minute

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

		if totalCurrentNodes >= int32(targetNodes) {
			klog.Infof("Target node count reached: current (%d) >= target (%d)", totalCurrentNodes, targetNodes)
			return nil
		}

		scalingStartTime := time.Now()

		// Find ASG with least nodes
		var smallestASG *types.AutoScalingGroup
		var smallestSize int32 = math.MaxInt32

		for _, asg := range asgs {
			currentSize := *asg.DesiredCapacity
			if currentSize < smallestSize {
				smallestASG = &asg
				smallestSize = currentSize
			}
		}

		if smallestASG == nil {
			return fmt.Errorf("no ASGs found to scale")
		}

		// Calculate how many nodes to add this interval
		nodesToAdd := int32(rate)
		remainingToTarget := int32(targetNodes) - totalCurrentNodes
		if nodesToAdd > remainingToTarget {
			nodesToAdd = remainingToTarget
		}

		newSize := smallestSize + nodesToAdd
		klog.Infof("Scaling ASG %s from %d to %d nodes (adding %d nodes)",
			*smallestASG.AutoScalingGroupName, smallestSize, newSize, nodesToAdd)

		input := &autoscaling.UpdateAutoScalingGroupInput{
			AutoScalingGroupName: smallestASG.AutoScalingGroupName,
			DesiredCapacity:      awssdk.Int32(newSize),
			MinSize:              awssdk.Int32(newSize),
			MaxSize:              awssdk.Int32(newSize),
		}

		_, err = s.asgClient.UpdateAutoScalingGroup(ctx, input)
		if err != nil {
			return fmt.Errorf("failed to update ASG %s capacity: %v", *smallestASG.AutoScalingGroupName, err)
		}

		// Wait for ASG to reach desired capacity with interval as timeout
		if err := s.waitForASGCapacity(ctx, *smallestASG.AutoScalingGroupName, newSize, intervalDuration); err != nil {
			return fmt.Errorf("failed to scale at requested rate: %v", err)
		}

		scalingDuration := time.Since(scalingStartTime)
		if scalingDuration > intervalDuration {
			return fmt.Errorf("scaling operation took %v which exceeds the interval of %v - cannot maintain requested rate of %d nodes per %d minutes",
				scalingDuration, intervalDuration, rate, intervalMins)
		}

		// Wait for the remainder of the interval before next scaling operation
		if totalCurrentNodes+nodesToAdd < int32(targetNodes) {
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
	deadline := startTime.Add(timeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out after %v waiting for ASG to reach capacity - cannot maintain requested scaling rate", timeout)
		}

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

		// Wait before checking again
		time.Sleep(30 * time.Second)
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
