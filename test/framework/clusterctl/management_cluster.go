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

package clusterctl

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clusterctlconfig "sigs.k8s.io/cluster-api/cmd/clusterctl/client/config"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Provides utilities for setting up a management cluster using clusterctl.

// CreateManagementClusterInput is the information required to CreateManagementCluster a new management cluster for e2e testing.
type CreateManagementClusterInput struct {
	// Name of the cluster
	Name string

	// Images to be loaded in the cluster (this is kind specific)
	Images []framework.ContainerImage

	// Scheme is used to initialize the scheme for the management cluster client.
	Scheme *runtime.Scheme

	// NewManagementClusterFn should return a new management cluster.
	NewManagementClusterFn func(name string, scheme *runtime.Scheme) (cluster framework.ManagementCluster, err error)
}

// CreateManagementCluster returns a new empty management cluster eventually with the required images pre-loaded (this is kind specific).
func CreateManagementCluster(ctx context.Context, input *CreateManagementClusterInput) (framework.ManagementCluster, error) {
	Expect(input.Name).ToNot(BeEmpty(), "Invalid argument. Name can't be empty when calling CreateManagementCluster")
	Expect(input.Scheme).ToNot(BeNil(), "Invalid argument. Scheme can't be nil when calling CreateManagementCluster")

	By(fmt.Sprintf("Creating the management cluster with name %s", input.Name))

	managementCluster, err := input.NewManagementClusterFn(input.Name, input.Scheme)
	Expect(err).ToNot(HaveOccurred(), "Failed to create the management cluster with name %s", input.Name)
	Expect(managementCluster).ToNot(BeNil(), "The management cluster with name %s should not be nil", input.Name)
	Expect(managementCluster.GetKubeconfigPath()).To(BeAnExistingFile(), "The kubeConfig file for the management cluster with name %s does not exists at %s", input.Name, managementCluster.GetKubeconfigPath())

	// Load the images into the cluster.
	if imageLoader, ok := managementCluster.(framework.ImageLoader); ok {
		By("Loading images into the management cluster")

		for _, image := range input.Images {
			err := imageLoader.LoadImage(ctx, image.Name)
			switch image.LoadBehavior {
			case framework.MustLoadImage:
				Expect(err).ToNot(HaveOccurred(), "Failed to load image %s into the kind cluster", image.Name)
			case framework.TryLoadImage:
				if err != nil {
					fmt.Fprintf(GinkgoWriter, "[WARNING] Unable to load image %s into the kind cluster: %v \n", image.Name, err)
				}
			}
		}
	}

	return managementCluster, nil
}

// InitManagementClusterInput is the information required to initialize a management cluster by running "clusterctl init".
type InitManagementClusterInput struct {
	// ManagementCluster to initialize.
	ManagementCluster framework.ManagementCluster

	// InfrastructureProviders to be installed during "clusterctl init".
	InfrastructureProviders []string

	// ClusterctlConfigPath is the path to a clusterctl config file that points to repositories to be used during "clusterctl init".
	ClusterctlConfigPath string

	// WaitForProviderIntervals
	WaitForProviderIntervals []interface{}

	// LogsFolder defines a folder where to store clusterctl logs.
	LogsFolder string
}

