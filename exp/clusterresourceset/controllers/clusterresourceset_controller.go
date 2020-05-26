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
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/controllers/remote"
	clusterresourcesetv1 "sigs.k8s.io/cluster-api/exp/clusterresourceset/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ClusterResourceSetReconciler reconciles a ClusterResourceSet object
type ClusterResourceSetReconciler struct {
	Client client.Client
	Log    logr.Logger

	config   *rest.Config
	scheme   *runtime.Scheme
	recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=core,resources=events,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=clusterresourceset.cluster.x-k8s.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=clusterresourceset.cluster.x-k8s.io,resources=clusterresourcesets/status,verbs=get;update;patch

func (r *ClusterResourceSetReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
	ctx := context.Background()

	// Fetch the ClusterResourceSet instance.
	clusterResourceSet := &clusterresourcesetv1.ClusterResourceSet{}
	if err := r.Client.Get(ctx, req.NamespacedName, clusterResourceSet); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(clusterResourceSet, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		// Always attempt to Patch the ClusterResourceSet object and status after each reconciliation.
		if err := patchHelper.Patch(ctx, clusterResourceSet); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle normal reconciliation loop.
	return r.reconcile(ctx, clusterResourceSet)
}

// reconcile handles cluster reconciliation.
func (r *ClusterResourceSetReconciler) reconcile(ctx context.Context, clusterResourceSet *clusterresourcesetv1.ClusterResourceSet) (ctrl.Result, error) {
	logger := r.Log.WithValues("ClusterResourceSet", clusterResourceSet.Name, "namespace", clusterResourceSet.Namespace)

	cls, err := r.getClustersFromClusterResourceSet(ctx, clusterResourceSet)
	if err != nil {
		logger.Error(err, "Failed fetching clusters that matches ClusterResourceSet labels", "ClusterResourceSet", clusterResourceSet.Name)
		return ctrl.Result{}, err
	}

	for _, cl := range cls {
		if err := r.ApplyClusterResourceSet(ctx, cl, clusterResourceSet); err != nil {
			logger.Error(err, "Failed applying resources to cluster", "Cluster", cl.Name)
		}
	}

	return ctrl.Result{}, nil
}

// getClustersFromClusterResourceSet fetches Clusters matched by the ClusterResourceSet's label selector that are in the same namespace as the ClusterResourceSet object.
func (r *ClusterResourceSetReconciler) getClustersFromClusterResourceSet(ctx context.Context, clusterResourceSet *clusterresourcesetv1.ClusterResourceSet) ([]*clusterv1.Cluster, error) {
	clusterList := &clusterv1.ClusterList{}
	if err := r.Client.List(ctx, clusterList, client.InNamespace(clusterResourceSet.Namespace), client.MatchingLabels(clusterResourceSet.Spec.ClusterSelector.MatchLabels)); err != nil {
		return nil, errors.Wrap(err, "failed to list clusters")
	}

	clusters := []*clusterv1.Cluster{}
	for i := range clusterList.Items {
		m := &clusterList.Items[i]
		if m.DeletionTimestamp.IsZero() {
			clusters = append(clusters, m)
		}
	}
	return clusters, nil
}

func (r *ClusterResourceSetReconciler) ApplyClusterResourceSet(ctx context.Context, cluster *clusterv1.Cluster, clusterResourceSet *clusterresourcesetv1.ClusterResourceSet) error {
	logger := r.Log.WithValues("ClusterResourceSet", clusterResourceSet.Name, "namespace", clusterResourceSet.Namespace)

	logger.Info("Applying ClusterResourceSet to cluster", "Cluster", cluster.Name)

	err := r.applyClusterResourceSet(ctx, cluster, clusterResourceSet)
	if err != nil {
		logger.Error(err, "Failed to apply resources")
		return err
	}

	logger.Info("Successfully applied ClusterResourceSet", "ClusterResourceSet", clusterResourceSet.Name+"/"+clusterResourceSet.Namespace)

	return nil
}

// GetConfigMapFromNamespacedName retrieves any ConfigMap from the given name and namespace.
func GetConfigMapFromNamespacedName(ctx context.Context, c client.Client, secretName types.NamespacedName) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{}
	configMapKey := client.ObjectKey{
		Namespace: secretName.Namespace,
		Name:      secretName.Name,
	}
	if err := c.Get(ctx, configMapKey, configMap); err != nil {
		return nil, err
	}

	return configMap, nil
}

// applyClusterResourceSet applies resources in a ClusterResourceSet to a Cluster. Once applied, a record will be added to the
// cluster's ClusterResourceSetBinding.
// In ApplyOnce mode, resources are applied only once to a particular cluster. ClusterResourceSetBinding is used to check if a resource is applied before.
// TODO: If a resource already exists in the cluster but not applied by ClusterResourceSet, the resource will be updated ?
func (r *ClusterResourceSetReconciler) applyClusterResourceSet(ctx context.Context, cluster *clusterv1.Cluster, clusterResourceSet *clusterresourcesetv1.ClusterResourceSet) error {
	logger := r.Log.WithValues("ClusterResourceSet", clusterResourceSet.Name, "namespace", clusterResourceSet.Namespace)

	c, err := remote.NewClusterClient(context.Background(), r.Client, client.ObjectKey{Namespace: cluster.Namespace, Name: cluster.Name}, r.scheme)
	if err != nil {
		return err
	}

	// Get ClusterResourceSetBinding object for the cluster.
	clusterResourceSetBinding, err := GetorCreateClusterResourceSetBinding(ctx, r.Client, cluster)
	if err != nil {
		return err
	}

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(clusterResourceSetBinding, r.Client)
	if err != nil {
		return err
	}

	defer func() {
		// Always attempt to Patch the ClusterResourceSetBinding object after each reconciliation.
		if err := patchHelper.Patch(ctx, clusterResourceSetBinding); err != nil {
			r.Log.Error(err, "failed to patch config")
		}
	}()

	// Initializes the maps in ClusterResourceSetBinding object if they are uninitialized.
	InitClusterResourceSetBinding(clusterResourceSetBinding, clusterResourceSet)

	var clusterResourceSetInMap clusterresourcesetv1.ResourcesInClusterResourceSet
	var ok bool
	if clusterResourceSetInMap, ok = clusterResourceSetBinding.ClusterResourceSetMap[clusterResourceSet.Name]; !ok {
		// This should never be the case as InitClusterResourceSetBinding is adding the ClusterResourceSet to the map during initialization.
		return errors.New("failed to find ClusterResourceSet " + clusterResourceSet.Name + " in ClusterResourceBinding")
	}

	// Iterate all resources and apply them to the cluster and update the resource status in the ClusterResourceSetBinding object.
	for _, resource := range clusterResourceSet.Spec.Resources {
		// If resource is already applied successfully and clusterResourceSet mod is "ApplyOnce", continue. (No need to check hash changes here)
		resourceInMap := clusterresourcesetv1.ClusterResourceSetResourceStatus{}
		if resourceInMap, ok = clusterResourceSetInMap.Resources[GenerateResourceKindName(resource)]; ok {
			if clusterResourceSet.Spec.Mode == string(clusterresourcesetv1.ClusterResourceSetModeApplyOnce) && resourceInMap.Successful {
				continue
			}
		}

		// Only allow using resources (Secrets/Configmaps) in the same namespace with the cluster.
		typedName := types.NamespacedName{Name: resource.Name, Namespace: cluster.Namespace}
		dataList := make([][]byte, 0)

		// Retrieve data in the resource as an array because there can be many key-value pairs in resource and all will be applied to the cluster.
		switch resource.Kind {
		case string(clusterresourcesetv1.ConfigMapClusterResourceSetResourceKind):
			resourceConfigMap, err := GetConfigMapFromNamespacedName(context.Background(), r.Client, typedName)
			if err != nil {
				return errors.Wrapf(err,
					"failed to fetch ClusterResourceSet configmap %q in namespace %q", resource.Name, clusterResourceSet.Namespace)
			}

			for _, dataStr := range resourceConfigMap.Data {
				data := []byte(dataStr)
				dataList = append(dataList, data)
			}
		case string(clusterresourcesetv1.SecretClusterResourceSetResourceKind):
			resourceSecret, err := secret.GetAnySecretFromNamespacedName(context.Background(), r.Client, typedName)
			if err != nil {
				return errors.Wrapf(err,
					"failed to fetch ClusterResourceSet secret %q in namespace %q", resource.Name, clusterResourceSet.Namespace)
			}

			if resourceSecret.Type != clusterresourcesetv1.ClusterResourceSetSecretType {
				return errors.Wrapf(err,
					"Unsupported ClusterResourceSet secret resource: %q/%q type: %q", clusterResourceSet.Namespace, resource.Name, resourceSecret.Type)
			}
			for _, dataStr := range resourceSecret.Data {
				dataList = append(dataList, dataStr)
			}

		default:
			return errors.New("resource kind is not supported")
		}

		// Apply all values in the key-value pair of the resource to the cluster.
		// As there can be multiple key-value pairs in a resource, each value may have multiple objects in it.
		isSuccessful := true
		for i := range dataList {
			data := dataList[i]

			if err := Apply(ctx, c, data); err != nil {
				isSuccessful = false
				logger.Error(err,
					"failed to apply ClusterResourceSet resource %q/%q in namespace %q", resource.Kind, resource.Name, clusterResourceSet.Namespace)
			}
		}

		resourceInMap = *GenerateResourceStatus(isSuccessful, ComputeHash(dataList))
		// or clusterResourceSetBinding.ClusterResourceSetMap[clusterResourceSet.Name].Resources[GenerateResourceKindName(resource)] = *GenerateResourceStatus(isSuccessful, ComputeHash(dataList))
		clusterResourceSetInMap.Resources[GenerateResourceKindName(resource)] = resourceInMap

	}
	return nil
}

// GenerateResourceStatus returns ClusterResourceSetResourceStatus to  be used in ClusterResourceSetBinding resource of the matching cluster
func GenerateResourceStatus(isSuccessful bool, hash string) *clusterresourcesetv1.ClusterResourceSetResourceStatus {
	return &clusterresourcesetv1.ClusterResourceSetResourceStatus{
		Hash:            hash,
		Successful:      isSuccessful,
		LastAppliedTime: &metav1.Time{Time: time.Now().UTC()},
	}
}

// GenerateResourceKindName generates a unique name to identify resources that is used in resources map in ClusterResourceSetBinding.
func GenerateResourceKindName(resource *clusterresourcesetv1.ClusterResourceSetResource) string {
	if resource == nil {
		return ""
	}
	return resource.Kind + "/" + resource.Name
}

func ComputeHash(dataArr [][]byte) string {
	hasher := sha256.New()
	for i := range dataArr {
		if _, err := hasher.Write(dataArr[i]); err != nil {
			continue
		}
	}
	return base64.URLEncoding.EncodeToString(hasher.Sum(nil))
}

// clusterToClusterResourceSet is mapper function that maps clusters to ClusterResourceSet
func (r *ClusterResourceSetReconciler) clusterToClusterResourceSet(o handler.MapObject) []ctrl.Request {
	result := []ctrl.Request{}

	cluster, ok := o.Object.(*clusterv1.Cluster)
	if !ok {
		r.Log.Error(nil, fmt.Sprintf("Expected a Cluster but got a %T", o.Object))
		return nil
	}

	resourceList := &clusterresourcesetv1.ClusterResourceSetList{}
	if err := r.Client.List(context.Background(), resourceList, client.MatchingLabels(cluster.GetLabels())); err != nil {
		r.Log.Error(err, "failed to list ClusterResourceSet")
		return nil
	}

	clusterResourceSets := make([]*clusterresourcesetv1.ClusterResourceSet, 0)
	for i := range resourceList.Items {
		m := &resourceList.Items[i]
		if m.DeletionTimestamp.IsZero() {
			clusterResourceSets = append(clusterResourceSets, m)
		}
	}

	for _, pa := range clusterResourceSets {
		name := client.ObjectKey{Namespace: pa.Namespace, Name: pa.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}

	return result
}

func (r *ClusterResourceSetReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	c, err := ctrl.NewControllerManagedBy(mgr).
		For(&clusterresourcesetv1.ClusterResourceSet{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPaused(r.Log)).
		Build(r)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}

	err = c.Watch(
		&source.Kind{Type: &clusterv1.Cluster{}},
		&handler.EnqueueRequestsFromMapFunc{
			ToRequests: handler.ToRequestsFunc(r.clusterToClusterResourceSet),
		},
		// TODO: should this wait for Cluster.Status.InfrastructureReady similar to Infra Machine resources?
		predicates.ClusterUnpaused(r.Log),
	)
	if err != nil {
		return errors.Wrap(err, "failed adding Watch for Cluster to ClusterResourceSet controller manager")
	}

	r.config = mgr.GetConfig()
	r.scheme = mgr.GetScheme()
	r.recorder = mgr.GetEventRecorderFor("clusterresourceset-controller")
	return nil
}
