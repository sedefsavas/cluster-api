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
	"strconv"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ApplyMachineHealthCheckAndWaitInput struct {
	ClusterProxy              ClusterProxy
	Cluster                   *clusterv1.Cluster
	ControlPlane              *controlplanev1.KubeadmControlPlane
	MachineDeployments        []*clusterv1.MachineDeployment
	WaitForMachineDeployments []interface{}
}

func ApplyMachineHealthCheckAndWait(ctx context.Context, input ApplyMachineHealthCheckAndWaitInput) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for ApplyMachineHealthCheckAndWait")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling ApplyMachineHealthCheckAndWait")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling ApplyMachineHealthCheckAndWait")
	Expect(input.MachineDeployments).ToNot(BeEmpty(), "Invalid argument. input.MachineDeployments can't be empty when calling ApplyMachineHealthCheckAndWait")

	fmt.Fprintf(GinkgoWriter, "Creating MachineHealthCheck instance\n")
	mhc := GenerateMachineHealthCheck(input.Cluster)
	Expect(input.ClusterProxy.GetClient().Create(ctx, mhc)).ShouldNot(HaveOccurred())

	fmt.Fprintf(GinkgoWriter, "Patching an unhealthy condition to nodes and waiting for remediation\n")
	WaitForMachineHealthCheckToDetectUnhealthyNodeCondition(ctx, WaitForMachineHealthCheckToDetectUnhealthyNodeConditionInput{
		ClusterProxy:           input.ClusterProxy,
		Cluster:                input.Cluster,
		ControlPlane:           input.ControlPlane,
		MachineDeployments:     input.MachineDeployments,
		MachineHealthCheckName: mhc.Name,
	}, input.WaitForMachineDeployments...)
}

func GenerateMachineHealthCheck(cluster *clusterv1.Cluster) *clusterv1.MachineHealthCheck {
	mhc := &clusterv1.MachineHealthCheck{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("mhc-%s%s", cluster.Name, util.RandomString(6)),
			Namespace: cluster.Namespace,
		},
		Spec: clusterv1.MachineHealthCheckSpec{
			ClusterName: cluster.Name,
			UnhealthyConditions: []clusterv1.UnhealthyCondition{
				{
					Type:    corev1.NodeConditionType("E2ENodeHealthy"),
					Status:  corev1.ConditionFalse,
					Timeout: metav1.Duration{Duration: 30 * time.Second},
				},
			},
		},
	}
	return mhc
}

// WaitForMachineHealthCheckToDetectUnhealthyNodeConditionInput is the input for WaitForMachineHealthCheckToDetectUnhealthyNodeCondition.
type WaitForMachineHealthCheckToDetectUnhealthyNodeConditionInput struct {
	ClusterProxy                 ClusterProxy
	Cluster                      *clusterv1.Cluster
	ControlPlane                 *controlplanev1.KubeadmControlPlane
	MachineDeployments           []*clusterv1.MachineDeployment
	MachineHealthCheckName       string
	WaitForControlPlaneIntervals []interface{}
}

