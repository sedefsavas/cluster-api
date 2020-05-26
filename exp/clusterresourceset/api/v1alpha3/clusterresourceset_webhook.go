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
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

func (m *ClusterResourceSet) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(m).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-exp-cluster-x-k8s-io-v1alpha3-clusterresourceset,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=exp.cluster.x-k8s.io,resources=clusterresourcesets,versions=v1alpha3,name=validation.clusterresourceset.exp.cluster.x-k8s.io,sideEffects=None
// +kubebuilder:webhook:verbs=create;update,path=/mutate-exp-cluster-x-k8s-io-v1alpha3-clusterresourceset,mutating=true,failurePolicy=fail,matchPolicy=Equivalent,groups=exp.cluster.x-k8s.io,resources=clusterresourcesets,versions=v1alpha3,name=default.clusterresourceset.exp.cluster.x-k8s.io,sideEffects=None

var _ webhook.Defaulter = &ClusterResourceSet{}
var _ webhook.Validator = &ClusterResourceSet{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (m *ClusterResourceSet) Default() {
	// ClusterResourceSet Mode defaults to ApplyOnce.
	if m.Spec.Mode == "" {
		m.Spec.Mode = string(ClusterResourceSetModeApplyOnce)
	}
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (m *ClusterResourceSet) ValidateCreate() error {
	return m.validate(nil)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (m *ClusterResourceSet) ValidateUpdate(old runtime.Object) error {
	oldCRS, ok := old.(*ClusterResourceSet)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a ClusterResourceSet but got a %T", old))
	}
	return m.validate(oldCRS)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (m *ClusterResourceSet) ValidateDelete() error {
	return m.validate(nil)
}

func (m *ClusterResourceSet) validate(old *ClusterResourceSet) error {
	var allErrs field.ErrorList

	// Validate selector parses as Selector
	selector, err := metav1.LabelSelectorAsSelector(&m.Spec.ClusterSelector)
	if err != nil {
		allErrs = append(
			allErrs,
			field.Invalid(field.NewPath("spec", "clusterSelector"), m.Spec.ClusterSelector, err.Error()),
		)
	}

	// Validate that the selector isn't empty as null selectors do not select any objects.
	if selector != nil && selector.Empty() {
		allErrs = append(
			allErrs,
			field.Invalid(field.NewPath("spec", "selector"), m.Spec.ClusterSelector, "selector must not be empty"),
		)
	}

	// Only allow resources of kind ConfigMap or Secret
	for _, resource := range m.Spec.Resources {
		if !isSupportedResourceKind(resource.Kind) {
			allErrs = append(
				allErrs,
				field.NotSupported(field.NewPath("spec", "resources"), m.Spec.Resources, []string{string(SecretClusterResourceSetResourceKind), string(ConfigMapClusterResourceSetResourceKind)}),
			)
		}
	}

	if old != nil && old.Spec.Mode != m.Spec.Mode {
		allErrs = append(
			allErrs,
			field.Invalid(field.NewPath("spec", "mode"), m.Spec.Mode, "field is immutable"),
		)
	}

	// TODO: Any changes to clusterSelector is not allowed ?
	if old != nil && reflect.DeepEqual(old.Spec.ClusterSelector, m.Spec.ClusterSelector) {
		allErrs = append(
			allErrs,
			field.Invalid(field.NewPath("spec", "clusterSelector"), m.Spec.ClusterSelector, "field is immutable"),
		)
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(GroupVersion.WithKind("ClusterResourceSet").GroupKind(), m.Name, allErrs)
}

// isSupportedResourceKind checks if a resource kind is supported.
func isSupportedResourceKind(resourceKind string) bool {
	switch crsk := ClusterResourceSetResourceKind(resourceKind); crsk {
	case
		SecretClusterResourceSetResourceKind,
		ConfigMapClusterResourceSetResourceKind:
		return true
	default:
		return false
	}
}
