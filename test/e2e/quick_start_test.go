// +build e2e

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

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/cluster-api/test/framework/discovery"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/util"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("When following the Cluster API quick-start", func() {
	var (
		suiteArtifactFolder string
		logPath             string
		namespaceName       string
		clusterName         string
		client              ctrlclient.Client

		cluster *clusterv1.Cluster
	)

	BeforeEach(func() {

		By("Getting a client for the management cluster")
		var err error
		client, err = managementCluster.GetClient()
		Expect(err).NotTo(HaveOccurred(), "Failed to get a client for the management cluster")

		//TODO: refactor this and stream events out of the namespace
		namespaceName = fmt.Sprintf("quick-start-%s", util.RandomString(6))
		framework.CreateNamespace(context.TODO(), framework.CreateNamespaceInput{Creator: client, Name: namespaceName}, "40s", "10s")

		clusterName = fmt.Sprintf("cluster-%s", util.RandomString(6))

		suiteArtifactFolder = filepath.Join(artifactFolder, "quick-start")
		logPath = filepath.Join(suiteArtifactFolder, "logs")
	})

	It("Should create a workload cluster", func() {

		//TODO: refactor this into a framework function

		//TODO: evaluate if to continue to enforce variables in the config file or if to let pass variables via other means as well (e.g. env var)
		Expect(e2eConfig.Variables).To(HaveKey("KUBERNETES_VERSION"), "Invalid configuration. Please add KUBERNETES_VERSION to the e2e.config file")
		Expect(e2eConfig.Variables).To(HaveKey("CONTROL_PLANE_MACHINE_COUNT"), "Invalid configuration. Please add CONTROL_PLANE_MACHINE_COUNT to the e2e.config file")
		controlPlaneMachineCount, err := strconv.ParseInt(e2eConfig.Variables["CONTROL_PLANE_MACHINE_COUNT"], 10, 0)
		Expect(err).ToNot(HaveOccurred(), "Invalid configuration. The CONTROL_PLANE_MACHINE_COUNT variable should be a valid integer number")
		Expect(e2eConfig.Variables).To(HaveKey("WORKER_MACHINE_COUNT"), "Invalid configuration. Please add WORKER_MACHINE_COUNT to the e2e.config file")
		workerMachineCount, err := strconv.ParseInt(e2eConfig.Variables["WORKER_MACHINE_COUNT"], 10, 0)
		Expect(err).ToNot(HaveOccurred(), "Invalid configuration. The WORKER_MACHINE_COUNT variable should be a valid integer number")

		By("Running clusterctl config cluster to get a cluster template")
		workloadClusterTemplate := clusterctl.ConfigCluster(context.TODO(), clusterctl.ConfigClusterInput{
			// pass reference to the management cluster hosting this test
			KubeconfigPath: managementClusterKubeConfigPath,
			// pass the clusterctl config file that points to the local provider repository created for this test,
			ClusterctlConfigPath: clusterctlConfigPath,
			// define the cluster template source
			InfrastructureProvider: e2eConfig.InfraProvider(),
			// define template variables
			Namespace:                namespaceName,
			ClusterName:              clusterName,
			KubernetesVersion:        e2eConfig.Variables["KUBERNETES_VERSION"],
			ControlPlaneMachineCount: &controlPlaneMachineCount,
			WorkerMachineCount:       &workerMachineCount,
			// setup output path for clusterctl logs
			LogPath: logPath,
		})
		Expect(workloadClusterTemplate).ToNot(BeNil(), "Failed to get the cluster template")

		By("Applying the cluster template to the cluster")
		managementCluster.Apply(context.TODO(), workloadClusterTemplate)

		By("Waiting for the cluster infrastructure to be provisioned")
		cluster = discovery.GetClusterByName(context.TODO(), discovery.GetClusterByNameInput{
			Getter:    client,
			Name:      clusterName,
			Namespace: namespaceName,
		})
		Expect(cluster).ToNot(BeNil(), "Failed to get the Cluster object")
		framework.WaitForClusterToProvision(context.TODO(), framework.WaitForClusterToProvisionInput{
			Getter:  client,
			Cluster: cluster,
		}, e2eConfig.IntervalsOrDefault("quick-start/wait-provision-cluster", "3m", "10s")...)

		By("Waiting for the first control plane machine to be provisioned")
		controlPlane := discovery.GetKubeadmControlPlaneByCluster(context.TODO(), discovery.GetKubeadmControlPlaneByClusterInput{
			Lister:      client,
			ClusterName: clusterName,
			Namespace:   namespaceName,
		})
		Expect(controlPlane).ToNot(BeNil())
		framework.WaitForOneKubeadmControlPlaneMachineToExist(context.TODO(), framework.WaitForOneKubeadmControlPlaneMachineToExistInput{
			Lister:       client,
			Cluster:      cluster,
			ControlPlane: controlPlane,
		}, e2eConfig.IntervalsOrDefault("quick-start/wait-provision-first-control-plane-node", "3m", "10s")...)

		By("Installing Calico on the workload cluster")
		workloadClient, err := managementCluster.GetWorkloadClient(context.TODO(), namespaceName, clusterName)
		Expect(err).ToNot(HaveOccurred(), "Failed to get the client for the workload cluster with name %s", cluster.Name)

		//TODO: read calico manifest from data folder, so we are removing an external variable that might affect the test
		//TODO: CNI plugin configurable/pluggable (see CAPA)?
		applyYAMLURLInput := framework.ApplyYAMLURLInput{
			Client:        workloadClient,
			HTTPGetter:    http.DefaultClient,
			NetworkingURL: "https://docs.projectcalico.org/manifests/calico.yaml",
			Scheme:        scheme,
		}
		framework.ApplyYAMLURL(context.TODO(), applyYAMLURLInput)

		By("Waiting for the remaining control plane machines to be provisioned (if any)")
		if controlPlane.Spec.Replicas != nil && int(*controlPlane.Spec.Replicas) > 1 {
			framework.WaitForKubeadmControlPlaneMachinesToExist(context.TODO(), framework.WaitForKubeadmControlPlaneMachinesToExistInput{
				Lister:       client,
				Cluster:      cluster,
				ControlPlane: controlPlane,
			}, e2eConfig.IntervalsOrDefault("quick-start/wait-provision-remaining-control-plane-nodes", "3m", "10s")...)

			//TODO: apparently the next text does not pass on docker :-(

			// Wait for the control plane to be ready
			/*
					waitForControlPlaneToBeReadyInput := coreframework.WaitForControlPlaneToBeReadyInput{
					Getter:       client,
					ControlPlane: controlPlane,
				}
				coreframework.WaitForControlPlaneToBeReady(ctx, waitForControlPlaneToBeReadyInput)
			*/
		}

		By("Waiting for the worker machines to be provisioned")
		machineDeployments := discovery.GetMachineDeploymentsByCluster(context.TODO(), discovery.GetMachineDeploymentsByClusterInput{
			Lister:      client,
			ClusterName: clusterName,
			Namespace:   "default",
		})
		for _, deployment := range machineDeployments {
			framework.WaitForMachineDeploymentNodesToExist(context.TODO(), framework.WaitForMachineDeploymentNodesToExistInput{
				Lister:            client,
				Cluster:           cluster,
				MachineDeployment: deployment,
			}, e2eConfig.IntervalsOrDefault("quick-start/wait-provision-worker-nodes", "3m", "10s")...)
		}
	})

	AfterEach(func() {
		//TODO: dump all the ClusterAPI resources in the namespace

		//TODO: refactor this into a framework function
		By("Deleting the Cluster object")
		framework.DeleteCluster(context.TODO(), framework.DeleteClusterInput{
			Deleter: client,
			Cluster: cluster,
		})

		By("Waiting for the Cluster object to be deleted")
		framework.WaitForClusterDeleted(context.TODO(), framework.WaitForClusterDeletedInput{
			Getter:  client,
			Cluster: cluster,
		}, e2eConfig.IntervalsOrDefault("quick-start/wait-delete-cluster", "3m", "10s")...)

		//TODO: make this generic (work with
		By("Check for all the Cluster API resources being deleted")
		framework.AssertAllClusterAPIResourcesAreGone(context.TODO(), framework.AssertAllClusterAPIResourcesAreGoneInput{
			Lister:  client,
			Cluster: cluster,
		})
	})
})
