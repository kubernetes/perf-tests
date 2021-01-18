/*
Copyright 2019 The Kubernetes Authors.

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

package measurement

import (
	"strings"
	"time"
)

type genericSummary struct {
	name      string
	ext       string
	timestamp time.Time
	content   string
}

// CreateSummary creates generic summary.
func CreateSummary(name, ext, content string) Summary {
	return &genericSummary{
		name:      name,
		ext:       strings.TrimPrefix(ext, "."),
		timestamp: time.Now(),
		content:   content,
	}
}

// SummaryName returns summary name.
func (gs *genericSummary) SummaryName() string {
	return gs.name
}

// SummaryExt returns summary extension.
func (gs *genericSummary) SummaryExt() string {
	return gs.ext
}

// SummaryTime returns summary timestamp.
func (gs *genericSummary) SummaryTime() time.Time {
	return gs.timestamp
}

// SummaryContent returns summary content.
func (gs *genericSummary) SummaryContent() string {
	return gs.content
}
