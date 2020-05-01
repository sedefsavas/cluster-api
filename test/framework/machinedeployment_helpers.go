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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateMachineDeploymentInput is the input for CreateMachineDeployment.
type CreateMachineDeploymentInput struct {
	Creator                 Creator
	MachineDeployment       *clusterv1.MachineDeployment
	BootstrapConfigTemplate runtime.Object
	InfraMachineTemplate    runtime.Object
}

// CreateMachineDeployment creates the machine deployment and dependencies.
func CreateMachineDeployment(ctx context.Context, input CreateMachineDeploymentInput) {
	By("creating a core MachineDeployment resource")
	Expect(input.Creator.Create(ctx, input.MachineDeployment)).To(Succeed())

	By("creating a BootstrapConfigTemplate resource")
	Expect(input.Creator.Create(ctx, input.BootstrapConfigTemplate)).To(Succeed())

	By("creating an InfrastructureMachineTemplate resource")
	Expect(input.Creator.Create(ctx, input.InfraMachineTemplate)).To(Succeed())
}

// GetMachineDeploymentsByClusterInput is the input for GetMachineDeploymentsByCluster.
type GetMachineDeploymentsByClusterInput struct {
	Lister      Lister
	ClusterName string
	Namespace   string
}

// GetMachineDeploymentsByCluster returns the MachineDeployments objects for a cluster.
// Important! this method relies on labels that are created by the CAPI controllers during the first reconciliation, so
// it is necessary to ensure this is already happened before calling it.
func GetMachineDeploymentsByCluster(ctx context.Context, input GetMachineDeploymentsByClusterInput) []*clusterv1.MachineDeployment {
	deploymentList := &clusterv1.MachineDeploymentList{}
	Expect(input.Lister.List(ctx, deploymentList, byClusterOptions(input.ClusterName, input.Namespace)...)).To(Succeed(), "Failed to list MachineDeployments object for Cluster %s/%s", input.Namespace, input.ClusterName)

	deployments := make([]*clusterv1.MachineDeployment, len(deploymentList.Items))
	for i := range deploymentList.Items {
		deployments[i] = &deploymentList.Items[i]
	}
	return deployments
}

// WaitForMachineDeploymentNodesToExistInput is the input for WaitForMachineDeploymentNodesToExist.
type WaitForMachineDeploymentNodesToExistInput struct {
	Lister            Lister
	Cluster           *clusterv1.Cluster
	MachineDeployment *clusterv1.MachineDeployment
}

// WaitForMachineDeploymentNodesToExist waits until all nodes associated with a machine deployment exist.
func WaitForMachineDeploymentNodesToExist(ctx context.Context, input WaitForMachineDeploymentNodesToExistInput, intervals ...interface{}) {
	By("waiting for the workload nodes to exist")
	Eventually(func() (int, error) {
		selectorMap, err := metav1.LabelSelectorAsMap(&input.MachineDeployment.Spec.Selector)
		if err != nil {
			return 0, err
		}
		ms := &clusterv1.MachineSetList{}
		if err := input.Lister.List(ctx, ms, client.InNamespace(input.Cluster.Namespace), client.MatchingLabels(selectorMap)); err != nil {
			return 0, err
		}
		if len(ms.Items) == 0 {
			return 0, errors.New("no machinesets were found")
		}
		machineSet := ms.Items[0]
		selectorMap, err = metav1.LabelSelectorAsMap(&machineSet.Spec.Selector)
		if err != nil {
			return 0, err
		}
		machines := &clusterv1.MachineList{}
		if err := input.Lister.List(ctx, machines, client.InNamespace(machineSet.Namespace), client.MatchingLabels(selectorMap)); err != nil {
			return 0, err
		}
		count := 0
		for _, machine := range machines.Items {
			if machine.Status.NodeRef != nil {
				count++
			}
		}
		return count, nil
	}, intervals...).Should(Equal(int(*input.MachineDeployment.Spec.Replicas)))
}

// DiscoveryAndWaitForMachineDeploymentsInput is the input type for DiscoveryAndWaitForMachineDeployments.
type DiscoveryAndWaitForMachineDeploymentsInput struct {
	Lister  Lister
	Cluster *clusterv1.Cluster
}

