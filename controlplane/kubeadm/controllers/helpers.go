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
	"context"
	"encoding/json"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"reflect"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal/machinefilters"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/storage/names"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	bootstrapv1 "sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1alpha3"
	"sigs.k8s.io/cluster-api/controllers/external"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal/hash"
	capierrors "sigs.k8s.io/cluster-api/errors"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/certs"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *KubeadmControlPlaneReconciler) reconcileKubeconfig(ctx context.Context, clusterName client.ObjectKey, endpoint clusterv1.APIEndpoint, kcp *controlplanev1.KubeadmControlPlane) error {
	if endpoint.IsZero() {
		return nil
	}

	controllerOwnerRef := *metav1.NewControllerRef(kcp, controlplanev1.GroupVersion.WithKind("KubeadmControlPlane"))
	configSecret, err := secret.GetFromNamespacedName(ctx, r.Client, clusterName, secret.Kubeconfig)
	switch {
	case apierrors.IsNotFound(err):
		createErr := kubeconfig.CreateSecretWithOwner(
			ctx,
			r.Client,
			clusterName,
			endpoint.String(),
			controllerOwnerRef,
		)
		if errors.Is(createErr, kubeconfig.ErrDependentCertificateNotFound) {
			return errors.Wrapf(&capierrors.RequeueAfterError{RequeueAfter: dependentCertRequeueAfter},
				"could not find secret %q, requeuing", secret.ClusterCA)
		}
		// always return if we have just created in order to skip rotation checks
		return createErr
	case err != nil:
		return errors.Wrap(err, "failed to retrieve kubeconfig Secret")
	}

	// only do rotation on owned secrets
	if !util.IsControlledBy(configSecret, kcp) {
		return nil
	}

	needsRotation, err := kubeconfig.NeedsClientCertRotation(configSecret, certs.ClientCertificateRenewalDuration)
	if err != nil {
		return err
	}

	if needsRotation {
		r.Log.Info("rotating kubeconfig secret")
		if err := kubeconfig.RegenerateSecret(ctx, r.Client, configSecret); err != nil {
			return errors.Wrap(err, "failed to regenerate kubeconfig")
		}
	}

	return nil
}

func (r *KubeadmControlPlaneReconciler) reconcileExternalReference(ctx context.Context, cluster *clusterv1.Cluster, ref corev1.ObjectReference) error {
	if !strings.HasSuffix(ref.Kind, external.TemplateSuffix) {
		return nil
	}

	obj, err := external.Get(ctx, r.Client, &ref, cluster.Namespace)
	if err != nil {
		return err
	}

	// Note: We intentionally do not handle checking for the paused label on an external template reference

	patchHelper, err := patch.NewHelper(obj, r.Client)
	if err != nil {
		return err
	}

	obj.SetOwnerReferences(util.EnsureOwnerRef(obj.GetOwnerReferences(), metav1.OwnerReference{
		APIVersion: clusterv1.GroupVersion.String(),
		Kind:       "Cluster",
		Name:       cluster.Name,
		UID:        cluster.UID,
	}))

	return patchHelper.Patch(ctx, obj)
}

func (r *KubeadmControlPlaneReconciler) cloneConfigsAndGenerateMachine(ctx context.Context, cluster *clusterv1.Cluster, kcp *controlplanev1.KubeadmControlPlane, bootstrapSpec *bootstrapv1.KubeadmConfigSpec, failureDomain *string) error {
	var errs []error

	// Since the cloned resource should eventually have a controller ref for the Machine, we create an
	// OwnerReference here without the Controller field set
	infraCloneOwner := &metav1.OwnerReference{
		APIVersion: controlplanev1.GroupVersion.String(),
		Kind:       "KubeadmControlPlane",
		Name:       kcp.Name,
		UID:        kcp.UID,
	}

	// Clone the infrastructure template
	infraRef, err := external.CloneTemplate(ctx, &external.CloneTemplateInput{
		Client:      r.Client,
		TemplateRef: &kcp.Spec.InfrastructureTemplate,
		Namespace:   kcp.Namespace,
		OwnerRef:    infraCloneOwner,
		ClusterName: cluster.Name,
		Labels:      internal.ControlPlaneLabelsForClusterWithHash(cluster.Name, hash.Compute(&kcp.Spec)),
	})
	if err != nil {
		// Safe to return early here since no resources have been created yet.
		return errors.Wrap(err, "failed to clone infrastructure template")
	}

	// Clone the bootstrap configuration
	bootstrapRef, err := r.generateKubeadmConfig(ctx, kcp, cluster, bootstrapSpec)
	if err != nil {
		errs = append(errs, errors.Wrap(err, "failed to generate bootstrap config"))
	}

	// Only proceed to generating the Machine if we haven't encountered an error
	if len(errs) == 0 {
		if err := r.generateMachine(ctx, kcp, cluster, infraRef, bootstrapRef, failureDomain); err != nil {
			errs = append(errs, errors.Wrap(err, "failed to create Machine"))
		}
	}

	// If we encountered any errors, attempt to clean up any dangling resources
	if len(errs) > 0 {
		if err := r.cleanupFromGeneration(ctx, infraRef, bootstrapRef); err != nil {
			errs = append(errs, errors.Wrap(err, "failed to cleanup generated resources"))
		}

		return kerrors.NewAggregate(errs)
	}

	return nil
}

