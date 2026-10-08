/*
Copyright 2017 The Kubernetes Authors.

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

package main

import "regexp"
import "strings"

var /* const */ versionRegexp = regexp.MustCompile("^v[0-9].*")

// ec2InstanceIDHexRegexp matches the hex part of an EC2 instance id (i-0e39c6d2c2304ee3c),
// which kops uses as the node name on AWS. Most ids contain an "a" or "e", so looksLikeHash
// misses ~90% of them and every node ends up as its own series.
var /* const */ ec2InstanceIDHexRegexp = regexp.MustCompile("^[0-9a-f]{17}$")

// RemoveDisambiguationInfixes removes (from pod/container names) version strings and hashes inserted replication controllers and the like.
func RemoveDisambiguationInfixes(podAndContainer string) string {
	split := strings.SplitN(podAndContainer, "/", 2)
	if len(split) < 2 {
		return podAndContainer
	}
	pod, container := split[0], split[1]
	pieces := strings.Split(pod, "-")
	var last string
	for i, piece := range pieces {
		// Only strip version/hash segments after we've already seen a stable pod-name prefix.
		if i > 0 && (looksLikeHash(piece) || versionRegexp.MatchString(piece) || isEC2InstanceID(pieces[i:])) {
			break
		}
		last = strings.Join(pieces[:i+1], "-")
	}
	return strings.Join([]string{last, container}, "/")
}

// looksLikeHash returns true if piece seems to be one of those pseudo-random disambiguation strings
func looksLikeHash(piece string) bool {
	return len(piece) >= 4 && !strings.ContainsAny(piece, "eyuioa")
}

// isEC2InstanceID returns true if the remaining pieces start with an EC2 instance id ("i", "<17 hex>"),
// or with just its hex part (when "i" was the first piece and is kept as the stable prefix).
func isEC2InstanceID(rest []string) bool {
	if len(rest) >= 2 && rest[0] == "i" && ec2InstanceIDHexRegexp.MatchString(rest[1]) {
		return true
	}
	return ec2InstanceIDHexRegexp.MatchString(rest[0])
}
