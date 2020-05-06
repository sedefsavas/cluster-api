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

package framework

import (
	"context"
	"fmt"
	"time"

	"sigs.k8s.io/cluster-api/util/patch"

	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DiscoverMachineHealthCheckAndWaitInput is the input for DiscoverMachineHealthCheckAndWait.
type DiscoverMachineHealthCheckAndWaitInput struct {
	ClusterProxy              ClusterProxy
	Cluster                   *clusterv1.Cluster
	MachineDeployments        []*clusterv1.MachineDeployment
	WaitForMachineRemediation []interface{}
}

// DiscoverMachineHealthCheckAndWait patches an unhealthy node condition to one node in each MachineDeployment and then wait for remediation.
func DiscoverMachineHealthChecksAndWait(ctx context.Context, input DiscoverMachineHealthCheckAndWaitInput) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for DiscoverMachineHealthChecksAndWait")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling DiscoverMachineHealthChecksAndWait")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling DiscoverMachineHealthChecksAndWait")
	Expect(input.MachineDeployments).ToNot(BeEmpty(), "Invalid argument. input.MachineDeployments can't be empty when calling DiscoverMachineHealthChecksAndWait")

	machineHealthChecks := GetMachineHealthChecksByCluster(ctx, GetMachineHealthChecksByClusterInput{
		Lister:      input.ClusterProxy.GetClient(),
		ClusterName: input.Cluster.Name,
		Namespace:   input.Cluster.Namespace,
	})

	fmt.Fprintf(GinkgoWriter, "Patching an unhealthy condition to nodes and waiting for remediation\n")
	for _, mhc := range machineHealthChecks {
		if len(mhc.Spec.UnhealthyConditions) < 1 {
			continue
		}
		fmt.Fprintf(GinkgoWriter, "Patching MachineDeployments with MachinehealthCheckLabel\n")
		for _, md := range input.MachineDeployments {
			if len(mhc.Spec.Selector.MatchLabels) < 0 {
				continue
			}
			selectorMap, err := metav1.LabelSelectorAsMap(&md.Spec.Selector)
			Expect(err).ToNot(HaveOccurred())

			ms := &clusterv1.MachineSetList{}
			Expect(input.ClusterProxy.GetClient().List(ctx, ms, client.InNamespace(input.Cluster.Namespace), client.MatchingLabels(selectorMap))).To(Succeed())

			if len(ms.Items) == 0 {
				continue
			}
			machineSet := ms.Items[0]
			selectorMap, err = metav1.LabelSelectorAsMap(&machineSet.Spec.Selector)
			Expect(err).ToNot(HaveOccurred())

			machines := &clusterv1.MachineList{}
			Expect(input.ClusterProxy.GetClient().List(ctx, machines, client.InNamespace(machineSet.Namespace), client.MatchingLabels(selectorMap))).To(Succeed())

			for _, machine := range machines.Items {
				patchHelper, err := patch.NewHelper(&machine, input.ClusterProxy.GetClient())
				Expect(err).ToNot(HaveOccurred())
				machine.SetLabels(mhc.Spec.Selector.MatchLabels)
				Expect(patchHelper.Patch(ctx, &machine)).To(Succeed())
			}
		}

		machines := GetMachinesByMachineHealthCheck(context.TODO(), GetMachinesByMachineHealthCheckInput{
			Lister:             input.ClusterProxy.GetClient(),
			ClusterName:        input.Cluster.Name,
			Namespace:          input.Cluster.Namespace,
			MachineHealthCheck: *mhc,
		})
		if len(machines) == 0 {
			continue
		}

		fmt.Fprintf(GinkgoWriter, "Patching MachineHealthCheck unhealthy condition to one of the nodes\n")
		unhealthyNodeCondition := corev1.NodeCondition{
			Type:               mhc.Spec.UnhealthyConditions[0].Type,
			Status:             mhc.Spec.UnhealthyConditions[0].Status,
			LastTransitionTime: metav1.Time{Time: time.Now()},
		}
		PatchNodeCondition(ctx, PatchNodeConditionInput{
			ClusterProxy:  input.ClusterProxy,
			Cluster:       input.Cluster,
			NodeCondition: unhealthyNodeCondition,
			Machine:       machines[0],
		})
	}

	fmt.Fprintf(GinkgoWriter, "Waiting for remediation\n")
	WaitForMachineHealthCheckToRemediateUnhealthyNodeCondition(ctx, WaitForMachineHealthCheckToRemediateUnhealthyNodeConditionInput{
		ClusterProxy:        input.ClusterProxy,
		Cluster:             input.Cluster,
		MachineHealthChecks: machineHealthChecks,
	}, input.WaitForMachineRemediation...)
}

