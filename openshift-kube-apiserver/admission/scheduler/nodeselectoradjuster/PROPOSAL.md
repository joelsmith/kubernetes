# Proposal: Node Selector Adjuster Admission Plugin

## Summary

This proposal describes a new kube-apiserver admission plugin that removes the
`node-role.kubernetes.io/master` node selector from qualifying pods when running in
a hosted control plane (HCP) environment. This allows control-plane-adjacent Day 2
operators installed via OLM — operators that intentionally run on master nodes on a
standalone cluster — to run on data plane nodes in an HCP cluster without any manual
intervention from a cluster administrator. The initial consumer of this behaviour is
the Vertical Pod Autoscaler (VPA) operator and its operand pods, but the plugin is
designed to be extensible to other control-plane-adjacent Day 2 operators that have
the same requirement.

## Background

### Standalone clusters

On a traditional standalone OpenShift cluster, most Day 2 operators installed through
OLM run on worker nodes. A subset of operators perform functions that are adjacent to
the control plane — such as resource autoscaling, policy enforcement, or cluster-wide
observability — and intentionally schedule their pods on master nodes to remain
co-located with the control plane components they interact with. These
control-plane-adjacent operators use `node-role.kubernetes.io/master` in their
`spec.nodeSelector` to enforce that placement.

### Hosted control plane clusters

In a Hosted Control Plane (HCP / HyperShift) deployment, the OpenShift control plane
runs inside a separate management cluster as ordinary pods. The guest cluster itself
consists entirely of worker (data plane) nodes — there are no nodes labelled as
masters in the guest cluster.

When a control-plane-adjacent Day 2 operator is installed into such a guest cluster
via OLM, its upstream manifests are applied unchanged. Those manifests carry the
`node-role.kubernetes.io/master` node selector that is correct for a standalone
cluster but matches no node in an HCP guest cluster. As a result the operator and its
operands remain permanently `Pending`. A cluster administrator must manually remove
the node selector from every affected operator subscription and other operand
Deployments to work around the issue.

## Motivation

The root cause is a mismatch between the node topology assumed by
control-plane-adjacent Day 2 operator manifests (master nodes exist) and the actual
topology of an HCP guest cluster (no master nodes). The fix must not require changes
to the operator manifests: OLM currently provides no means of installing different
operator manifests for different cluster topologies. The fix must also not require
any manual steps from the cluster administrator after operator installation via OLM.

An in-process admission plugin running inside the kube-apiserver is the appropriate
place to apply this fix: it intercepts every pod creation request, can identify pods
belonging to known control-plane-adjacent Day 2 operators by their labels, and can
remove the offending node selector transparently before the pod object is persisted.
The operator and its upstream manifests remain unchanged.

## Goals

- Remove `node-role.kubernetes.io/master` from `spec.nodeSelector` of VPA operator
  and VPA operand pods (and similar workloads with the same requirements) at admission
  time on HCP clusters.
- Require zero manual intervention from a cluster administrator after operator
  installation.
- Only activate in hosted control plane environments; have zero overhead and zero
  impact on standalone clusters.

## Non-Goals

- Modifying any field of a pod other than the master node selector key.
- Affecting pods that are not part of a known control-plane-adjacent Day 2 operator
  that has explicitly opted in.
- Implementing any scheduling policy beyond removing the single node selector key.
- Blanket removal of the master node selector from all pods; only pods belonging
  to control-plane-adjacent operators that have been explicitly registered in the
  plugin are eligible.

## Design

### Plugin Location

```
openshift-kube-apiserver/admission/scheduler/nodeselectoradjuster/
```

### Plugin Name

```
scheduling.openshift.io/NodeSelectorAdjuster
```

The name follows the `<group>.openshift.io/<Name>` convention used by other
OpenShift scheduler-category admission plugins.

### Activation Condition

The plugin is activated exclusively in hosted control plane environments. At API
server start-up the plugin inspects the environment variable
`OPENSHIFT_CONTROL_PLANE_TYPE`. If and only if that variable is set to the value
`hosted` does the plugin register itself and join the admission chain. When the
variable is absent or holds any other value the plugin performs no registration and
takes no further action, incurring no cost whatsoever on non-hosted clusters.

### Admission Hook

The plugin implements the `admission.MutationInterface` and handles only the
`CREATE` operation on `Pod` objects in the core (`""`) API group. All other
resources, subresources, and operations are passed through immediately without
inspection.

### Pod Identification

Each control-plane-adjacent Day 2 operator that opts in to master node selector
removal is identified by well-known labels that its operator sets on every pod it
manages. The plugin checks these labels and removes the node selector when a match
is found.

Currently the following workloads are registered:

| Label key                     | Label value                          | Component         |
|-------------------------------|--------------------------------------|-------------------|
| `k8s-app`                     | `vertical-pod-autoscaler-operator`   | VPA Operator      |
| `vertical-pod-autoscaler`     | `default`                            | VPA Operands      |

These label conventions are set by the VPA operator on all pods it manages. Future
control-plane-adjacent Day 2 operators that use the master node selector can be added
by extending the `requiresNodeSelectorAdjustment` function in `admission.go`.

### Mutation

When a matching pod is identified, the plugin deletes the key
`node-role.kubernetes.io/master` from `spec.nodeSelector`. If that key is not
present in the node selector the deletion is a no-op and the pod is admitted
without modification.

The plugin never rejects pods; it only mutates them.

### Plugin Ordering

The plugin is inserted into `openshiftAdmissionPluginsForKubeBeforeMutating`,
which places it before the mutating webhook stage. This is consistent with all
other OpenShift mutating admission plugins and ensures the node selector is
removed before any webhook has a chance to observe or re-add it.

Like registration, the plugin name is only appended to the ordered chain when
`OPENSHIFT_CONTROL_PLANE_TYPE=hosted` is detected at start-up.

## Alternatives Considered

### Use a different set of operator manifests when installing OLM operators on HCP

OLM is considering a feature where operator manifests can be templated. Potentially
this would allow the operator manifests to be installed with no node selectors
in HCP-topology clusters and with master node selectors in stand-alone clusters.
Unfortunately, the feature is in the early stages and doesn't yet have a firm
timeline for implementation.

### Require the Day 2 operator to detect the HCP environment and update its operands

In some cases, the operator itself doesn't need to run on a master node on a
stand-alone cluster. In such cases, the operator can run on the guest cluster
and modify the deployments of its operands to remove the master node selector,
making this admission plugin redundant for those operators. But in some cases
the operator also needs to run on a master node in a stand-alone cluster. Because
OLM provides no mechanism to install different manifests per cluster topology, the
operator's Deployment manifest cannot be altered to remove the master node selector
for HCP without equally removing it for standalone clusters. In the case of the VPA,
the operator runs with elevated privileges so that it can create VPA operand pods
and grant the RBAC privileges needed to run a mutating webhook. This privilege
can be used for privilege escalation, and so for security reasons the VPA operator
should run on a master node on stand-alone clusters. The typical security posture
of HCP clusters is that the cluster itself is the security boundary rather than the
individual node, so it is acceptable to run the VPA operator on the guest cluster
in an HCP environment.

### Use a MutatingWebhookConfiguration

A standalone mutating webhook could perform the same mutation. However, webhooks
require a running service, TLS certificates, and additional infrastructure. An
in-process admission plugin has no such requirements and is far simpler to operate
and reason about.

### Require cluster administrators to remove the node selector manually

A cluster administrator could remove the master node selector from each affected
Deployment after installation. This is the current state without this feature, and it
is precisely the manual intervention that this proposal aims to eliminate.

## Implementation

See `admission.go` in the same directory.
