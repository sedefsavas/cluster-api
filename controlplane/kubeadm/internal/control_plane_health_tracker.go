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

package internal

import (
	"sync"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/conditions"
)

// Common control-plane pod name prefixes
const (
	KubeAPIServerPodNamePrefix         = "kube-apiserver"
	KubeControllerManagerPodNamePrefix = "kube-controller-manager"
	KubeSchedulerHealthyPodNamePrefix  = "kube-scheduler"
	EtcdPodNamePrefix                  = "etcd"
)

type ConditionReason string

// GetSeverity returns the default severity for the conditions owned by KCP.
func (c ConditionReason) GetSeverity() clusterv1.ConditionSeverity {

	switch {
	case c == ConditionReason(clusterv1.PodProvisioningReason):
		return clusterv1.ConditionSeverityInfo
	case c == ConditionReason(clusterv1.PodMissingReason):
		return clusterv1.ConditionSeverityWarning
	case c == ConditionReason(clusterv1.PodProvisioningFailedReason):
		return clusterv1.ConditionSeverityWarning
	case c == ConditionReason(clusterv1.PodFailedReason):
		return clusterv1.ConditionSeverityWarning
	case c == ConditionReason(controlplanev1.EtcdAlarmExistReason):
		return clusterv1.ConditionSeverityWarning
	case c == ConditionReason(controlplanev1.EtcdMemberNumMismatchWithPodNumReason):
		return clusterv1.ConditionSeverityWarning
	case c == ConditionReason(controlplanev1.EtcdMemberListUnstableReason):
		return clusterv1.ConditionSeverityInfo
	case c == ConditionReason(controlplanev1.EtcdUnknownMemberReason):
		return clusterv1.ConditionSeverityInfo
	default:
		return clusterv1.ConditionSeverityInfo
	}
}

// ControlPlaneHealthTracker follows the singleton pattern and holds KCP related health conditions/structs that may be added in different part of the code.
// Keeping all conditions in one place reduces the complexity of patching them especially since control-plane Machine related conditions are calculated deep in the code.
// This structure can be expanded to keep general health related structs which needs to be calculated only once.
var HealthTrackerInstance *ControlPlaneHealthTracker

type ControlPlaneHealthTracker struct {
	// Machines is a map with machine name as the key and ControlPlaneMachineHealth as the value to keep track of control-plane machine conditions.
	Machines map[string]*ControlPlaneMachineHealth

	// KCP related conditions
	EtcdClusterHealthy *clusterv1.Condition

	// TODO: CHeck if protecting getter/setters is needed
	lock sync.Mutex
}

type ControlPlaneMachineHealth struct {
	// MachineConditions is a map with condition type as the key and Condition as the value.
	MachineConditions map[clusterv1.ConditionType]*clusterv1.Condition
}

// GetHealthTracker creates and returns ControlPlaneHealthTracker object if not exist, return the existing one otherwise.
func GetHealthTracker() *ControlPlaneHealthTracker {
	if HealthTrackerInstance == nil {
		HealthTrackerInstance = &ControlPlaneHealthTracker{Machines: map[string]*ControlPlaneMachineHealth{}, lock: sync.Mutex{}}
	}
	return HealthTrackerInstance
}

// SetMachineConditionFalse stores/updates the condition in the tracker belongs to the Machine with machineName as False.
func (h *ControlPlaneHealthTracker) SetMachineConditionFalse(machineName string, conditionType clusterv1.ConditionType, reason string, message ...string) {
	if h.Machines[machineName] == nil {
		h.Machines[machineName] = &ControlPlaneMachineHealth{}
	}
	if h.Machines[machineName].MachineConditions == nil {
		h.Machines[machineName].MachineConditions = map[clusterv1.ConditionType]*clusterv1.Condition{}
	}
	// Create a new condition for this Condition Type, no need to modify the existing one.
	msgFormat := ""
	if len(message) > 0 {
		msgFormat = message[0]
	}
	h.Machines[machineName].MachineConditions[conditionType] = conditions.FalseCondition(conditionType, reason, ConditionReason(reason).GetSeverity(), msgFormat)
}

// SetMachineConditionTrue stores/updates the condition in the tracker belongs to the Machine with machineName as True.
func (h *ControlPlaneHealthTracker) SetMachineConditionTrue(machineName string, conditionType clusterv1.ConditionType) {
	if h.Machines[machineName] == nil {
		h.Machines[machineName] = &ControlPlaneMachineHealth{}
	}
	if h.Machines[machineName].MachineConditions == nil {
		h.Machines[machineName].MachineConditions = map[clusterv1.ConditionType]*clusterv1.Condition{}
	}
	h.Machines[machineName].MachineConditions[conditionType] = conditions.TrueCondition(conditionType)
}

// SetEtcdClusterConditionFalse sets/updates EtcdClusterHealthy condition belongs to KCP to False.
func (h *ControlPlaneHealthTracker) SetEtcdClusterConditionFalse(reason string, message ...string) {
	msgFormat := ""
	if len(message) > 0 {
		msgFormat = message[0]
	}
	h.EtcdClusterHealthy = conditions.FalseCondition(controlplanev1.EtcdClusterHealthy, reason, ConditionReason(reason).GetSeverity(), msgFormat)

}

// SetEtcdClusterConditionFalse sets/updates EtcdClusterHealthy condition that belongs to KCP to True.
func (h *ControlPlaneHealthTracker) SetEtcdClusterConditionTrue() {
	h.EtcdClusterHealthy = conditions.TrueCondition(controlplanev1.EtcdClusterHealthy)
}

// SetKCPConditions updates the kcp object's conditions according to health tracker.
func SetKCPConditions(kcp *controlplanev1.KubeadmControlPlane) {
	tracker := GetHealthTracker()
	conditions.Set(kcp, tracker.EtcdClusterHealthy)
}

// SetSingleMachineConditions updates the machine's conditions according to health tracker.
func SetSingleMachineConditions(machine *clusterv1.Machine) {
	tracker := GetHealthTracker()
	machineHealth := tracker.Machines[machine.Name]
	for condType, condition := range machineHealth.MachineConditions {

		doesConditionExist := false
		for _, mCondition := range machine.Status.Conditions {
			// If the condition already exists, change the condition.
			if mCondition.Type == condType {
				conditions.Set(machine, condition)
				doesConditionExist = true
			}
		}
		if !doesConditionExist {
			if machine.Status.Conditions == nil {
				machine.Status.Conditions = clusterv1.Conditions{}
			}
			conditions.Set(machine, condition)
		}

	}
}