func (r *KubeadmControlPlaneReconciler) cleanupFromGeneration(ctx context.Context, remoteRefs ...*corev1.ObjectReference) error {
	var errs []error

	for _, ref := range remoteRefs {
		if ref != nil {
			config := &unstructured.Unstructured{}
			config.SetKind(ref.Kind)
			config.SetAPIVersion(ref.APIVersion)
			config.SetNamespace(ref.Namespace)
			config.SetName(ref.Name)

			if err := r.Client.Delete(ctx, config); err != nil && !apierrors.IsNotFound(err) {
				errs = append(errs, errors.Wrap(err, "failed to cleanup generated resources after error"))
			}
		}
	}

	return kerrors.NewAggregate(errs)
}

func (r *KubeadmControlPlaneReconciler) generateKubeadmConfig(ctx context.Context, kcp *controlplanev1.KubeadmControlPlane, cluster *clusterv1.Cluster, spec *bootstrapv1.KubeadmConfigSpec) (*corev1.ObjectReference, error) {
	// Create an owner reference without a controller reference because the owning controller is the machine controller
	owner := metav1.OwnerReference{
		APIVersion: controlplanev1.GroupVersion.String(),
		Kind:       "KubeadmControlPlane",
		Name:       kcp.Name,
		UID:        kcp.UID,
	}

	bootstrapConfig := &bootstrapv1.KubeadmConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:            names.SimpleNameGenerator.GenerateName(kcp.Name + "-"),
			Namespace:       kcp.Namespace,
			Labels:          internal.ControlPlaneLabelsForClusterWithHash(cluster.Name, hash.Compute(&kcp.Spec)),
			OwnerReferences: []metav1.OwnerReference{owner},
		},
		Spec: *spec,
	}

	if err := r.Client.Create(ctx, bootstrapConfig); err != nil {
		return nil, errors.Wrap(err, "Failed to create bootstrap configuration")
	}

	bootstrapRef := &corev1.ObjectReference{
		APIVersion: bootstrapv1.GroupVersion.String(),
		Kind:       "KubeadmConfig",
		Name:       bootstrapConfig.GetName(),
		Namespace:  bootstrapConfig.GetNamespace(),
		UID:        bootstrapConfig.GetUID(),
	}

	return bootstrapRef, nil
}

func (r *KubeadmControlPlaneReconciler) generateMachine(ctx context.Context, kcp *controlplanev1.KubeadmControlPlane, cluster *clusterv1.Cluster, infraRef, bootstrapRef *corev1.ObjectReference, failureDomain *string) error {
	machine := &clusterv1.Machine{
		ObjectMeta: metav1.ObjectMeta{
			Name:      names.SimpleNameGenerator.GenerateName(kcp.Name + "-"),
			Namespace: kcp.Namespace,
			Labels:    internal.ControlPlaneLabelsForClusterWithHash(cluster.Name, hash.Compute(&kcp.Spec)),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(kcp, controlplanev1.GroupVersion.WithKind("KubeadmControlPlane")),
			},
		},
		Spec: clusterv1.MachineSpec{
			ClusterName:       cluster.Name,
			Version:           &kcp.Spec.Version,
			InfrastructureRef: *infraRef,
			Bootstrap: clusterv1.Bootstrap{
				ConfigRef: bootstrapRef,
			},
			FailureDomain: failureDomain,
		},
	}

	if err := r.Client.Create(ctx, machine); err != nil {
		return errors.Wrap(err, "Failed to create machine")
	}
	return nil
}

