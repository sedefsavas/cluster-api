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

package controllers

import (
	"context"
	"time"

	"github.com/pkg/errors"
	apicorev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/controllers/noderefutil"
	capierrors "sigs.k8s.io/cluster-api/errors"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	ErrNodeNotFound = errors.New("cannot find node with matching ProviderID")
)

func (r *MachineReconciler) reconcileNode(ctx context.Context, cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	logger := r.Log.WithValues("machine", machine.Name, "namespace", machine.Namespace)
	// Check that the Machine hasn't been deleted or in the process.
	if !machine.DeletionTimestamp.IsZero() {
		return nil
	}

	logger = logger.WithValues("cluster", cluster.Name)

	// Check that the Machine has a valid ProviderID.
	if machine.Spec.ProviderID == nil || *machine.Spec.ProviderID == "" {
		logger.Info("Machine doesn't have a valid ProviderID yet")
		return nil
	}

	providerID, err := noderefutil.NewProviderID(*machine.Spec.ProviderID)
	if err != nil {
		return err
	}

	remoteClient, err := r.Tracker.GetClient(ctx, util.ObjectKey(cluster))
	if err != nil {
		return err
	}

	// Even if Status.NodeRef exists, continue to do the following checks to make sure Node is healthy
	node, err := r.getNode(remoteClient, providerID)
	if err != nil {
		if err == ErrNodeNotFound {
			// While a NodeRef is set in the status, getting ErrNodeNotFound error means the node is deleted.
			// If Status.NodeRef is not set before, node still can be in the provisioning state.
			if machine.Status.NodeRef != nil {
				conditions.MarkFalse(machine, clusterv1.MachineNodeHealthyCondition, clusterv1.NodeNotFoundReason, clusterv1.ConditionSeverityError, "")
			}
			return errors.Wrapf(&capierrors.RequeueAfterError{RequeueAfter: 20 * time.Second},
				"cannot assign NodeRef to Machine %q in namespace %q, no matching Node", machine.Name, machine.Namespace)
		}
		logger.Error(err, "Failed to assign NodeRef")
		r.recorder.Event(machine, apicorev1.EventTypeWarning, "FailedSetNodeRef", err.Error())
		return err
	}

	// Set the Machine NodeRef.
	if machine.Status.NodeRef == nil {
		machine.Status.NodeRef = &apicorev1.ObjectReference{
			Kind:       node.Kind,
			APIVersion: node.APIVersion,
			Name:       node.Name,
			UID:        node.UID,
		}
		logger.Info("Set Machine's NodeRef", "noderef", machine.Status.NodeRef.Name)
		r.recorder.Event(machine, apicorev1.EventTypeNormal, "SuccessfulSetNodeRef", machine.Status.NodeRef.Name)
		conditions.MarkFalse(machine, clusterv1.MachineNodeHealthyCondition, clusterv1.NodeProvisioningReason, clusterv1.ConditionSeverityWarning, "")
		return nil
	}

	// Do the remaining node health checks, then set the node health to true if all checks pass.
	if !checkNodeConditions(node) {
		conditions.MarkFalse(machine, clusterv1.MachineNodeHealthyCondition, clusterv1.NodeConditionsFailedReason, clusterv1.ConditionSeverityWarning, "")
		return nil
	}
	conditions.MarkTrue(machine, clusterv1.MachineNodeHealthyCondition)

	return nil
}

// checkNodeConditions checks if a Node is healthy and does not have any semantically negative Conditions.
// Returns true if NodeMemoryPressure, NodeDiskPressure, or NodePIDPressure Conditions are false or Ready Condition is true.
func checkNodeConditions(node *apicorev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == apicorev1.NodeMemoryPressure && condition.Status == apicorev1.ConditionTrue {
			return false
		}
		if condition.Type == apicorev1.NodeDiskPressure && condition.Status == apicorev1.ConditionTrue {
			return false
		}
		if condition.Type == apicorev1.NodePIDPressure && condition.Status == apicorev1.ConditionTrue {
			return false
		}
		if condition.Type == apicorev1.NodeReady && condition.Status == apicorev1.ConditionFalse {
			return false
		}
	}
	return true
}

func (r *MachineReconciler) getNode(c client.Reader, providerID *noderefutil.ProviderID) (*apicorev1.Node, error) {
	logger := r.Log.WithValues("providerID", providerID)

	nodeList := apicorev1.NodeList{}
	for {
		if err := c.List(context.TODO(), &nodeList, client.Continue(nodeList.Continue)); err != nil {
			return nil, err
		}

		for _, node := range nodeList.Items {
			nodeProviderID, err := noderefutil.NewProviderID(node.Spec.ProviderID)
			if err != nil {
				logger.Error(err, "Failed to parse ProviderID", "node", node.Name)
				continue
			}

			if providerID.Equals(nodeProviderID) {
				return &node, nil
			}
		}

		if nodeList.Continue == "" {
			break
		}
	}
	return nil, ErrNodeNotFound
}
