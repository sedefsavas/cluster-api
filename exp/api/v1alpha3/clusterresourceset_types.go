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

// ClusterResourceSetSpec defines the desired state of ClusterResourceSet
type ClusterResourceSetSpec struct {
	ClusterSelector metav1.LabelSelector `json:"clusterSelector"`
	// PostApplyAddons is a list of Secrets in YAML format to be applied to remote clusters.
	PostApplyAddons []*PostApplyAddon `json:"postApplyAddons,omitempty"`
}

// ANCHOR: PostApplyAddon

// PostApplyAddon specifies the addon's Secret parameters.
type PostApplyAddon struct {
	Name string `json:"name,omitempty"`
	// Namespace is the namespace of the secret.
	Namespace string `json:"namespace,omitempty"`
}

// ANCHOR_END: PostApplyAddon

// PostApplyConfigStatus defines the observed state of ClusterResourceSet
type PostApplyConfigStatus struct {
	// ClusterRefList will point to the clusters that the postApplyConfig yamls successfully applied.
	// +optional
	ClusterRefList []*corev1.ObjectReference `json:"clusterRefList,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=postapplyconfigs,scope=Namespaced,categories=cluster-api
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// ClusterResourceSet is the Schema for the postapplyconfigs API
type ClusterResourceSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClusterResourceSetSpec `json:"spec,omitempty"`
	Status PostApplyConfigStatus  `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// PostApplyConfigList contains a list of ClusterResourceSet
type PostApplyConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourceSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterResourceSet{}, &PostApplyConfigList{})
}