// DiscoveryAndWaitForMachineDeployments discovers the MachineDeployments existing in a cluster and waits for them to be ready (all the machine provisioned).
func DiscoveryAndWaitForMachineDeployments(ctx context.Context, input DiscoveryAndWaitForMachineDeploymentsInput, intervals ...interface{}) []*clusterv1.MachineDeployment {
	Expect(ctx).NotTo(BeNil(), "ctx is required for DiscoveryAndWaitForMachineDeployments")
	Expect(input.Lister).ToNot(BeNil(), "Invalid argument. input.Lister can't be nil when calling DiscoveryAndWaitForMachineDeployments")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling DiscoveryAndWaitForMachineDeployments")

	machineDeployments := GetMachineDeploymentsByCluster(ctx, GetMachineDeploymentsByClusterInput{
		Lister:      input.Lister,
		ClusterName: input.Cluster.Name,
		Namespace:   input.Cluster.Namespace,
	})
	for _, deployment := range machineDeployments {
		WaitForMachineDeploymentNodesToExist(ctx, WaitForMachineDeploymentNodesToExistInput{
			Lister:            input.Lister,
			Cluster:           input.Cluster,
			MachineDeployment: deployment,
		}, intervals...)
	}
	return machineDeployments
}

// UpgradeMachineDeploymentAndWaitInput is the input type for UpgradeMachineDeploymentAndWait.
type UpgradeMachineDeploymentAndWaitInput struct {
	ClusterProxy                ClusterProxy
	Cluster                     *clusterv1.Cluster
	UpgradeVersion              string
	WaitForMachinesToBeUpgraded []interface{}
	MachineDeployments          []*clusterv1.MachineDeployment
}

// UpgradeMachineDeploymentAndWait upgrades a machine deployment and waits for its machines to be upgraded.
func UpgradeMachineDeploymentAndWait(ctx context.Context, input UpgradeMachineDeploymentAndWaitInput) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for UpgradeMachineDeploymentAndWait")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling UpgradeMachineDeploymentAndWait")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling UpgradeMachineDeploymentAndWait")
	Expect(input.UpgradeVersion).ToNot(BeNil(), "Invalid argument. input.KubernetesUpgradeVersion can't be nil when calling UpgradeMachineDeploymentAndWait")
	Expect(input.MachineDeployments).ToNot(BeEmpty(), "Invalid argument. input.MachineDeployments can't be empty when calling UpgradeMachineDeploymentAndWait")

	mgmtClient := input.ClusterProxy.GetClient()

	for _, deployment := range input.MachineDeployments {
		fmt.Fprintf(GinkgoWriter, "Patching the new kubernetes version to Machine Deployment\n")
		patchHelper, err := patch.NewHelper(deployment, mgmtClient)
		Expect(err).ToNot(HaveOccurred())

		deployment.Spec.Template.Spec.Version = &input.UpgradeVersion
		Expect(patchHelper.Patch(context.TODO(), deployment)).To(Succeed())

		fmt.Fprintf(GinkgoWriter, "Waiting for machines to have the upgraded kubernetes version\n")
		WaitForMachineDeploymentMachinesToBeUpgraded(ctx, WaitForMachineDeploymentMachinesToBeUpgradedInput{
			Lister:                   mgmtClient,
			Cluster:                  input.Cluster,
			MachineCount:             int(*deployment.Spec.Replicas),
			KubernetesUpgradeVersion: input.UpgradeVersion,
			Deployment:               *deployment,
		}, input.WaitForMachinesToBeUpgraded...)
	}
}

// WaitForMachineDeploymentRollingUpgradeToStartInput is the input for WaitForMachineDeploymentRollingUpgradeToStart.
type WaitForMachineDeploymentRollingUpgradeToStartInput struct {
	Getter            Getter
	MachineDeployment *clusterv1.MachineDeployment
}

// WaitForMachineDeploymentRollingUpgradeToStart waits until rolling upgrade starts.
func WaitForMachineDeploymentRollingUpgradeToStart(ctx context.Context, input WaitForMachineDeploymentRollingUpgradeToStartInput, intervals ...interface{}) {
	fmt.Fprintf(GinkgoWriter, "Waiting for MachineDeployment rolling upgrade to start\n")
	Eventually(func() bool {
		md := &clusterv1.MachineDeployment{}
		Expect(input.Getter.Get(ctx, client.ObjectKey{Namespace: input.MachineDeployment.Namespace, Name: input.MachineDeployment.Name}, md)).To(Succeed())
		return md.Status.Replicas != md.Status.AvailableReplicas
	}, intervals...).Should(BeTrue())
}

// WaitForMachineDeploymentRollingUpgradeToCompleteInput is the input for WaitForMachineDeploymentRollingUpgradeToComplete.
type WaitForMachineDeploymentRollingUpgradeToCompleteInput struct {
	Getter            Getter
	MachineDeployment *clusterv1.MachineDeployment
}