// InitManagementCluster initialize a management cluster by running "clusterctl init", waits for the providers to come up
// and finally creates the log watcher for all the controller logs.
func InitManagementCluster(ctx context.Context, input *InitManagementClusterInput) {
	// validate parameters and apply defaults

	Expect(input.ManagementCluster).ToNot(BeNil(), "Invalid argument. input.ManagementCluster can't be nil when calling InitManagementCluster")
	Expect(input.InfrastructureProviders).ToNot(BeEmpty(), "Invalid argument. input.InfrastructureProviders can't be empty when calling InitManagementCluster")
	Expect(input.ClusterctlConfigPath).To(BeAnExistingFile(), "Invalid argument. input.ClusterctlConfigPath should be an existing file")
	Expect(os.MkdirAll(input.LogsFolder, 0755)).To(Succeed(), "Invalid argument. can't create input.LogsFolder %s", input.LogsFolder)

	Init(ctx, InitInput{
		// pass reference to the management cluster hosting this test
		KubeconfigPath: input.ManagementCluster.GetKubeconfigPath(),
		// pass the clusterctl config file that points to the local provider repository created for this test,
		ClusterctlConfigPath: input.ClusterctlConfigPath,
		// setup the desired list of providers for a single-tenant management cluster
		CoreProvider:            clusterctlconfig.ClusterAPIProviderName,
		BootstrapProviders:      []string{clusterctlconfig.KubeadmBootstrapProviderName},
		ControlPlaneProviders:   []string{clusterctlconfig.KubeadmControlPlaneProviderName},
		InfrastructureProviders: input.InfrastructureProviders,
		// setup output path for clusterctl logs
		LogPath: input.LogsFolder,
	})

	By("Waiting for providers controllers to be running")

	client, err := input.ManagementCluster.GetClient()
	Expect(err).NotTo(HaveOccurred(), "Failed to get controller runtime client for the management cluster")
	controllersDeployments := discovery.GetControllerDeployments(ctx, discovery.GetControllerDeploymentsInput{
		Lister: client,
	})
	Expect(controllersDeployments).ToNot(BeEmpty(), "The list of controller deployments should not be empty")
	for _, deployment := range controllersDeployments {
		framework.WaitForDeploymentsAvailable(ctx, framework.WaitForDeploymentsAvailableInput{
			Getter:     client,
			Deployment: deployment,
		}, input.WaitForProviderIntervals...)

		// Start streaming logs from all controller providers
		watchLogs(ctx, input.ManagementCluster, deployment.Namespace, deployment.Name, input.LogsFolder)
	}
}

// TODO: move this method in the framework
// watchLogs streams logs for all containers for all pods belonging to a deployment. Each container's logs are streamed
// in a separate goroutine so they can all be streamed concurrently. This only causes a test failure if there are errors
// retrieving the deployment, its pods, or setting up a log file. If there is an error with the log streaming itself,
// that does not cause the test to fail.
func watchLogs(ctx context.Context, mgmt framework.ManagementCluster, namespace, deploymentName, logDir string) error {
	c, err := mgmt.GetClient()
	if err != nil {
		return err
	}
	clientSet, err := mgmt.GetClientSet()
	if err != nil {
		return err
	}

	deployment := &appsv1.Deployment{}
	Expect(c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: deploymentName}, deployment)).To(Succeed(), "Failed to get deployment %s/%s", namespace, deploymentName)

	selector, err := metav1.LabelSelectorAsMap(deployment.Spec.Selector)
	Expect(err).NotTo(HaveOccurred(), "Failed to Pods selector for deployment %s/%s", namespace, deploymentName)

	pods := &corev1.PodList{}
	Expect(c.List(ctx, pods, client.InNamespace(namespace), client.MatchingLabels(selector))).To(Succeed(), "Failed to list Pods for deployment %s/%s", namespace, deploymentName)

	for _, pod := range pods.Items {
		for _, container := range deployment.Spec.Template.Spec.Containers {
			// Watch each container's logs in a goroutine so we can stream them all concurrently.
			go func(pod corev1.Pod, container corev1.Container) {
				defer GinkgoRecover()

				logFile := path.Join(logDir, deploymentName, pod.Name, container.Name+".log")
				fmt.Fprintf(GinkgoWriter, "Creating directory: %s\n", filepath.Dir(logFile))
				Expect(os.MkdirAll(filepath.Dir(logFile), 0755)).To(Succeed())

				fmt.Fprintf(GinkgoWriter, "Creating file: %s\n", logFile)
				f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
				Expect(err).NotTo(HaveOccurred())
				defer f.Close()

				opts := &corev1.PodLogOptions{
					Container: container.Name,
					Follow:    true,
				}

				podLogs, err := clientSet.CoreV1().Pods(namespace).GetLogs(pod.Name, opts).Stream()
				if err != nil {
					// Failing to stream logs should not cause the test to fail
					fmt.Fprintf(GinkgoWriter, "Error starting logs stream for pod %s/%s, container %s: %v\n", namespace, pod.Name, container.Name, err)
					return
				}
				defer podLogs.Close()

				out := bufio.NewWriter(f)
				defer out.Flush()
				_, err = out.ReadFrom(podLogs)
				if err != nil && err != io.ErrUnexpectedEOF {
					// Failing to stream logs should not cause the test to fail
					fmt.Fprintf(GinkgoWriter, "Got error while streaming logs for pod %s/%s, container %s: %v\n", namespace, pod.Name, container.Name, err)
				}
			}(pod, container)
		}
	}
	return nil
}
