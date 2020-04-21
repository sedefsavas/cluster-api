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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/pkg/errors"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
)

type GetMachinesByClusterInput struct {
	Lister      Lister
	ClusterName string
	Namespace   string
}

type GetControlPlaneMachinesByClusterInput struct {
	GetMachinesByClusterInput
}

// GetControlPlaneMachinesByCluster returns the control plane Machine objects for a cluster.
// Important! this method relies on labels that are created by the CAPI controllers during the first reconciliation, so
// it is necessary to ensure this is already happened before calling it.
func GetControlPlaneMachinesByCluster(ctx context.Context, input GetControlPlaneMachinesByClusterInput) []clusterv1.Machine {
	options := append(byClusterOptions(input.ClusterName, input.Namespace), controlPlaneMachineOptions()...)

	machineList := &clusterv1.MachineList{}
	Expect(input.Lister.List(ctx, machineList, options...)).To(Succeed(), "Failed to list MachineList object for Cluster %s/%s", input.Namespace, input.ClusterName)

	return machineList.Items
}

type GetMachinesByMachineDeploymentsInput struct {
	GetMachinesByClusterInput
	Deployment clusterv1.MachineDeployment
}

// GetMachinesByMachineDeployments returns Machine objects for a cluster belonging to a machine deployment.
// Important! this method relies on labels that are created by the CAPI controllers during the first reconciliation, so
// it is necessary to ensure this is already happened before calling it.
func GetMachinesByMachineDeployments(ctx context.Context, input GetMachinesByMachineDeploymentsInput) []clusterv1.Machine {
	opts := byClusterOptions(input.ClusterName, input.Namespace)
	opts = append(opts, machineDeploymentOptions(input.Deployment)...)

	machineList := &clusterv1.MachineList{}
	Expect(input.Lister.List(ctx, machineList, opts...)).To(Succeed(), "Failed to list MachineList object for Cluster %s/%s", input.Namespace, input.ClusterName)

	return machineList.Items
}

// WaitForControlPlaneMachinesToBeUpgradedInput is the input for WaitForControlPlaneMachinesToBeUpgraded.
type WaitForControlPlaneMachinesToBeUpgradedInput struct {
	Lister         Lister
	Cluster        *clusterv1.Cluster
	UpgradeVersion string
	MachineCount   int
}

// WaitForControlPlaneMachinesToBeUpgraded waits until all control plane machines are upgraded to the correct kubernetes version.
func WaitForControlPlaneMachinesToBeUpgraded(ctx context.Context, input WaitForControlPlaneMachinesToBeUpgradedInput, intervals ...interface{}) {
	By("ensuring all machines have upgraded kubernetes version")
	Eventually(func() (int, error) {
		machines := GetControlPlaneMachinesByCluster(context.TODO(), GetControlPlaneMachinesByClusterInput{
			GetMachinesByClusterInput: GetMachinesByClusterInput{
				Lister:      input.Lister,
				ClusterName: input.Cluster.Name,
				Namespace:   input.Cluster.Namespace,
			},
		})

		upgraded := 0
		for _, machine := range machines {
			if *machine.Spec.Version == input.UpgradeVersion {
				upgraded++
			}
		}
		if len(machines) > upgraded {
			return 0, errors.New("old nodes remain")
		}
		return upgraded, nil
	}, intervals...).Should(Equal(input.MachineCount))
}

// WaitForMachineDeploymentMachinesToBeUpgradedInput is the input for WaitForMachineDeploymentMachinesToBeUpgraded.
type WaitForMachineDeploymentMachinesToBeUpgradedInput struct {
	Lister         Lister
	Cluster        *clusterv1.Cluster
	UpgradeVersion string
	MachineCount   int
	Deployment     clusterv1.MachineDeployment
}

// WaitForMachineDeploymentMachinesToBeUpgraded waits until all machines belonging to a MachineDeployment are upgraded to the correct kubernetes version.
func WaitForMachineDeploymentMachinesToBeUpgraded(ctx context.Context, input WaitForMachineDeploymentMachinesToBeUpgradedInput, intervals ...interface{}) {
	By("ensuring all machines have upgraded kubernetes version")
	Eventually(func() (int, error) {
		machines := GetMachinesByMachineDeployments(context.TODO(), GetMachinesByMachineDeploymentsInput{
			GetMachinesByClusterInput: GetMachinesByClusterInput{
				Lister:      input.Lister,
				ClusterName: input.Cluster.Name,
				Namespace:   input.Cluster.Namespace,
			},
			Deployment: input.Deployment,
		})

		upgraded := 0
		for _, machine := range machines {
			if *machine.Spec.Version == input.UpgradeVersion {
				upgraded++
			}
		}
		if len(machines) > upgraded {
			return 0, errors.New("old nodes remain")
		}
		return upgraded, nil
	}, intervals...).Should(Equal(input.MachineCount))
}
