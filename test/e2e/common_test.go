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
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/cluster-api/test/framework/discovery"
	dockerv1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func valueOrDefault(v string) string {
	if v != "" {
		return v
	}
	return "(default)"
}

type SpecContext struct {
	clusterctlConfigPath string
	managementCluster    framework.ManagementCluster
	namespace            *corev1.Namespace
	logPath              string
	getIntervals         func(key string) []interface{}
}

type initSpecInput struct {
	specName             string
	clusterctlConfigPath string
	managementCluster    framework.ManagementCluster
	artifactFolder       string
	getIntervals         func(spec, key string) []interface{}
}

func initSpec(input initSpecInput) *SpecContext {
	//TODO: validate test args

	client := getClient(input.managementCluster)

	logPath := filepath.Join(artifactFolder, input.specName)
	Expect(os.MkdirAll(logPath, 0755)).To(Succeed(), "Failed to create log folder %s", logPath)

	//TODO: stream logs out of the namespace
	namespaceName := fmt.Sprintf("%s-%s", input.specName, util.RandomString(6))
	namespace := framework.CreateNamespace(context.TODO(), framework.CreateNamespaceInput{Creator: client, Name: namespaceName}, "40s", "10s")
	Expect(namespace).ToNot(BeNil(), "Failed to create namespace %s", namespaceName)

	return &SpecContext{
		clusterctlConfigPath: input.clusterctlConfigPath,
		managementCluster:    input.managementCluster,
		namespace:            namespace,
		logPath:              logPath,
		getIntervals: func(key string) []interface{} {
			return input.getIntervals(input.specName, key)
		},
	}
}

type createClusterTemplateInput struct {
	getClusterTemplateInput
}

func createClusterTemplate(spec *SpecContext, input createClusterTemplateInput) (*clusterv1.Cluster, *controlplanev1.KubeadmControlPlane, []*clusterv1.MachineDeployment) {
	By("Getting a cluster template yaml")
	workloadClusterTemplate := getClusterTemplate(spec, input.getClusterTemplateInput)

	By("Applying the cluster template yaml to the cluster")
	managementCluster.Apply(context.TODO(), workloadClusterTemplate)

	By("Waiting for the cluster infrastructure to be provisioned")
	cluster := waitForCluster(spec, input.clusterName)

	By("Waiting for control plane to be provisioned")
	controlPlane := waitForControlPlane(spec, cluster)

	By("Waiting for the worker machines to be provisioned")
	machineDeployments := waitForMachineDeployments(spec, cluster)

	return cluster, controlPlane, machineDeployments
}

type getClusterTemplateInput struct {
	flavor                   string
	clusterName              string
	kubernetesVersion        string
	controlPlaneMachineCount int64
	workerMachineCount       int64
}

func getClusterTemplate(spec *SpecContext, input getClusterTemplateInput) []byte {
	workloadClusterTemplate := clusterctl.ConfigCluster(context.TODO(), clusterctl.ConfigClusterInput{
		// pass reference to the management cluster hosting this test
		KubeconfigPath: managementCluster.GetKubeconfigPath(),
		// pass the clusterctl config file that points to the local provider repository created for this test,
		ClusterctlConfigPath: spec.clusterctlConfigPath,
		// select template
		Flavor: input.flavor,
		// define template variables
		Namespace:                spec.namespace.Name,
		ClusterName:              input.clusterName,
		KubernetesVersion:        input.kubernetesVersion,
		ControlPlaneMachineCount: &input.controlPlaneMachineCount,
		WorkerMachineCount:       &input.workerMachineCount,
		// setup output path for clusterctl logs
		LogPath: spec.logPath,
	})
	Expect(workloadClusterTemplate).ToNot(BeNil(), "Failed to get the cluster template")

	return workloadClusterTemplate
}

func waitForCluster(spec *SpecContext, name string) *clusterv1.Cluster {
	client := getClient(spec.managementCluster)

	cluster := discovery.GetClusterByName(context.TODO(), discovery.GetClusterByNameInput{
		Getter:    client,
		Name:      name,
		Namespace: spec.namespace.Name,
	})
	Expect(cluster).ToNot(BeNil(), "Failed to get the Cluster object")
	framework.WaitForClusterToProvision(context.TODO(), framework.WaitForClusterToProvisionInput{
		Getter:  client,
		Cluster: cluster,
	}, spec.getIntervals("wait-cluster")...)

	return cluster
}

