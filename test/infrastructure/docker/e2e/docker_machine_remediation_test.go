// +build e2e

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

package e2e

import (
	"fmt"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	"sigs.k8s.io/cluster-api/test/framework"
	infrav1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1alpha3"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Docker Recover", func() {
	var (
		replicas     = 3
		namespace    = "default"
		clusterGen   = newClusterGenerator("recover")
		mgmtClient   ctrlclient.Client
		cluster      *clusterv1.Cluster
		controlPlane *controlplanev1.KubeadmControlPlane
	)
	SetDefaultEventuallyTimeout(10 * time.Minute)
	SetDefaultEventuallyPollingInterval(10 * time.Second)

	BeforeEach(func() {
		// Ensure multi-controlplane workload cluster is up and running
		var (
			infraCluster *infrav1.DockerCluster
			template     *infrav1.DockerMachineTemplate
			err          error
		)
		cluster, infraCluster, controlPlane, template = clusterGen.GenerateCluster(namespace, int32(replicas))

		// Set up the client to the management cluster
		mgmtClient, err = mgmt.GetClient()
		Expect(err).NotTo(HaveOccurred())

		// Set up the cluster object
		createClusterInput := framework.CreateClusterInput{
			Creator:      mgmtClient,
			Cluster:      cluster,
			InfraCluster: infraCluster,
		}
		framework.CreateCluster(ctx, createClusterInput)

		// Set up the KubeadmControlPlane
		createKubeadmControlPlaneInput := framework.CreateKubeadmControlPlaneInput{
			Creator:         mgmtClient,
			ControlPlane:    controlPlane,
			MachineTemplate: template,
		}
		framework.CreateKubeadmControlPlane(ctx, createKubeadmControlPlaneInput)

		// Wait for the cluster to provision.
		assertClusterProvisionsInput := framework.WaitForClusterToProvisionInput{
			Getter:  mgmtClient,
			Cluster: cluster,
		}
		framework.WaitForClusterToProvision(ctx, assertClusterProvisionsInput)

		// Wait for at least one control plane node to be ready
		waitForOneKubeadmControlPlaneMachineToExistInput := framework.WaitForOneKubeadmControlPlaneMachineToExistInput{
			Lister:       mgmtClient,
			Cluster:      cluster,
			ControlPlane: controlPlane,
		}
		framework.WaitForOneKubeadmControlPlaneMachineToExist(ctx, waitForOneKubeadmControlPlaneMachineToExistInput, "5m")

		// Insatll a networking solution on the workload cluster
		workloadClient, err := mgmt.GetWorkloadClient(ctx, cluster.Namespace, cluster.Name)
		Expect(err).ToNot(HaveOccurred())
		applyYAMLURLInput := framework.ApplyYAMLURLInput{
			Client:        workloadClient,
			HTTPGetter:    http.DefaultClient,
			NetworkingURL: "https://docs.projectcalico.org/manifests/calico.yaml",
			Scheme:        mgmt.Scheme,
		}
		framework.ApplyYAMLURL(ctx, applyYAMLURLInput)

		// Wait for the controlplane nodes to exist
		assertKubeadmControlPlaneNodesExistInput := framework.WaitForKubeadmControlPlaneMachinesToExistInput{
			Lister:       mgmtClient,
			Cluster:      cluster,
			ControlPlane: controlPlane,
		}
		framework.WaitForKubeadmControlPlaneMachinesToExist(ctx, assertKubeadmControlPlaneNodesExistInput, "10m", "10s")

		// Wait for the control plane to be ready
		waitForControlPlaneToBeReadyInput := framework.WaitForControlPlaneToBeReadyInput{
			Getter:       mgmtClient,
			ControlPlane: controlPlane,
		}
		framework.WaitForControlPlaneToBeReady(ctx, waitForControlPlaneToBeReadyInput)
	})

	AfterEach(func() {
		// Delete the workload cluster
		deleteClusterInput := framework.DeleteClusterInput{
			Deleter: mgmtClient,
			Cluster: cluster,
		}
		framework.DeleteCluster(ctx, deleteClusterInput)

		waitForClusterDeletedInput := framework.WaitForClusterDeletedInput{
			Getter:  mgmtClient,
			Cluster: cluster,
		}
		framework.WaitForClusterDeleted(ctx, waitForClusterDeletedInput)

		assertAllClusterAPIResourcesAreGoneInput := framework.AssertAllClusterAPIResourcesAreGoneInput{
			Lister:  mgmtClient,
			Cluster: cluster,
		}
		framework.AssertAllClusterAPIResourcesAreGone(ctx, assertAllClusterAPIResourcesAreGoneInput)

		ensureDockerDeletedInput := ensureDockerArtifactsDeletedInput{
			Lister:  mgmtClient,
			Cluster: cluster,
		}
		ensureDockerArtifactsDeleted(ensureDockerDeletedInput)

		// Dump cluster API and docker related resources to artifacts before deleting them.
		Expect(framework.DumpResources(mgmt, resourcesPath, GinkgoWriter)).To(Succeed())
		resources := map[string]runtime.Object{
			"DockerCluster":         &infrav1.DockerClusterList{},
			"DockerMachine":         &infrav1.DockerMachineList{},
			"DockerMachineTemplate": &infrav1.DockerMachineTemplateList{},
		}
		Expect(framework.DumpProviderResources(mgmt, resources, resourcesPath, GinkgoWriter)).To(Succeed())
	})
	It("should recover from manual workload machine deletion", func() {
		By("cleaning up etcd members and kubeadm configMap")
		inClustersNamespaceListOption := ctrlclient.InNamespace(cluster.Namespace)
		// ControlPlane labels
		matchClusterListOption := ctrlclient.MatchingLabels{
			clusterv1.MachineControlPlaneLabelName: "",
			clusterv1.ClusterLabelName:             cluster.Name,
		}

		machineList := &clusterv1.MachineList{}
		err := mgmtClient.List(ctx, machineList, inClustersNamespaceListOption, matchClusterListOption)
		Expect(err).ToNot(HaveOccurred())
		Expect(machineList.Items).To(HaveLen(int(*controlPlane.Spec.Replicas)))

		Expect(mgmtClient.Delete(ctx, &machineList.Items[0])).To(Succeed())

		Eventually(func() (int, error) {
			machineList := &clusterv1.MachineList{}
			if err := mgmtClient.List(ctx, machineList, inClustersNamespaceListOption, matchClusterListOption); err != nil {
				fmt.Println(err)
				return 0, err
			}
			return len(machineList.Items), nil
		}, "10m", "5s").Should(Equal(int(*controlPlane.Spec.Replicas) - 1))

		By("ensuring a replacement machine is created")
		Eventually(func() (int, error) {
			machineList := &clusterv1.MachineList{}
			if err := mgmtClient.List(ctx, machineList, inClustersNamespaceListOption, matchClusterListOption); err != nil {
				fmt.Println(err)
				return 0, err
			}
			return len(machineList.Items), nil
		}, "10m", "30s").Should(Equal(int(*controlPlane.Spec.Replicas)))

	})

})
