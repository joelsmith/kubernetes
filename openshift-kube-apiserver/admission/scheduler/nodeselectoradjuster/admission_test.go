package nodeselectoradjuster

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/authentication/user"
	coreapi "k8s.io/kubernetes/pkg/apis/core"
)

func TestAdmit(t *testing.T) {
	tests := []struct {
		name                   string
		pod                    *coreapi.Pod
		resource               schema.GroupVersionResource
		subresource            string
		expectedNodeSelector   map[string]string
		expectError            bool
	}{
		{
			name: "VPA operator pod: master node selector is removed",
			pod: makePod(
				withLabels(map[string]string{vpaOperatorLabelKey: vpaOperatorLabelValue}),
				withNodeSelector(map[string]string{masterNodeSelectorKey: ""}),
			),
			resource:             coreapi.Resource("pods").WithVersion("v1"),
			expectedNodeSelector: map[string]string{},
		},
		{
			name: "VPA operand pod: master node selector is removed",
			pod: makePod(
				withLabels(map[string]string{vpaOperandLabelKey: vpaOperandLabelValue}),
				withNodeSelector(map[string]string{masterNodeSelectorKey: ""}),
			),
			resource:             coreapi.Resource("pods").WithVersion("v1"),
			expectedNodeSelector: map[string]string{},
		},
		{
			name: "VPA operator pod: master node selector removed, other selectors preserved",
			pod: makePod(
				withLabels(map[string]string{vpaOperatorLabelKey: vpaOperatorLabelValue}),
				withNodeSelector(map[string]string{
					masterNodeSelectorKey:      "",
					"node-role.kubernetes.io/worker": "",
					"topology.kubernetes.io/zone":    "us-east-1a",
				}),
			),
			resource: coreapi.Resource("pods").WithVersion("v1"),
			expectedNodeSelector: map[string]string{
				"node-role.kubernetes.io/worker": "",
				"topology.kubernetes.io/zone":    "us-east-1a",
			},
		},
		{
			name: "VPA operator pod: no master node selector is a no-op",
			pod: makePod(
				withLabels(map[string]string{vpaOperatorLabelKey: vpaOperatorLabelValue}),
				withNodeSelector(map[string]string{"node-role.kubernetes.io/worker": ""}),
			),
			resource:             coreapi.Resource("pods").WithVersion("v1"),
			expectedNodeSelector: map[string]string{"node-role.kubernetes.io/worker": ""},
		},
		{
			name: "VPA operand pod: no node selector at all is a no-op",
			pod: makePod(
				withLabels(map[string]string{vpaOperandLabelKey: vpaOperandLabelValue}),
			),
			resource:             coreapi.Resource("pods").WithVersion("v1"),
			expectedNodeSelector: nil,
		},
		{
			name: "non-VPA pod: master node selector is not removed",
			pod: makePod(
				withLabels(map[string]string{"app": "some-other-app"}),
				withNodeSelector(map[string]string{masterNodeSelectorKey: ""}),
			),
			resource:             coreapi.Resource("pods").WithVersion("v1"),
			expectedNodeSelector: map[string]string{masterNodeSelectorKey: ""},
		},
		{
			name: "pod with no labels: master node selector is not removed",
			pod: makePod(
				withNodeSelector(map[string]string{masterNodeSelectorKey: ""}),
			),
			resource:             coreapi.Resource("pods").WithVersion("v1"),
			expectedNodeSelector: map[string]string{masterNodeSelectorKey: ""},
		},
		{
			name: "VPA operator label with wrong value: not treated as VPA pod",
			pod: makePod(
				withLabels(map[string]string{vpaOperatorLabelKey: "something-else"}),
				withNodeSelector(map[string]string{masterNodeSelectorKey: ""}),
			),
			resource:             coreapi.Resource("pods").WithVersion("v1"),
			expectedNodeSelector: map[string]string{masterNodeSelectorKey: ""},
		},
		{
			name: "VPA operand label with wrong value: not treated as VPA pod",
			pod: makePod(
				withLabels(map[string]string{vpaOperandLabelKey: "not-default"}),
				withNodeSelector(map[string]string{masterNodeSelectorKey: ""}),
			),
			resource:             coreapi.Resource("pods").WithVersion("v1"),
			expectedNodeSelector: map[string]string{masterNodeSelectorKey: ""},
		},
		{
			name: "non-pod resource: request is ignored",
			pod: makePod(
				withLabels(map[string]string{vpaOperatorLabelKey: vpaOperatorLabelValue}),
				withNodeSelector(map[string]string{masterNodeSelectorKey: ""}),
			),
			resource:             coreapi.Resource("nodes").WithVersion("v1"),
			expectedNodeSelector: map[string]string{masterNodeSelectorKey: ""},
		},
		{
			name: "pod subresource: request is ignored",
			pod: makePod(
				withLabels(map[string]string{vpaOperatorLabelKey: vpaOperatorLabelValue}),
				withNodeSelector(map[string]string{masterNodeSelectorKey: ""}),
			),
			resource:             coreapi.Resource("pods").WithVersion("v1"),
			subresource:          "exec",
			expectedNodeSelector: map[string]string{masterNodeSelectorKey: ""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plugin := &nodeSelectorAdjustor{
				Handler: admission.NewHandler(admission.Create),
			}

			attrs := admission.NewAttributesRecord(
				tc.pod,
				nil,
				schema.GroupVersionKind{},
				tc.pod.Namespace,
				tc.pod.Name,
				tc.resource,
				tc.subresource,
				admission.Create,
				nil,
				false,
				fakeUser(),
			)

			err := plugin.Admit(context.TODO(), attrs, nil)
			if (err != nil) != tc.expectError {
				t.Fatalf("expected error=%v, got: %v", tc.expectError, err)
			}

			got := tc.pod.Spec.NodeSelector
			if len(got) != len(tc.expectedNodeSelector) {
				t.Fatalf("node selector length mismatch: expected %v, got %v", tc.expectedNodeSelector, got)
			}
			for k, v := range tc.expectedNodeSelector {
				if got[k] != v {
					t.Errorf("node selector key %q: expected value %q, got %q", k, v, got[k])
				}
			}
		})
	}
}

