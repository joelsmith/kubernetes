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
	"testing"
)

func TestSlowStart(t *testing.T) {
	tests := []struct {
		name        string
		total       int
		limit       int
		maxAttempts int
	}{
		{"med limit", 500, 31, 64},
		{"low limit", 10, 1, 3},
		{"high limit", 500, 400, 500},
		{"no limit", 500, 500, 500},
	}

	for _, test := range tests {
		attempts := 0
		skipped := SlowStart(test.total, func(toStart, pos int) int {
			if pos != attempts {
				t.Errorf("expected pos: %d, got %d for %s", attempts, pos, test.name)
			}
			attempts += toStart
			if attempts > test.limit {
				return integer.IntMin(toStart, attempts-test.limit)
			}
			return 0
		})
		if attempts > test.maxAttempts {
			t.Errorf("too many attempts made. expected: <=%d, got %d for %s", test.maxAttempts, attempts, test.name)
		}
		if attempts < test.limit {
			t.Errorf("too few attempts made. expected: >=%d, got %d for %s", test.limit, attempts, test.name)
		}
		if skipped < test.total-test.maxAttempts {
			t.Errorf("not enough pod starts skipped. expected: >=%d, got %d for %s", test.total-test.maxAttempts, skipped, test.name)
		}
	}
}