// WaitForMachineDeploymentNodesToExist waits until rolling upgrade is complete.
func WaitForMachineDeploymentRollingUpgradeToComplete(ctx context.Context, input WaitForMachineDeploymentRollingUpgradeToCompleteInput, intervals ...interface{}) {
	fmt.Fprintf(GinkgoWriter, "Waiting for MachineDeployment rolling upgrade to complete\n")
	Eventually(func() bool {
		md := &clusterv1.MachineDeployment{}
		Expect(input.Getter.Get(ctx, client.ObjectKey{Namespace: input.MachineDeployment.Namespace, Name: input.MachineDeployment.Name}, md)).To(Succeed())
		return md.Status.Replicas == md.Status.AvailableReplicas
	}, intervals...).Should(BeTrue())
}

// UpgradeMachineDeploymentInfrastructureRefAndWaitInput is the input type for UpgradeMachineDeploymentInfrastructureRefAndWait.
type UpgradeMachineDeploymentInfrastructureRefAndWaitInput struct {
	ClusterProxy                ClusterProxy
	Cluster                     *clusterv1.Cluster
	WaitForMachinesToBeUpgraded []interface{}
	MachineDeployments          []*clusterv1.MachineDeployment
}

// UpgradeMachineDeploymentInfrastructureRefAndWait upgrades a machine deployment infrastructure ref and waits for its machines to be upgraded.
func UpgradeMachineDeploymentInfrastructureRefAndWait(ctx context.Context, input UpgradeMachineDeploymentInfrastructureRefAndWaitInput) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for UpgradeMachineDeploymentAndWait")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling UpgradeMachineDeploymentAndWait")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling UpgradeMachineDeploymentAndWait")
	Expect(input.MachineDeployments).ToNot(BeEmpty(), "Invalid argument. input.MachineDeployments can't be empty when calling UpgradeMachineDeploymentAndWait")

	mgmtClient := input.ClusterProxy.GetClient()

	for _, deployment := range input.MachineDeployments {
		fmt.Fprintf(GinkgoWriter, "Patching the new infrastructure ref to Machine Deployment\n")
		// Retrieve infra object
		infraRef := deployment.Spec.Template.Spec.InfrastructureRef
		infraObj := &unstructured.Unstructured{}
		infraObj.SetGroupVersionKind(infraRef.GroupVersionKind())
		key := client.ObjectKey{
			Namespace: input.Cluster.Namespace,
			Name:      infraRef.Name,
		}
		Expect(mgmtClient.Get(ctx, key, infraObj)).NotTo(HaveOccurred())

		// Creates a new infra object
		newInfraObj := infraObj
		newInfraObjName := fmt.Sprintf("%s-%s", infraRef.Name, util.RandomString(6))
		newInfraObj.SetName(newInfraObjName)
		newInfraObj.SetResourceVersion("")
		Expect(mgmtClient.Create(ctx, newInfraObj)).NotTo(HaveOccurred())

		// Patch the new infra object's ref to the machine deployment
		patchHelper, err := patch.NewHelper(deployment, mgmtClient)
		Expect(err).ToNot(HaveOccurred())
		infraRef.Name = newInfraObjName
		deployment.Spec.Template.Spec.InfrastructureRef = infraRef
		Expect(patchHelper.Patch(context.TODO(), deployment)).To(Succeed())

		fmt.Fprintf(GinkgoWriter, "Waiting for rolling upgrade to start.\n")
		WaitForMachineDeploymentRollingUpgradeToStart(ctx, WaitForMachineDeploymentRollingUpgradeToStartInput{
			Getter:            mgmtClient,
			MachineDeployment: deployment,
		}, input.WaitForMachinesToBeUpgraded...)

		fmt.Fprintf(GinkgoWriter, "Waiting for rolling upgrade to complete.\n")
		WaitForMachineDeploymentRollingUpgradeToComplete(ctx, WaitForMachineDeploymentRollingUpgradeToCompleteInput{
			Getter:            mgmtClient,
			MachineDeployment: deployment,
		}, input.WaitForMachinesToBeUpgraded...)
	}
}

// machineDeploymentOptions returns a set of ListOptions that allows to get all machine objects belonging to a machine deployment.
func machineDeploymentOptions(deployment clusterv1.MachineDeployment) []client.ListOption {
	return []client.ListOption{
		client.MatchingLabels(deployment.Spec.Selector.MatchLabels),
	}
}