// GetMachineHealthChecksByClusterInput is the input for GetMachineHealthChecksByCluster.
type GetMachineHealthChecksByClusterInput struct {
	Lister      Lister
	ClusterName string
	Namespace   string
}

// GetMachineHealthChecksByCluster returns the MachineHealthCheck objects for a cluster.
// Important! this method relies on labels that are created by the CAPI controllers during the first reconciliation, so
// it is necessary to ensure this is already happened before calling it.
func GetMachineHealthChecksByCluster(ctx context.Context, input GetMachineHealthChecksByClusterInput) []*clusterv1.MachineHealthCheck {
	machineHealthCheckList := &clusterv1.MachineHealthCheckList{}
	Expect(input.Lister.List(ctx, machineHealthCheckList, byClusterOptions(input.ClusterName, input.Namespace)...)).To(Succeed(), "Failed to list MachineDeployments object for Cluster %s/%s", input.Namespace, input.ClusterName)

	machineHealthChecks := make([]*clusterv1.MachineHealthCheck, len(machineHealthCheckList.Items))
	for i := range machineHealthCheckList.Items {
		machineHealthChecks[i] = &machineHealthCheckList.Items[i]
	}
	return machineHealthChecks
}

// machineHealthCheckOptions returns a set of ListOptions that allows to get all machine objects belonging to a MachineHealthCheck.
func machineHealthCheckOptions(machineHealthCheck clusterv1.MachineHealthCheck) []client.ListOption {
	return []client.ListOption{
		client.MatchingLabels(machineHealthCheck.Spec.Selector.MatchLabels),
	}
}

// WaitForMachineHealthCheckToRemediateUnhealthyNodeConditionInput is the input for WaitForMachineHealthCheckToRemediateUnhealthyNodeCondition.
type WaitForMachineHealthCheckToRemediateUnhealthyNodeConditionInput struct {
	ClusterProxy        ClusterProxy
	Cluster             *clusterv1.Cluster
	MachineHealthChecks []*clusterv1.MachineHealthCheck
}

// WaitForMachineHealthCheckToRemediateUnhealthyNodeCondition patches a node condition to any one of the machines with a node ref.
func WaitForMachineHealthCheckToRemediateUnhealthyNodeCondition(ctx context.Context, input WaitForMachineHealthCheckToRemediateUnhealthyNodeConditionInput, intervals ...interface{}) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for PatchNodeConditions")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling PatchNodeConditions")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling PatchNodeConditions")

	for _, mhc := range input.MachineHealthChecks {
		fmt.Fprintf(GinkgoWriter, "Waiting until the node with unhealthy node condition is remediated\n")
		Eventually(func() bool {
			machines := GetMachinesByMachineHealthCheck(context.TODO(), GetMachinesByMachineHealthCheckInput{
				Lister:             input.ClusterProxy.GetClient(),
				ClusterName:        input.Cluster.Name,
				Namespace:          input.Cluster.Namespace,
				MachineHealthCheck: *mhc,
			})
			if len(machines) == 0 {
				return true
			}

			for _, machine := range machines {
				if machine.Status.NodeRef == nil {
					return false
				}
				node := &corev1.Node{}
				Expect(input.ClusterProxy.GetWorkloadCluster(ctx, input.Cluster.Namespace, input.Cluster.Name).GetClient().Get(ctx, types.NamespacedName{Name: machine.Status.NodeRef.Name, Namespace: machine.Status.NodeRef.Namespace}, node)).To(Succeed())
				if hasMatchingLabels(mhc.Spec.Selector, node.Labels) {
					return false
				}
			}
			return true
		}, intervals...).Should(BeTrue())
	}
}