//TODO:  cniPath
//TODO: consider if to break down this in wait first cp, apply cni, wait other cp
func waitForControlPlane(spec *SpecContext, cluster *clusterv1.Cluster) *controlplanev1.KubeadmControlPlane {
	client := getClient(spec.managementCluster)

	var cniPath = "./data/calico/calico.yaml"

	controlPlane := discovery.GetKubeadmControlPlaneByCluster(context.TODO(), discovery.GetKubeadmControlPlaneByClusterInput{
		Lister:      client,
		ClusterName: cluster.Name,
		Namespace:   cluster.Namespace,
	})
	Expect(controlPlane).ToNot(BeNil())

	Byf("Waiting for the first control plane machine managed by %s/%s to be provisioned", controlPlane.Namespace, controlPlane.Name)
	framework.WaitForOneKubeadmControlPlaneMachineToExist(context.TODO(), framework.WaitForOneKubeadmControlPlaneMachineToExistInput{
		Lister:       client,
		Cluster:      cluster,
		ControlPlane: controlPlane,
	}, spec.getIntervals("wait-first-control-plane-node")...)

	By("Installing Calico on the workload cluster")
	workloadClient, err := spec.managementCluster.GetWorkloadClient(context.TODO(), cluster.Namespace, cluster.Name)
	Expect(err).ToNot(HaveOccurred(), "Failed to get the client for the workload cluster with name %s", cluster.Name)

	//TODO: make cni configurable in the e2econfig file (by provider). Use variables?
	//TODO: refactor this. using the same input struct of ApplyYAMLURL is confusing. what about using managementCluster.Apply
	applyYAMLInput := framework.ApplyYAMLInput{
		Client:   workloadClient,
		Scheme:   spec.managementCluster.GetScheme(),
		Filename: cniPath,
	}
	framework.ApplyYAMLFromFile(context.TODO(), applyYAMLInput)

	if controlPlane.Spec.Replicas != nil && int(*controlPlane.Spec.Replicas) > 1 {
		Byf("for the remaining control plane machines managed by %s/%s to be provisioned", controlPlane.Namespace, controlPlane.Name)
		framework.WaitForKubeadmControlPlaneMachinesToExist(context.TODO(), framework.WaitForKubeadmControlPlaneMachinesToExistInput{
			Lister:       client,
			Cluster:      cluster,
			ControlPlane: controlPlane,
		}, spec.getIntervals("wait-other-control-plane-nodes")...)

		//TODO: apparently the next test does not pass on docker :-(
		// Wait for the control plane to be ready
		/*
				waitForControlPlaneToBeReadyInput := coreframework.WaitForControlPlaneToBeReadyInput{
				Getter:       client,
				ControlPlane: controlPlane,
			}
			coreframework.WaitForControlPlaneToBeReady(ctx, waitForControlPlaneToBeReadyInput)
		*/
	}

	return controlPlane
}

func waitForMachineDeployments(spec *SpecContext, cluster *clusterv1.Cluster) []*clusterv1.MachineDeployment {
	client := getClient(spec.managementCluster)

	machineDeployments := discovery.GetMachineDeploymentsByCluster(context.TODO(), discovery.GetMachineDeploymentsByClusterInput{
		Lister:      client,
		ClusterName: cluster.Name,
		Namespace:   cluster.Namespace,
	})
	for _, deployment := range machineDeployments {
		framework.WaitForMachineDeploymentNodesToExist(context.TODO(), framework.WaitForMachineDeploymentNodesToExistInput{
			Lister:            client,
			Cluster:           cluster,
			MachineDeployment: deployment,
		}, spec.getIntervals("wait-worker-nodes")...)
	}
	return machineDeployments
}

func dumpResources(spec *SpecContext) {
	//TODO: make dump generic -> discovery cluster API crds, dump * (See clusterctl move / clusterctl delete)
	resourcesPath := filepath.Join(spec.logPath, "resources")
	Expect(os.MkdirAll(resourcesPath, 0755)).To(Succeed(), "Failed to create resource folder %s", resourcesPath)

	// Dump cluster API and docker related resources to artifacts before deleting them.
	Expect(framework.DumpCAPIResources(managementCluster, resourcesPath, GinkgoWriter, framework.ListByNamespaceOptions(spec.namespace.Name)...)).To(Succeed())
	resources := map[string]runtime.Object{
		"DockerCluster":         &dockerv1.DockerClusterList{},
		"DockerMachine":         &dockerv1.DockerMachineList{},
		"DockerMachineTemplate": &dockerv1.DockerMachineTemplateList{},
	}
	Expect(framework.DumpProviderResources(managementCluster, resources, resourcesPath, GinkgoWriter, framework.ListByNamespaceOptions(spec.namespace.Name)...)).To(Succeed())
}

func deleteCluster(spec *SpecContext, cluster *clusterv1.Cluster) {
	client := getClient(spec.managementCluster)

	framework.DeleteCluster(context.TODO(), framework.DeleteClusterInput{
		Deleter: client,
		Cluster: cluster,
	})

	By("Waiting for the Cluster object to be deleted")
	framework.WaitForClusterDeleted(context.TODO(), framework.WaitForClusterDeletedInput{
		Getter:  client,
		Cluster: cluster,
	}, spec.getIntervals("wait-delete-cluster")...)

	//TODO: make this generic (work with all the providers --> discovery, same as dump resources
	By("Check for all the Cluster API resources being deleted")
	framework.AssertAllClusterAPIResourcesAreGone(context.TODO(), framework.AssertAllClusterAPIResourcesAreGoneInput{
		Lister:  client,
		Cluster: cluster,
	})
}

func getClient(managementCluster framework.ManagementCluster) ctrlclient.Client {
	client, err := managementCluster.GetClient()
	Expect(err).NotTo(HaveOccurred(), "Failed to get controller runtime client for the management cluster")
	return client
}
