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
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/cluster-api/test/framework/discovery"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
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
	cancelWatches        context.CancelFunc
}

type initSpecInput struct {
	specName             string
	clusterctlConfigPath string
	managementCluster    framework.ManagementCluster
	artifactFolder       string
	getIntervals         func(spec, key string) []interface{}
}

func initSpec(input initSpecInput) *SpecContext {
	//TODO: validate spec args

	client := getClient(input.managementCluster)

	logPath := filepath.Join(artifactFolder, input.specName)
	Expect(os.MkdirAll(logPath, 0755)).To(Succeed(), "Failed to create log folder %s", logPath)

	namespaceName := fmt.Sprintf("%s-%s", input.specName, util.RandomString(6))
	namespace := framework.CreateNamespace(context.TODO(), framework.CreateNamespaceInput{Creator: client, Name: namespaceName}, "40s", "10s")
	Expect(namespace).ToNot(BeNil(), "Failed to create namespace %s", namespaceName)

	// watch the namespace for events ...
	clientSet, err := input.managementCluster.GetClientSet()
	Expect(err).NotTo(HaveOccurred(), "Failed to get client go client for the management cluster")

	ctx, cancelWatches := context.WithCancel(context.Background())
	go func() {
		defer GinkgoRecover()
		framework.WatchNamespaceEvents(ctx, framework.WatchNamespaceEventsInput{
			ClientSet: clientSet,
			Name:      namespace.Name,
			LogPath:   logPath,
		})
	}()

	return &SpecContext{
		clusterctlConfigPath: input.clusterctlConfigPath,
		managementCluster:    input.managementCluster,
		namespace:            namespace,
		logPath:              logPath,
		getIntervals: func(key string) []interface{} {
			return input.getIntervals(input.specName, key)
		},
		cancelWatches: cancelWatches,
	}
}

func (s *SpecContext) Cleanup() {
	client := getClient(s.managementCluster)
	framework.DeleteNamespace(context.TODO(), framework.DeleteNamespaceInput{Deleter: client, Name: s.namespace.Name}, "40s", "10s")
}

func (s *SpecContext) Close() {
	s.cancelWatches()
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

type upgradeKCPInput struct {
	cluster      *clusterv1.Cluster
	controlPlane *controlplanev1.KubeadmControlPlane
	etcdImageTag string
	dnsImageTag  string
}

func upgradeKCP(spec *SpecContext, input upgradeKCPInput) []clusterv1.Machine {
	client := getClient(spec.managementCluster)

	By("Patching the new kubernetes version to KCP")
	patchHelper, err := patch.NewHelper(input.controlPlane, client)
	Expect(err).ToNot(HaveOccurred())
	input.controlPlane.Spec.Version = e2eConfig.GetKubernetesUpgradeVersion()
	Expect(input.controlPlane.Spec.Version).NotTo(BeNil())
	Expect(patchHelper.Patch(ctx, input.controlPlane)).To(Succeed())

	By("Waiting for machines to have the upgraded kubernetes version")
	machines := discovery.GetMachinesByCluster(context.TODO(), discovery.GetMachinesByClusterInput{
		Lister:      client,
		ClusterName: input.cluster.Name,
		Namespace:   input.cluster.Namespace,
	})

	framework.WaitForMachinesToBeUpgraded(ctx, framework.WaitForMachinesToBeUpgradedInput{
		Lister:            client,
		Cluster:           input.cluster,
		Machines:          machines,
		MachineCount:      int(*input.controlPlane.Spec.Replicas),
		KubernetesVersion: input.controlPlane.Spec.Version,
	}, spec.getIntervals("wait-machine-upgrade")...)

	//TODO: add etcd, DNS, and kube-proxy checks too
	return machines
}

func dumpResources(spec *SpecContext) {
	client := getClient(spec.managementCluster)

	// Dump all Cluster API related resources to artifacts before deleting them.
	discovery.DumpAllResources(context.TODO(), discovery.DumpAllResourcesInput{
		Lister:    client,
		Namespace: spec.namespace.Name,
		LogPath:   filepath.Join(spec.logPath, "resources"),
	})
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

	By("Check for all the Cluster API resources being deleted")
	resources := discovery.GetCAPIResources(context.TODO(), discovery.GetCAPIResourcesInput{
		Lister:    client,
		Namespace: spec.namespace.Name,
	})
	Expect(resources).To(BeEmpty(), "There are still Cluster API resources in the %s namespace", spec.namespace.Name)
}

func getClient(managementCluster framework.ManagementCluster) ctrlclient.Client {
	client, err := managementCluster.GetClient()
	Expect(err).NotTo(HaveOccurred(), "Failed to get controller runtime client for the management cluster")
	return client
}