// WaitForMachineHealthCheckToDetectUnhealthyNodeCondition waits until MachineHealthCheck detects nodes with unhealthy condition and starts rolling upgrade.
func WaitForMachineHealthCheckToDetectUnhealthyNodeCondition(ctx context.Context, input WaitForMachineHealthCheckToDetectUnhealthyNodeConditionInput, intervals ...interface{}) {
	Expect(ctx).NotTo(BeNil(), "ctx is required for WaitForMachineHealthCheckToDetectUnhealthyNodeCondition")
	Expect(input.ClusterProxy).ToNot(BeNil(), "Invalid argument. input.ClusterProxy can't be nil when calling WaitForMachineHealthCheckToDetectUnhealthyNodeCondition")
	Expect(input.Cluster).ToNot(BeNil(), "Invalid argument. input.Cluster can't be nil when calling WaitForMachineHealthCheckToDetectUnhealthyNodeCondition")
	Expect(input.ControlPlane).ToNot(BeNil(), "Invalid argument. input.ControlPlane can't be nil when calling WaitForMachineHealthCheckToDetectUnhealthyNodeCondition")
	Expect(input.MachineDeployments).ToNot(BeEmpty(), "Invalid argument. input.MachineDeployments can't be empty when calling WaitForMachineHealthCheckToDetectUnhealthyNodeCondition")
	Expect(input.MachineHealthCheckName).ToNot(BeEmpty(), "Invalid argument. input.MachineHealthCheckName can't be empty when calling WaitForMachineHealthCheckToDetectUnhealthyNodeCondition")

	fmt.Fprintf(GinkgoWriter, "Checking if MachineHealthCheck machines are initially healthy\n")
	mhc := &clusterv1.MachineHealthCheck{}
	Eventually(func() bool {
		Expect(input.ClusterProxy.GetClient().Get(ctx, client.ObjectKey{Namespace: input.Cluster.Namespace, Name: input.MachineHealthCheckName}, mhc)).To(Succeed())
		return len(mhc.Spec.UnhealthyConditions) > 0 && mhc.Status.ExpectedMachines > 0 && mhc.Status.CurrentHealthy == mhc.Status.ExpectedMachines
	}, intervals...).Should(BeTrue())

	fmt.Fprintf(GinkgoWriter, "Patching MachineHealthCheck unhealthy condition to worker nodes\n")
	unhealthyNodeCondition := corev1.NodeCondition{
		Type:               mhc.Spec.UnhealthyConditions[0].Type,
		Status:             mhc.Spec.UnhealthyConditions[0].Status,
		LastTransitionTime: metav1.Time{Time: time.Now()},
	}
	for _, md := range input.MachineDeployments {
		selectorMap, err := metav1.LabelSelectorAsMap(&md.Spec.Selector)
		Expect(err).ToNot(HaveOccurred())

		ms := &clusterv1.MachineSetList{}
		Expect(input.ClusterProxy.GetClient().List(ctx, ms, client.InNamespace(input.Cluster.Namespace), client.MatchingLabels(selectorMap))).To(Succeed())
		Expect(len(ms.Items)).NotTo(Equal(0))

		machineSet := ms.Items[0]
		selectorMap, err = metav1.LabelSelectorAsMap(&machineSet.Spec.Selector)
		Expect(err).ToNot(HaveOccurred())
		machines := &clusterv1.MachineList{}
		Expect(input.ClusterProxy.GetClient().List(ctx, machines, client.InNamespace(machineSet.Namespace), client.MatchingLabels(selectorMap))).To(Succeed())

		for _, machine := range machines.Items {
			node := &corev1.Node{}
			Expect(input.ClusterProxy.GetWorkloadCluster(ctx, input.Cluster.Namespace, input.Cluster.Name).GetClient().Get(ctx, types.NamespacedName{Name: machine.Status.NodeRef.Name, Namespace: machine.Status.NodeRef.Namespace}, node)).To(Succeed())
			patchHelper, err := patch.NewHelper(node, input.ClusterProxy.GetWorkloadCluster(ctx, input.Cluster.Namespace, input.Cluster.Name).GetClient())
			Expect(err).ToNot(HaveOccurred())
			node.Status.Conditions = append(node.Status.Conditions, unhealthyNodeCondition)
			Expect(patchHelper.Patch(ctx, node)).To(Succeed())
		}
	}

	fmt.Fprintf(GinkgoWriter, "Patching MachineHealthCheck to initiate reconcile\n")
	Expect(input.ClusterProxy.GetClient().Get(ctx, client.ObjectKey{Namespace: input.Cluster.Namespace, Name: input.MachineHealthCheckName}, mhc)).To(Succeed())
	patchHelper, err := patch.NewHelper(mhc, input.ClusterProxy.GetClient())
	Expect(err).ToNot(HaveOccurred())
	mhc.Labels["e2e.x-k8s.io"] = "triggered"
	Expect(patchHelper.Patch(ctx, mhc)).To(Succeed())

	fmt.Fprintf(GinkgoWriter, "Waiting for MachineHealthCheck to detect unhealthy nodes\n")
	Eventually(func() bool {
		Expect(input.ClusterProxy.GetClient().Get(ctx, client.ObjectKey{Namespace: input.Cluster.Namespace, Name: input.MachineHealthCheckName}, mhc)).To(Succeed())
		return mhc.Status.ExpectedMachines > 0 && mhc.Status.CurrentHealthy != mhc.Status.ExpectedMachines
	}, intervals...).Should(BeTrue())

	fmt.Fprintf(GinkgoWriter, "Waiting for MachineHealthCheck to remediate unhealthy nodes\n")
	counter := 0
	Eventually(func() bool {
		// Patching MachineHealthCheck here to avoid waiting for MachineHealthCheck to detect the changes
		Expect(input.ClusterProxy.GetClient().Get(ctx, client.ObjectKey{Namespace: input.Cluster.Namespace, Name: input.MachineHealthCheckName}, mhc)).To(Succeed())
		patchHelper, err := patch.NewHelper(mhc, input.ClusterProxy.GetClient())
		Expect(err).ToNot(HaveOccurred())
		mhc.Labels["e2e.x-k8s.io"] = "triggered" + strconv.Itoa(counter)
		Expect(patchHelper.Patch(ctx, mhc)).To(Succeed())

		counter++
		Expect(input.ClusterProxy.GetClient().Get(ctx, client.ObjectKey{Namespace: input.Cluster.Namespace, Name: input.MachineHealthCheckName}, mhc)).To(Succeed())
		return mhc.Status.ExpectedMachines > 0 && mhc.Status.CurrentHealthy == mhc.Status.ExpectedMachines
	}, intervals...).Should(BeTrue())
}
