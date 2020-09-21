package internal

import (
	"fmt"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sync"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
)

// Common control-plane components
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
	case c == ConditionReason(controlplanev1.EtcdMemberMismatchWithPodReason):
		return clusterv1.ConditionSeverityWarning
	default:
		return clusterv1.ConditionSeverityInfo
	}
}

var HealthTrackerInstance *ControlPlaneHealthTracker

type ControlPlaneHealthTracker struct {
	// Machines is a map with machine name as the key and ControlPlaneMachineHealth as the value to keep track of machine conditions.
	Machines map[string]*ControlPlaneMachineHealth

	// KCP related conditions

	EtcdClusterHealthy *clusterv1.Condition

	// TODO: CHeck if protecting set methods is needed
	lock sync.Mutex
}

type ControlPlaneMachineHealth struct {
	// MachineConditions is a map with condition type as the key and Condition as the value.
	MachineConditions map[clusterv1.ConditionType]*clusterv1.Condition
}

// This is a singleton object.
func GetHealthTracker() *ControlPlaneHealthTracker {
	if HealthTrackerInstance == nil {
		HealthTrackerInstance = &ControlPlaneHealthTracker{Machines: map[string]*ControlPlaneMachineHealth{}, lock: sync.Mutex{}}
	}
	return HealthTrackerInstance
}

func (h *ControlPlaneHealthTracker) SetMachineConditionFalse(owningMachineName string, conditionType clusterv1.ConditionType, reason string, message ...string) {
	if h.Machines[owningMachineName] == nil {
		h.Machines[owningMachineName] = &ControlPlaneMachineHealth{}
	}
	if h.Machines[owningMachineName].MachineConditions == nil {
		h.Machines[owningMachineName].MachineConditions = map[clusterv1.ConditionType]*clusterv1.Condition{}
	}
	// Create a new condition for this Condition Type, no need to modify the existing one.
	msgFormat := ""
	if len(message) > 1 {
		msgFormat = message[0]
	}
	h.Machines[owningMachineName].MachineConditions[conditionType] = conditions.FalseCondition(conditionType, reason, ConditionReason(reason).GetSeverity(), msgFormat)
}

func (h *ControlPlaneHealthTracker) SetMachineConditionTrue(owningMachineName string, conditionType clusterv1.ConditionType) {
	if h.Machines[owningMachineName] == nil {
		h.Machines[owningMachineName] = &ControlPlaneMachineHealth{}
	}
	if h.Machines[owningMachineName].MachineConditions == nil {
		h.Machines[owningMachineName].MachineConditions = map[clusterv1.ConditionType]*clusterv1.Condition{}
	}
	h.Machines[owningMachineName].MachineConditions[conditionType] = conditions.TrueCondition(conditionType)
}

func (h *ControlPlaneHealthTracker) SetEtcdClusterConditionFalse(reason string, message ...string) {
	msgFormat := ""
	if len(message) > 1 {
		msgFormat = message[0]
	}
	h.EtcdClusterHealthy = conditions.FalseCondition(controlplanev1.EtcdClusterHealthy, reason, ConditionReason(reason).GetSeverity(), msgFormat)

}

func (h *ControlPlaneHealthTracker) SetEtcdClusterConditionTrue() {
	h.EtcdClusterHealthy = conditions.TrueCondition(controlplanev1.EtcdClusterHealthy)
}

func SetControlPlaneMachineConditions(machines FilterableMachineCollection) {
	fmt.Println("xx")
	for _, m := range machines {
		tracker := GetHealthTracker()
		machineHealth := tracker.Machines[m.Name]
		for condType, condition := range machineHealth.MachineConditions {

			doesConditionExist := false
			for _, m := range m.Status.Conditions {
				// If the condition already exists, change the condition.
				if m.Type == condType {
					m = *condition
					doesConditionExist = true
				}
			}
			if !doesConditionExist {
				if m.Status.Conditions == nil {
					m.Status.Conditions = clusterv1.Conditions{}
				}
				m.Status.Conditions = append(m.Status.Conditions, *condition)
			}

		}
	}
}

func SetKCPConditions(kcp *controlplanev1.KubeadmControlPlane) {
	tracker := GetHealthTracker()
	conditions.Set(kcp, tracker.EtcdClusterHealthy)
}

func SetSingleMachineConditions(m *clusterv1.Machine) {
	tracker := GetHealthTracker()
	machineHealth := tracker.Machines[m.Name]
	for condType, condition := range machineHealth.MachineConditions {

		doesConditionExist := false
		for _, mCondition := range m.Status.Conditions {
			// If the condition already exists, change the condition.
			if mCondition.Type == condType {
				conditions.Set(m, condition)
				doesConditionExist = true
			}
		}
		if !doesConditionExist {
			if m.Status.Conditions == nil {
				m.Status.Conditions = clusterv1.Conditions{}
			}
			conditions.Set(m, condition)
		}

	}
}
