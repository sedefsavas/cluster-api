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
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha3"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1alpha3"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// DumpCAPIResources dump cluster API related resources to YAML
func DumpCAPIResources(mgmt ManagementCluster, resourcePath string, writer io.Writer, options ...client.ListOption) error {
	resources := map[string]runtime.Object{
		"Cluster":             &clusterv1.ClusterList{},
		"MachineDeployment":   &clusterv1.MachineDeploymentList{},
		"MachineSet":          &clusterv1.MachineSetList{},
		"MachinePool":         &expv1.MachinePoolList{},
		"Machine":             &clusterv1.MachineList{},
		"KubeadmControlPlane": &controlplanev1.KubeadmControlPlaneList{},
		"KubeadmConfig":       &bootstrapv1.KubeadmConfigList{},
		"Node":                &corev1.NodeList{},
	}

	return dumpResources(mgmt, resources, resourcePath, writer, options...)
}

// DumpProviderResources dump provider specific API related resources to YAML
func DumpProviderResources(mgmt ManagementCluster, resources map[string]runtime.Object, resourcePath string, writer io.Writer, options ...client.ListOption) error {
	return dumpResources(mgmt, resources, resourcePath, writer, options...)
}

func dumpResources(mgmt ManagementCluster, resources map[string]runtime.Object, resourcePath string, writer io.Writer, options ...client.ListOption) error {
	c, err := mgmt.GetClient()
	if err != nil {
		return err
	}

	for kind, resourceList := range resources {
		if err := c.List(context.TODO(), resourceList, options...); err != nil {
			return errors.Wrapf(err, "error getting resources of kind %s", kind)
		}

		objs, err := apimeta.ExtractList(resourceList)
		if err != nil {
			return errors.Wrapf(err, "error extracting list of kind %s", kind)
		}

		for _, obj := range objs {
			metaObj, _ := apimeta.Accessor(obj)
			if err != nil {
				return err
			}

			namespace := metaObj.GetNamespace()
			name := metaObj.GetName()

			resourceFilePath := path.Join(resourcePath, namespace, kind, name+".yaml")
			if err := dumpResource(resourceFilePath, obj, writer); err != nil {
				return err
			}
		}
	}

	return nil
}

// ListByNamespaceOptions returns a set of ListOptions that allows listing objects only in a particular namespace.
func ListByNamespaceOptions(namespace string) []client.ListOption {
	return []client.ListOption{
		client.InNamespace(namespace),
	}
}

func dumpResource(resourceFilePath string, resource runtime.Object, writer io.Writer) error {
	fmt.Fprintf(writer, "Creating directory: %s\n", filepath.Dir(resourceFilePath))
	if err := os.MkdirAll(filepath.Dir(resourceFilePath), 0755); err != nil {
		return errors.Wrapf(err, "error making logDir %q", filepath.Dir(resourceFilePath))
	}

	f, err := os.OpenFile(resourceFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return errors.Wrapf(err, "error opening created logFile %q", resourceFilePath)
	}
	defer f.Close()

	resourceYAML, err := yaml.Marshal(resource)
	if err != nil {
		return errors.Wrapf(err, "error marshaling cluster ")
	}

	if err := ioutil.WriteFile(f.Name(), resourceYAML, 0644); err != nil {
		return errors.Wrapf(err, "error writing cluster yaml to file %q", f.Name())
	}

	return nil
}
