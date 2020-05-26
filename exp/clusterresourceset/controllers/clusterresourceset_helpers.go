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

package controllers

import (
	"bufio"
	"bytes"
	"context"
	"io"

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	clusterresourcesetv1 "sigs.k8s.io/cluster-api/exp/clusterresourceset/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"
)

// ToUnstructured takes a YAML and converts it to a list of Unstructured objects
func ToUnstructured(rawyaml []byte) ([]unstructured.Unstructured, error) {
	var ret []unstructured.Unstructured //nolint

	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(rawyaml)))
	count := 1
	for {
		// Read one YAML document at a time, until io.EOF is returned
		b, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, errors.Wrapf(err, "failed to read yaml")
		}
		if len(b) == 0 {
			break
		}

		var m map[string]interface{}
		if err := yaml.Unmarshal(b, &m); err != nil {
			return nil, errors.Wrapf(err, "failed to unmarshal the %s yaml document: %q", util.Ordinalize(count), string(b))
		}

		var u unstructured.Unstructured
		u.SetUnstructuredContent(m)

		// Ignore empty objects.
		// Empty objects are generated if there are weird things in manifest files like e.g. two --- in a row without a yaml doc in the middle
		if u.Object == nil {
			continue
		}

		ret = append(ret, u)
		count++
	}

	return ret, nil
}

func Apply(ctx context.Context, c client.Client, data []byte) error {
	// Transform the yaml in a list of objects, so following transformation can work on typed objects (instead of working on a string/slice of bytes)
	reader := bufio.NewReader(bytes.NewReader(data))
	// TODO: Check how many bytes enough here
	_, _, isJSON := utilyaml.GuessJSONStream(reader, 2048)
	if isJSON {
		var err error
		data, err = yaml.JSONToYAML(data)
		if err != nil {
			return errors.Wrapf(err, "failed converting JSON data to YAML")
		}
	}
	objs, err := ToUnstructured(data)
	if err != nil {
		return errors.Wrapf(err, "failed converting data to unstructured objects")
	}

	// TODO: if one of them fails, return with error or continue creating other objects in the resource?
	for i := range objs {
		err = apply(ctx, c, &objs[i])
		if err != nil {
			return errors.Wrapf(err,
				"failed to applying resource %q/%q", objs[i].GetKind(), objs[i].GetName())
		}
	}
	return nil
}

func apply(ctx context.Context, c client.Client, obj *unstructured.Unstructured) error {
	// Create the object on the API server.
	if err := c.Create(ctx, obj); err != nil {
		// The create call is idempotent, so if the object already exists
		// then do not consider it to be an error.
		if !apierrors.IsAlreadyExists(err) {
			return errors.Wrapf(
				err,
				"failed to create object %s %s/%s",
				obj.GroupVersionKind(),
				obj.GetNamespace(),
				obj.GetName())
		}
		// If the object already exists, try to update it.
		if err := c.Update(ctx, obj); err != nil {
			return errors.Wrapf(
				err,
				"failed to update object %s %s/%s",
				obj.GroupVersionKind(),
				obj.GetNamespace(),
				obj.GetName())
		}
	}
	return nil
}

// GetorCreateClusterResourceSetBinding retrieves ClusterResourceSetBinding resource owned by the cluster or create a new one if not found.
func GetorCreateClusterResourceSetBinding(ctx context.Context, c client.Client, cluster *clusterv1.Cluster) (*clusterresourcesetv1.ClusterResourceSetBinding, error) {
	clusterResourceSetBinding := &clusterresourcesetv1.ClusterResourceSetBinding{}
	clusterResourceSetBindingKey := client.ObjectKey{
		Namespace: cluster.Namespace,
		Name:      cluster.Name,
	}

	if err := c.Get(ctx, clusterResourceSetBindingKey, clusterResourceSetBinding); err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, err
		}

		clusterResourceSetBinding.Name = cluster.Name
		clusterResourceSetBinding.Namespace = cluster.Namespace
		clusterResourceSetBinding.OwnerReferences = util.EnsureOwnerRef(clusterResourceSetBinding.OwnerReferences, metav1.OwnerReference{
			APIVersion: clusterv1.GroupVersion.String(),
			Kind:       "Cluster",
			Name:       cluster.Name,
			UID:        cluster.UID,
		})
		if err := c.Create(ctx, clusterResourceSetBinding); err != nil {
			if apierrors.IsAlreadyExists(err) {
				err = c.Get(ctx, clusterResourceSetBindingKey, clusterResourceSetBinding)
				return clusterResourceSetBinding, err
			}
			return nil, errors.Wrapf(err, "failed to create clusterResourceSetBinding for cluster: %s/%s", cluster.Namespace, cluster.Name)
		}
	}

	return clusterResourceSetBinding, nil
}

// InitClusterResourceSetBinding initializes ClusterResourceSetBinding object for a  ClusterResourceSet if not already.
func InitClusterResourceSetBinding(clusterResourceSetBinding *clusterresourcesetv1.ClusterResourceSetBinding, clusterResourceSet *clusterresourcesetv1.ClusterResourceSet) {
	if clusterResourceSetBinding.ClusterResourceSetMap == nil {
		clusterResourceSetBinding.ClusterResourceSetMap = map[string]clusterresourcesetv1.ResourcesInClusterResourceSet{}
	}

	// Add ClusterResourceSet to clusterResourceSetBinding if not exist
	if _, ok := clusterResourceSetBinding.ClusterResourceSetMap[clusterResourceSet.Name]; !ok {
		clusterResourceSetBinding.ClusterResourceSetMap[clusterResourceSet.Name] = clusterresourcesetv1.ResourcesInClusterResourceSet{Resources: map[string]clusterresourcesetv1.ClusterResourceSetResourceStatus{}}
	}
}