// TODO: deletiontimestamp machine
// add comments when continue
// MachinesNeedingUpgrade return a list of machines that need to be upgraded.
func (r *KubeadmControlPlaneReconciler) MachinesNeedingUpgrade(ctx context.Context, c *internal.ControlPlane) internal.FilterableMachineCollection {
	machinesToUpgrade := internal.NewFilterableMachineCollection()

	now := metav1.Now()
	if c.KCP.Spec.UpgradeAfter != nil && c.KCP.Spec.UpgradeAfter.Before(&now) {
		oldMachines := c.Machines.Filter(machinefilters.OlderThan(c.KCP.Spec.UpgradeAfter))
		// Ignore deleted machines.
		oldMachines = oldMachines.Filter(func(machine *clusterv1.Machine) bool {
			return machine.DeletionTimestamp.IsZero()
		})
		for _, m := range oldMachines {
			machinesToUpgrade.Insert(m)
		}
	}

	for _, machine := range c.Machines {
		if !machine.DeletionTimestamp.IsZero() {
			continue
		}

		if *machine.Spec.Version != c.KCP.Spec.Version {
			machinesToUpgrade.Insert(machine)
			continue
		}

		ref := machine.Spec.Bootstrap.ConfigRef
		if ref == nil {
			continue
		}

		machineCfg := &bootstrapv1.KubeadmConfig{}
		if err := r.Client.Get(ctx, client.ObjectKey{Name: ref.Name, Namespace: c.KCP.Namespace}, machineCfg); err != nil {
			continue
		}

		kcpSpec := createKcpKubeadmConfig(machineCfg.Spec, c.KCP.Spec.KubeadmConfigSpec)

		if !reflect.DeepEqual(machineCfg.Spec, kcpSpec) {
			machinesToUpgrade.Insert(machine)
			continue
		}

		infraObj, err := external.Get(ctx, r.Client, &machine.Spec.InfrastructureRef, machine.Spec.InfrastructureRef.Namespace)
		if err != nil {
			//
			continue
		}

		template, exist := infraObj.GetAnnotations()[clusterv1.TemplateClonedFromAnnotation]
		if !exist {
			// All KCP owned machines should have this annotation as the ones that do not have it, will be annotated during
			// ReconcileInfrastructureMachineRefs(), which is called before entering this method.
			// Missing the annotation here may be due to a manual intervention, in which case in the next reconcile the machine will be annotated.
			continue
		}

		infraRef := &corev1.ObjectReference{}
		if err := json.Unmarshal([]byte(template), infraRef); err != nil {
			// If infra template ref is corrupt, it needs upgrade.
			machinesToUpgrade.Insert(machine)
			continue
		}
		if !referSameObject(*infraRef, c.KCP.Spec.InfrastructureTemplate) {
			machinesToUpgrade.Insert(machine)
			continue
		}
	}

	return machinesToUpgrade
}

// TODO: do we accept machines missing all 3 configs - init, join, and cluster?
// TODO: if init config is there, should cluster config be non-nil?
// createKcpKubeadmConfig returns a KubeadmConfigSpec that only has init/join/cluster configs that machine also has a non-nil value for.
func createKcpKubeadmConfig(machineConfig bootstrapv1.KubeadmConfigSpec, kcpConfig bootstrapv1.KubeadmConfigSpec) bootstrapv1.KubeadmConfigSpec {
	if machineConfig.InitConfiguration == nil {
		kcpConfig.InitConfiguration = nil
	}
	if machineConfig.JoinConfiguration == nil {
		kcpConfig.JoinConfiguration = nil
	}
	if machineConfig.ClusterConfiguration == nil {
		kcpConfig.ClusterConfiguration = nil
	}
	return kcpConfig
}

// Returns true if a and b point to the same object.
func referSameObject(a, b corev1.ObjectReference) bool {
	aGV, err := schema.ParseGroupVersion(a.APIVersion)
	if err != nil {
		return false
	}

	bGV, err := schema.ParseGroupVersion(b.APIVersion)
	if err != nil {
		return false
	}

	return aGV.Group == bGV.Group && a.Kind == b.Kind && a.Name == b.Name && a.Namespace == b.Namespace
}
