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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ANCHOR: ClusterResourceSetResourceStatus

// ClusterResourceSetResourceStatus keeps status of a resource that is applied to the cluster that ClusterResourceSetBinding belongs to.
type ClusterResourceSetResourceStatus struct {
	// Hash is the hash of a resource's data. This can be used to decide if a resource is changed.
	// For "ApplyOnce" ClusterResourceSet.spec.mode, this is no-op as that mode do not act on change.
	Hash string `json:"hash,omitempty"`
	// LastAppliedTime identifies when this resource was last applied to the cluster.
	// +optional
	LastAppliedTime *metav1.Time `json:"lastAppliedTime,omitempty"`
	// Successful is to track if a resource is successfully applied to the cluster or not.
	Successful bool `json:"successful,omitempty"`
}

// ANCHOR_END: ClusterResourceSetResourceStatus

// ResourcesInClusterResourceSet keeps info on all of the resources in a ClusterResourceSet.
type ResourcesInClusterResourceSet struct {
	// Resources is a map of Secrets/ConfigMaps and their ClusterResourceSetResourceStatus.
	// The map's key is a concatenated string of form: <resource-type>/<resource-name>.
	Resources map[string]ClusterResourceSetResourceStatus `json:"resources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=clusterresourcesetbindings,scope=Namespaced,categories=cluster-api
// +kubebuilder:subresource:status
// +kubebuilder:storageversion

// ClusterResourceSetBinding lists all matching ClusterResourceSets with the cluster it belongs to.
type ClusterResourceSetBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// ClusterResourceSetMap is a map of ClusterResourceSet and its resources which is also a map.
	ClusterResourceSetMap map[string]ResourcesInClusterResourceSet `json:"clusterresourcesetmap,omitempty"`
}

// +kubebuilder:object:root=true

// ClusterResourceSetBindingList contains a list of ClusterResourceSetBinding
type ClusterResourceSetBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterResourceSetBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClusterResourceSetBinding{}, &ClusterResourceSetBindingList{})
}