func TestRequiresNodeSelectorAdjustment(t *testing.T) {
	tests := []struct {
		name     string
		pod      *coreapi.Pod
		expected bool
	}{
		{
			name:     "VPA operator label matches",
			pod:      makePod(withLabels(map[string]string{vpaOperatorLabelKey: vpaOperatorLabelValue})),
			expected: true,
		},
		{
			name:     "VPA operand label matches",
			pod:      makePod(withLabels(map[string]string{vpaOperandLabelKey: vpaOperandLabelValue})),
			expected: true,
		},
		{
			name: "both VPA labels present: matches",
			pod: makePod(withLabels(map[string]string{
				vpaOperatorLabelKey: vpaOperatorLabelValue,
				vpaOperandLabelKey:  vpaOperandLabelValue,
			})),
			expected: true,
		},
		{
			name:     "no labels: no match",
			pod:      makePod(),
			expected: false,
		},
		{
			name:     "unrelated labels: no match",
			pod:      makePod(withLabels(map[string]string{"app": "foo", "version": "v1"})),
			expected: false,
		},
		{
			name:     "VPA operator label with wrong value: no match",
			pod:      makePod(withLabels(map[string]string{vpaOperatorLabelKey: "some-other-operator"})),
			expected: false,
		},
		{
			name:     "VPA operand label with wrong value: no match",
			pod:      makePod(withLabels(map[string]string{vpaOperandLabelKey: "not-default"})),
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := requiresNodeSelectorAdjustment(tc.pod)
			if got != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestIsHosted(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected bool
	}{
		{
			name:     "env var set to 'hosted': IsHosted returns true",
			envValue: "hosted",
			expected: true,
		},
		{
			name:     "env var set to empty string: IsHosted returns false",
			envValue: "",
			expected: false,
		},
		{
			name:     "env var set to another value: IsHosted returns false",
			envValue: "standalone",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(hostedControlPlaneEnvVar, tc.envValue)
			got := IsHosted()
			if got != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

// makePod constructs a coreapi.Pod, applying each option in order.
func makePod(opts ...func(*coreapi.Pod)) *coreapi.Pod {
	p := &coreapi.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-namespace",
		},
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

func withLabels(labels map[string]string) func(*coreapi.Pod) {
	return func(p *coreapi.Pod) {
		p.Labels = labels
	}
}

func withNodeSelector(selector map[string]string) func(*coreapi.Pod) {
	return func(p *coreapi.Pod) {
		p.Spec.NodeSelector = selector
	}
}

func fakeUser() user.Info {
	return &user.DefaultInfo{Name: "testuser"}
}
