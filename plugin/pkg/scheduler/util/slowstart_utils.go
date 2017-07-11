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

package util

import (
	"k8s.io/client-go/util/integer"
)

// A StartFunc takes a batchSize and pos (position). The batchSize indicates
// the number of start calls the StartFunc should issue and pos indicates the
// number of previous start calls that have been previously issued by the
// StartFunc during the controller's current sync cycle.  A StartFunc should
// return the number of unsuccessful calls (<= batchSize)
type StartFunc func(int, int) int

// Batch a start operation such as the creation of pods. Batch sizes start at 1
// and double with each successful iteration in a kind of "slow start".  This
// handles attempts to perform large numbers of start operations that would
// likely all fail with the same error. For example, a project with a low quota
// that attempts to create a large number of pods will be prevented from
// spamming the API service with the pod create requests after one of its pods
// fails. Conveniently, this also prevents the event spam that those failures
// would generate.  toStart is the total number of items that should be
// started.  startFunc is a function that can perform the start operation.  The
// return value is the number of skipped start operations as a result of a slow
// start failure. In the case where all start operations succeeded, 0 is
// returned. Otherwise the return value is the number of start attempts
// subtracted from toStart.
func SlowStart(toStart int, startFunc StartFunc) int {
	for batchSize, pos := 1, 0; toStart > pos; batchSize, pos = integer.IntMin(2*batchSize, toStart-(pos+batchSize)), pos+batchSize {
		// any skipped pods that we never attempted to start shouldn't be expected.
		if startFunc(batchSize, pos) > 0 {
			// The skipped pods will be retried later. The next controller resync will
			// retry the slow start process.
			return toStart - batchSize - pos
		}
	}
	return 0
}
