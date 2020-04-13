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

package management

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/cluster-api/controllers/remote"
	"sigs.k8s.io/cluster-api/test/framework/exec"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BaseCluster provides a base implementation of the ManagementCluster interface.
type BaseCluster struct {
	kubeconfigPath string
	scheme         *runtime.Scheme
}

// NewBaseCluster returns a BaseCluster given a KubeconfigPath and the scheme defining the types hosted in the cluster.
func NewBaseCluster(kubeconfigPath string, scheme *runtime.Scheme) *BaseCluster {
	// If a kubeconfig file isn't provided, find one in the standard locations.
	if kubeconfigPath == "" {
		kubeconfigPath = clientcmd.NewDefaultClientConfigLoadingRules().GetDefaultFilename()
	}
	return &BaseCluster{
		kubeconfigPath: kubeconfigPath,
		scheme:         scheme,
	}
}

// Apply wraps `kubectl apply` and prints the output so we can see what gets applied to the cluster.
func (c *BaseCluster) Apply(ctx context.Context, resources []byte) error {
	return exec.KubectlApply(ctx, c.kubeconfigPath, resources)
}

// Wait wraps `kubectl wait`.
func (c *BaseCluster) Wait(ctx context.Context, args ...string) error {
	return exec.KubectlWait(ctx, c.kubeconfigPath, args...)
}

// Teardown the cluster.
// For clusters not created by the E2E framework this is no-op.
func (c BaseCluster) Teardown(context.Context) {}

// GetName returns the name of the cluster.
// For clusters not created by the E2E framework it returns the name of the cluster as defined in the kubeconfig file
// or unknown in case of error in retrieving it.
func (c BaseCluster) GetName() string {
	config, err := clientcmd.LoadFromFile(c.kubeconfigPath)
	if err != nil {
		return "unknown"
	}

	if config.CurrentContext == "" {
		return "unknown"
	}

	v, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return "unknown"
	}

	if v.Cluster != "" {
		return v.Cluster
	}

	return "unknown"
}

// GetKubeconfigPath returns the path to the kubeconfig file for the cluster.
func (c BaseCluster) GetKubeconfigPath() string {
	return c.kubeconfigPath
}

// GetScheme returns the scheme defining the types hosted in the cluster.
func (c BaseCluster) GetScheme() *runtime.Scheme {
	return c.scheme
}

// GetClient returns a controller-runtime client for the management cluster.
func (c BaseCluster) GetClient() (client.Client, error) {
	config, err := c.getConfig()
	if err != nil {
		return nil, err
	}

	cl, err := client.New(config, client.Options{Scheme: c.scheme})
	if err != nil {
		return nil, err
	}
	return cl, nil
}

// GetClientSet returns a clientset to the management cluster to be used for object interface expansions such as pod logs.
func (c *BaseCluster) GetClientSet() (*kubernetes.Clientset, error) {
	restConfig, err := c.getConfig()
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return kubernetes.NewForConfig(restConfig)
}

// GetWorkloadClient returns a controller-runtime client for the workload cluster.
func (c BaseCluster) GetWorkloadClient(ctx context.Context, namespace, name string) (client.Client, error) {
	cl, err := c.GetClient()
	if err != nil {
		return nil, err
	}
	key := client.ObjectKey{
		Name:      name,
		Namespace: namespace,
	}
	return remote.NewClusterClient(ctx, cl, key, c.scheme)
}

// GetWorkerKubeconfigPath returns the path to the kubeconfig file for the specified workload cluster.
func (c *BaseCluster) GetWorkerKubeconfigPath(ctx context.Context, namespace, name string) (string, error) {
	panic("not implemented yet")
}

func (c BaseCluster) getConfig() (*rest.Config, error) {
	config, err := clientcmd.LoadFromFile(c.kubeconfigPath)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to load Kubeconfig file from %q", c.kubeconfigPath)
	}

	restConfig, err := clientcmd.NewDefaultClientConfig(*config, &clientcmd.ConfigOverrides{}).ClientConfig()
	if err != nil {
		if strings.HasPrefix(err.Error(), "invalid configuration:") {
			return nil, errors.New(strings.Replace(err.Error(), "invalid configuration:", "invalid kubeconfig file; clusterctl requires a valid kubeconfig file to connect to the management cluster:", 1))
		}
		return nil, err
	}
	restConfig.UserAgent = "cluster-api-e2e"

	return restConfig, nil
}
