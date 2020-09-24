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

import clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"

// Conditions and condition Reasons for the KubeadmControlPlane object

const (
	// MachinesReady reports an aggregate of current status of the machines controlled by the KubeadmControlPlane.
	MachinesReadyCondition clusterv1.ConditionType = "MachinesReady"
)

const (
	// CertificatesAvailableCondition documents that cluster certificates were generated as part of the
	// processing of a a KubeadmControlPlane object.
	CertificatesAvailableCondition clusterv1.ConditionType = "CertificatesAvailable"

	// CertificatesGenerationFailedReason (Severity=Warning) documents a KubeadmControlPlane controller detecting
	// an error while generating certificates; those kind of errors are usually temporary and the controller
	// automatically recover from them.
	CertificatesGenerationFailedReason = "CertificatesGenerationFailed"
)

const (
	// AvailableCondition documents that the first control plane instance has completed the kubeadm init operation
	// and so the control plane is available and an API server instance is ready for processing requests.
	AvailableCondition clusterv1.ConditionType = "Available"

	// WaitingForKubeadmInitReason (Severity=Info) documents a KubeadmControlPlane object waiting for the first
	// control plane instance to complete the kubeadm init operation.
	WaitingForKubeadmInitReason = "WaitingForKubeadmInit"
)

const (
	// MachinesSpecUpToDateCondition documents that the spec of the machines controlled by the KubeadmControlPlane
	// is up to date. Whe this condition is false, the KubeadmControlPlane is executing a rolling upgrade.
	MachinesSpecUpToDateCondition clusterv1.ConditionType = "MachinesSpecUpToDate"

	// RollingUpdateInProgressReason (Severity=Warning) documents a KubeadmControlPlane object executing a
	// rolling upgrade for aligning the machines spec to the desired state.
	RollingUpdateInProgressReason = "RollingUpdateInProgress"
)

const (
	// ResizedCondition documents a KubeadmControlPlane that is resizing the set of controlled machines.
	ResizedCondition clusterv1.ConditionType = "Resized"

	// ScalingUpReason (Severity=Info) documents a KubeadmControlPlane that is increasing the number of replicas.
	ScalingUpReason = "ScalingUp"

	// ScalingDownReason (Severity=Info) documents a KubeadmControlPlane that is decreasing the number of replicas.
	ScalingDownReason = "ScalingDown"
)

// Too many reasons. Add messages
// Check if there is quorum!!
const (
	// EtcdClusterHealthy documents the overall etcd cluster's health for the KCP-managed etcd.
	EtcdClusterHealthy clusterv1.ConditionType = "EtcdClusterHealthy"

	// EtcdUnknownMemberReason (Severity=Warning) documents that if there exist any node in etcd member list that cannot be associated with KCP machines.
	EtcdUnknownMemberReason = "EtcdUnknownMember"

	// EtcdAlarmExistReason (Severity=Warning) documents that if etcd cluster has alarms armed.
	EtcdAlarmExistReason = "EtcdAlarmExist"

	// EtcdMemberListUnstableReason (Severity=Info) documents if all etcd members do not have the same member-list view.
	EtcdMemberListUnstableReason = "EtcdMemberListUnstable"

	// EtcdMemberNumMismatchWithPodNumReason (Severity=Warning) documents if number of etcd pods does not match with etcd members.
	// This case may occur when there is a failing pod but it's been removed from the member list.
	// TODO: During scale down, etcd quorum may be preserved (cluster remains healthy) but there may be a mismatch between number of pods and members,
	// TODO: while pod is being deleted and removed from the etcd list. This case should be differentiated from this one.
	EtcdMemberNumMismatchWithPodNumReason = "EtcdMemberMismatchWithPod"
)
