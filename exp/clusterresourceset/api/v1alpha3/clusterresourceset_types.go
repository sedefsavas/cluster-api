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

package v1alpha3

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ClusterResourceSetSecretType is the only accepted type of secret in resources
	ClusterResourceSetSecretType corev1.SecretType = "clusterResourceSet" //nolint:gosec
)

// ANCHOR: ClusterResourceSetSpec

// ClusterResourceSetSpec defines the desired state of ClusterResourceSet
type ClusterResourceSetSpec struct {
	// Label selector for Clusters. The Clusters that are
	// selected by this will be the ones affected by this ClusterResourceSet.
	// It must match the Cluster labels.
	ClusterSelector metav1.LabelSelector `json:"clusterSelector"`
	// Resources is a list of Secrets/ConfigMaps where each contains 1 or more resources to be applied to remote clusters.
	Resources []*ClusterResourceSetResource `json:"resources,omitempty"`
	// Mode is the mode to be used during applying resources. Defaults to ClusterResourceSetModeApplyOnce.
	// +optional
	Mode string `json:"mode"`
}

// ANCHOR_END: ClusterResourceSetSpec

// ClusterResourceSetResourceKind is a string representation of a ClusterResourceSet resource kind.
type ClusterResourceSetResourceKind string

const (
	SecretClusterResourceSetResourceKind    ClusterResourceSetResourceKind = "Secret"
	ConfigMapClusterResourceSetResourceKind ClusterResourceSetResourceKind = "ConfigMap"
)

// ClusterResourceSetResource specifies a resource.
type ClusterResourceSetResource struct {
	// Name of the resource that is in the same namespace with ClusterResourceSet object.
	Name string `json:"name,omitempty"`
	// Kind of the resource. Supported kinds are: Secrets and ConfigMaps.
	Kind string `json:"kind,omitempty"`
}

// ClusterResourceSetMode is a string representation of a ClusterResourceSet Mode.
type ClusterResourceSetMode string

const (
	// ClusterResourceSetModeApplyOnce is the default mode a ClusterResourceSet mode is assigned by
	// ClusterResourceSet controller after being created if not specified by user.
	ClusterResourceSetModeApplyOnce ClusterResourceSetMode = "ApplyOnce"
)

// SetTypedMode sets the Mode field to the string representation of ClusterResourceSetMode.
func (c *ClusterResourceSetSpec) SetTypedMode(p ClusterResourceSetMode) {
	c.Mode = string(p)
}

// ANCHOR: ClusterResourceSetStatus

// ClusterResourceSetStatus defines the observed state of ClusterResourceSet
type ClusterResourceSetStatus struct {
	// LastUpdated identifies when the ClusterResourceSet last updated.
	// +optional
	LastUpdated *metav1.Time `json:"lastUpdated,omitempty"`
}

// ANCHOR_END: ClusterResourceSetStatus

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clusterresourcesets,scope=Namespaced,categories=cluster-api
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// ClusterResourceSet is the Schema for the clusterresourcesets API
type ClusterResourceSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterResourceSetSpec   `json:"spec,omitempty"`
	Status ClusterResourceSetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterResourceSetList contains a list of ClusterResourceSet
type ClusterResourceSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourceSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterResourceSet{}, &ClusterResourceSetList{})
}
