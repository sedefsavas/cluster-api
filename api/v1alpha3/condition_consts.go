/*
Copyright 2020 The Kubernetes Authors.

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

package v1alpha3

// ANCHOR: CommonConditions

// Common ConditionTypes used by Cluster API objects.
const (
	// ReadyCondition defines the Ready condition type that summarizes the operational state of a Cluster API object.
	ReadyCondition ConditionType = "Ready"
)

// Common ConditionReason used by Cluster API objects.
const (
	// DeletingReason (Severity=Info) documents an condition not in Status=True because the underlying object it is currently being deleted.
	DeletingReason = "Deleting"

	// DeletionFailedReason (Severity=Warning) documents an condition not in Status=True because the underlying object
	// encountered problems during deletion. This is a warning because the reconciler will retry deletion.
	DeletionFailedReason = "DeletionFailed"

	// DeletedReason (Severity=Info) documents an condition not in Status=True because the underlying object was deleted.
	DeletedReason = "Deleted"
)

const (
	// InfrastructureReadyCondition reports a summary of current status of the infrastructure object defined for this cluster/machine.
	// This condition is mirrored from the Ready condition in the infrastructure ref object, and
	// the absence of this condition might signal problems in the reconcile external loops or the fact that
	// the infrastructure provider does not not implements the Ready condition yet.
	InfrastructureReadyCondition ConditionType = "InfrastructureReady"

	// WaitingForInfrastructureFallbackReason (Severity=Info) documents a cluster/machine waiting for the cluster/machine infrastructure
	// to be available.
	// NOTE: This reason is used only as a fallback when the infrastructure object is not reporting its own ready condition.
	WaitingForInfrastructureFallbackReason = "WaitingForInfrastructure"
)

// ANCHOR_END: CommonConditions

// Conditions and condition Reasons for the Cluster object

const (
	// ControlPlaneReady reports the ready condition from the control plane object defined for this cluster.
	// This condition is mirrored from the Ready condition in the control plane ref object, and
	// the absence of this condition might signal problems in the reconcile external loops or the fact that
	// the control plane provider does not not implements the Ready condition yet.
	ControlPlaneReadyCondition ConditionType = "ControlPlaneReady"

	// WaitingForControlPlaneFallbackReason (Severity=Info) documents a cluster waiting for the control plane
	// to be available.
	// NOTE: This reason is used only as a fallback when the control plane object is not reporting its own ready condition.
	WaitingForControlPlaneFallbackReason = "WaitingForControlPlane"
)

// Conditions and condition Reasons for the Machine object

const (
	// BootstrapReadyCondition reports a summary of current status of the bootstrap object defined for this machine.
	// This condition is mirrored from the Ready condition in the bootstrap ref object, and
	// the absence of this condition might signal problems in the reconcile external loops or the fact that
	// the bootstrap provider does not not implements the Ready condition yet.
	BootstrapReadyCondition ConditionType = "BootstrapReady"

	// WaitingForDataSecretFallbackReason (Severity=Info) documents a machine waiting for the bootstrap data secret
	// to be available.
	// NOTE: This reason is used only as a fallback when the bootstrap object is not reporting its own ready condition.
	WaitingForDataSecretFallbackReason = "WaitingForDataSecret"

	// DrainingSucceededCondition provide evidence of the status of the node drain operation which happens during the machine
	// deletion process.
	DrainingSucceededCondition ConditionType = "DrainingSucceeded"

	// DrainingReason (Severity=Info) documents a machine node being drained.
	DrainingReason = "Draining"

	// DrainingFailedReason (Severity=Warning) documents a machine node drain operation failed.
	DrainingFailedReason = "DrainingFailed"

	// PreDrainDeleteHookSucceededCondition reports a machine waiting for a PreDrainDeleteHook before being delete.
	PreDrainDeleteHookSucceededCondition ConditionType = "PreDrainDeleteHookSucceeded"

	// PreTerminateDeleteHookSucceededCondition reports a machine waiting for a PreDrainDeleteHook before being delete.
	PreTerminateDeleteHookSucceededCondition ConditionType = "PreTerminateDeleteHookSucceeded"

	// WaitingExternalHookReason (Severity=Info) provide evidence that we are waiting for an external hook to complete.
	WaitingExternalHookReason = "WaitingExternalHook"
)

const (
	// MachineHealthCheckSuccededCondition is set on machines that have passed a healthcheck by the MachineHealthCheck controller.
	// In the event that the health check fails it will be set to False.
	MachineHealthCheckSuccededCondition ConditionType = "HealthCheckSucceeded"

	// MachineHasFailureReason is the reason used when a machine has either a FailureReason or a FailureMessage set on its status.
	MachineHasFailureReason = "MachineHasFailure"

	// NodeStartupTimeoutReason is the reason used when a machine's node does not appear within the specified timeout.
	NodeStartupTimeoutReason = "NodeStartupTimeout"

	// UnhealthyNodeConditionReason is the reason used when a machine's node has one of the MachineHealthCheck's unhealthy conditions.
	UnhealthyNodeConditionReason = "UnhealthyNode"
)

const (
	// MachineOwnerRemediatedCondition is set on machines that have failed a healthcheck by the Machine's owner controller.
	// MachineOwnerRemediatedCondition is set to False after a health check fails, but should be changed to True by the owning controller after remediation succeeds.
	MachineOwnerRemediatedCondition ConditionType = "OwnerRemediated"

	// WaitingForRemediationReason is the reason used when a machine fails a health check and remediation is needed.
	WaitingForRemediationReason = "WaitingForRemediation"
)

// Conditions and condition Reasons for the Machine's Node object

const (
	// MachineNodeHealthyCondition provides info about the operational state of the Kubernetes node hosted on the machine by summarizing  node conditions.
	// If the conditions defined in a Kubernetes node (i.e., NodeReady, NodeMemoryPressure, NodeDiskPressure, NodePIDPressure, and NodeNetworkUnavailable) are in a healthy state, it will be set to True.
	MachineNodeHealthyCondition ConditionType = "NodeHealthy"

	// NodeProvisioningReason (Severity=Info) documents machine in the process of provisioning a node.
	// NB. provisioning --> NodeRef == ""
	NodeProvisioningReason = "NodeProvisioning"

	// MachineNodeDeletedReason (Severity=Error) documents a machine's node has previously been observed but is now gone.
	// NB. provisioned --> NodeRef != ""
	NodeNotFoundReason = "NodeNotFound"

	// NodeConditionsFailedReason (Severity=Warning) documents a node is not in a healthy state due to the failed state of at least 1 Kubelet condition.
	NodeConditionsFailedReason = "NodeConditionsFailed"
)

// Common Pod-related Condition Reasons used by Pod-related Conditions.
const (
	// PodProvisioningReason (Severity=Info) documents a pod waiting  to be provisioned i.e., Pod is in "Pending" phase and
	// PodScheduled and Initialized conditions are not set to True yet.
	PodProvisioningReason = "PodProvisioning"

	// PodProvisioningReason (Severity=Warning) documents a pod failed during provisioning i.e., Pod is in "Pending" phase and
	// PodScheduled and Initialized conditions are set to True,
	// but ContainersReady or Ready condition is false (i.e., at least one of the containers are in waiting state(e.g CrashLoopbackOff, ImagePullBackOff)
	PodProvisioningFailedReason = "PodProvisioningFailed"

	// PodMissingReason (Severity=Warning) documents a pod does not exist.
	PodMissingReason = "PodMissing"

	// PodFailedReason (Severity=Error) documents a pod's at least one container has terminated in a failure
	// and hence Pod is in "Failed" phase.
	PodFailedReason = "PodFailed"
)

// Conditions that are only for control-plane machines. KubeadmControlPlane is the owner of these conditions.
const (
	// MachineKubeAPIServerHealthyCondition reports a machine's kube-apiserver's health status.
	// Set to true if kube-apiserver pod is in "Running" phase, otherwise uses Pod-related Condition Reasons.
	MachineKubeAPIServerHealthyCondition ConditionType = "KubeAPIServerHealthy"

	// MachineKubeControllerManagerHealthyCondition reports a machine's kube-controller-manager's health status.
	// Set to true if kube-controller-manager pod is in "Running" phase, otherwise uses Pod-related Condition Reasons.
	MachineKubeControllerManagerHealthyCondition ConditionType = "KubeControllerManagerHealthy"

	// MachineKubeSchedulerHealthyCondition reports a machine's kube-scheduler's health status.
	// Set to true if kube-scheduler pod is in "Running" phase, otherwise uses Pod-related Condition Reasons.
	MachineKubeSchedulerHealthyCondition ConditionType = "KubeSchedulerHealthy"

	// MachineEtcdPodHealthyCondition reports a machine's etcd pod's health status.
	// Set to true if etcd pod is in "Running" phase, otherwise uses Pod-related Condition Reasons.
	MachineEtcdPodHealthyCondition ConditionType = "EtcdPodHealthy"
)

// etcd condition is also for only control-plane machines. KubeadmControlPlane is the owner of this condition.
const (
	// MachineEtcdMemberHealthyCondition documents if the  machine has an healthy etcd member.
	// If not true, can use Pod-related Condition Reasons.
	MachineEtcdMemberHealthyCondition ConditionType = "EtcdMemberHealthy"

	// NodeEtcdMissingFromMemberListReason  (Severity=Warning) documents the machine's corresponding node has a ready etcd pod but not part of etcd members yet.
	NodeEtcdMissingFromMemberListReason = "NodeEtcdMissingFromMemberList"
)
