package nodeselectoradjuster

// The NodeSelectorAdjustor admission plugin removes the node-role.kubernetes.io/master
// node selector from qualifying pods at creation time. It only activates when the
// OPENSHIFT_CONTROL_PLANE_TYPE environment variable is set to "hosted", which
// indicates a HyperShift hosted control plane topology where dedicated master nodes
// do not exist in the management cluster.

import (
	"context"
	"fmt"
	"io"
	"os"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiserver/pkg/admission"
	coreapi "k8s.io/kubernetes/pkg/apis/core"
)

const (
	// PluginName is the name used to identify this plugin in the admission chain.
	PluginName = "scheduling.openshift.io/NodeSelectorAdjuster"

	// masterNodeSelectorKey is the node selector key removed from qualifying pods
	// in hosted control plane environments.
	masterNodeSelectorKey = "node-role.kubernetes.io/master"

	// vpaOperandLabelKey / vpaOperandLabelValue identify VPA operand pods that
	// opt in to master node selector removal.
	vpaOperandLabelKey   = "vertical-pod-autoscaler"
	vpaOperandLabelValue = "default"

	// vpaOperatorLabelKey / vpaOperatorLabelValue identify the VPA operator pod
	// that opts in to master node selector removal.
	vpaOperatorLabelKey   = "k8s-app"
	vpaOperatorLabelValue = "vertical-pod-autoscaler-operator"

	// hostedControlPlaneEnvVar is the environment variable checked at start-up.
	hostedControlPlaneEnvVar = "OPENSHIFT_CONTROL_PLANE_TYPE"
	// hostedControlPlaneEnvValue is the value that activates this plugin.
	hostedControlPlaneEnvValue = "hosted"
)

// IsHosted reports whether the current process is running in a hosted control
// plane environment. It is checked once at start-up to decide whether the plugin
// should register itself.
func IsHosted() bool {
	return os.Getenv(hostedControlPlaneEnvVar) == hostedControlPlaneEnvValue
}

// Register adds the plugin to the admission plugin registry. It must only be
// called when IsHosted() returns true.
func Register(plugins *admission.Plugins) {
	plugins.Register(PluginName, func(_ io.Reader) (admission.Interface, error) {
		return &nodeSelectorAdjustor{
			Handler: admission.NewHandler(admission.Create),
		}, nil
	})
}

// nodeSelectorAdjustor implements admission.MutationInterface.
type nodeSelectorAdjustor struct {
	*admission.Handler
}

var _ admission.MutationInterface = &nodeSelectorAdjustor{}

// Admit examines newly-created Pod objects and removes the master node selector
// from qualifying pods.
func (p *nodeSelectorAdjustor) Admit(_ context.Context, attr admission.Attributes, _ admission.ObjectInterfaces) error {
	if attr.GetResource().GroupResource() != corev1.Resource("pods") || attr.GetSubresource() != "" {
		return nil
	}

	pod, ok := attr.GetObject().(*coreapi.Pod)
	if !ok {
		return admission.NewForbidden(attr, fmt.Errorf("unexpected object type: %T", attr.GetObject()))
	}

	if !requiresNodeSelectorAdjustment(pod) {
		return nil
	}

	delete(pod.Spec.NodeSelector, masterNodeSelectorKey)
	return nil
}

// ValidateInitialization satisfies admission.InitializationValidator. The plugin
// has no external dependencies to validate.
func (p *nodeSelectorAdjustor) ValidateInitialization() error {
	return nil
}

// requiresNodeSelectorAdjustment returns true when the pod carries a label that
// opts it in to master node selector removal. Currently the VPA operator and VPA
// operand pods opt in via their well-known labels.
func requiresNodeSelectorAdjustment(pod *coreapi.Pod) bool {
	labels := pod.Labels
	if labels[vpaOperandLabelKey] == vpaOperandLabelValue {
		return true
	}
	if labels[vpaOperatorLabelKey] == vpaOperatorLabelValue {
		return true
	}
	return false
}
