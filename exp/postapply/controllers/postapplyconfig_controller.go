/*

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
	"fmt"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/controllers/remote"
	postapplyv1 "sigs.k8s.io/cluster-api/exp/postapply/api/v1alpha3"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/secret"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// PostApplyDataKey is the data key in post-apply-addon secrets
var PostApplyDataKey = "addon.yaml"

// PostApplyConfigReconciler reconciles a ClusterResourceSet object
type PostApplyConfigReconciler struct {
	client.Client
	Log      logr.Logger
	Scheme   *runtime.Scheme
	recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=postapply.cluster.x-k8s.io,resources=postapplyconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=postapply.cluster.x-k8s.io,resources=postapplyconfigs/status,verbs=get;update;patch

func (r *PostApplyConfigReconciler) Reconcile(req ctrl.Request) (_ ctrl.Result, reterr error) {
	ctx := context.Background()

	// Fetch the ClusterResourceSet instance.
	postApplyConf := &postapplyv1.PostApplyConfig{}
	if err := r.Client.Get(ctx, req.NamespacedName, postApplyConf); err != nil {
		if apierrors.IsNotFound(err) {
			// Object not found, return.  Created objects are automatically garbage collected.
			// For additional cleanup logic use finalizers.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Initialize the patch helper.
	patchHelper, err := patch.NewHelper(postApplyConf, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}

	defer func() {
		// Always attempt to Patch the ClusterResourceSet object and status after each reconciliation.
		if err := patchHelper.Patch(ctx, postApplyConf); err != nil {
			reterr = kerrors.NewAggregate([]error{reterr, err})
		}
	}()

	// Handle normal reconciliation loop.
	return r.reconcile(ctx, postApplyConf)
}

// reconcile handles cluster reconciliation.
func (r *PostApplyConfigReconciler) reconcile(ctx context.Context, postApplyConf *postapplyv1.PostApplyConfig) (ctrl.Result, error) {
	logger := r.Log.WithValues("postapplyconfig", postApplyConf.Name, "namespace", postApplyConf.Namespace)

	logger.Info("debug: in ClusterResourceSet Reconcile")
	cls, err := r.getMatchingClustersForPostApply(ctx, postApplyConf.Spec.ClusterSelector.MatchLabels)
	if err != nil {
		logger.Error(err, "Failed fetching clusters that matches ClusterResourceSet labels", "ClusterResourceSet", postApplyConf.Name)
		return ctrl.Result{}, err
	}

	for _, cl := range cls {
		logger.Info("debug: Applying addons to clusters", "cluster", cl.Name)

		r.PostApplyToCluster(cl, postApplyConf)
	}

	return ctrl.Result{}, nil
}

// getMatchingClustersForPostApply returns all of the Cluster objects
// that have a matching label with the postApplyConfig object
func (r *PostApplyConfigReconciler) getMatchingClustersForPostApply(ctx context.Context, postApplyLabels map[string]string) ([]*clusterv1.Cluster, error) {
	clusterList := &clusterv1.ClusterList{}
	if err := r.Client.List(ctx, clusterList, client.MatchingLabels(postApplyLabels)); err != nil {
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

func (r *PostApplyConfigReconciler) PostApplyToCluster(cluster *clusterv1.Cluster, postApplyConf *postapplyv1.PostApplyConfig) error {
	logger := r.Log.WithValues("ClusterResourceSet", postApplyConf.Name, "namespace", postApplyConf.Namespace)

	logger.Info("debug: Applying ClusterResourceSet to cluster", "Cluster", cluster.Name, "ClusterResourceSet", postApplyConf.Name)

	// Check if this postApplyConf is applied to the cluster, if not continue
	if r.IsYamlAppliedToCluster(cluster, postApplyConf.Status.ClusterRefList) {
		logger.Info("debug: "+
			" applied before", "Cluster", cluster.Name, "ClusterResourceSet", postApplyConf.Name)
		return nil
	}

	// Check if all secrets exist and in correct format. If not, return and do not apply any
	err := r.checkSecretsAreCorrect(postApplyConf)
	if err != nil {
		r.Log.Error(err, "error in PostApply secrets")
		return err
	}

	err = r.postApplyToCluster(cluster, postApplyConf)
	if err != nil {
		r.Log.Error(err, "Failed applying secrets")
		return err
	}

	logger.Info("debug: Successfully applied post-apply addon", "ClusterResourceSet", postApplyConf.Name+"/"+postApplyConf.Namespace)

	if postApplyConf.Status.ClusterRefList == nil {
		postApplyConf.Status.ClusterRefList = make([]*corev1.ObjectReference, 0)
	}

	postApplyConf.Status.ClusterRefList = append(postApplyConf.Status.ClusterRefList, &corev1.ObjectReference{
		Kind:      cluster.Kind,
		Name:      cluster.Name,
		Namespace: cluster.Namespace,
		UID:       cluster.UID,
	})
	return nil
}

func (r *PostApplyConfigReconciler) IsYamlAppliedToCluster(cluster *clusterv1.Cluster, refList []*corev1.ObjectReference) bool {
	for _, ref := range refList {
		// TODO: Is this check enough?
		if ref.Name == cluster.Name && ref.Namespace == cluster.Namespace && ref.Kind == cluster.Kind {
			return true
		}
	}

	return false
}

func (r *PostApplyConfigReconciler) postApplyToCluster(cluster *clusterv1.Cluster, postApplyConf *postapplyv1.PostApplyConfig) error {
	c, err := remote.NewClusterClient(context.Background(), r.Client, client.ObjectKey{cluster.Namespace, cluster.Name}, r.Scheme)
	// Failed to get remote cluster client: Kubeconfig secret may be missing for the cluster.
	if err != nil {
		return err
	}

	for _, addon := range postApplyConf.Spec.PostApplyAddons {
		typedName := types.NamespacedName{Name: addon.Name, Namespace: addon.Namespace}
		addonSecret, err := secret.GetAnySecretFromNamespacedName(context.Background(), r.Client, typedName)
		if err != nil {
			return errors.Wrapf(err,
				"failed to fetch PostApply secret %q in namespace %q", addon.Name, addon.Namespace)
		}

		data, ok := addonSecret.Data[PostApplyDataKey]
		if !ok {
			return errors.New("wrong secret format for addon")
		}
		err = ApplyYAMLWithNamespace(context.Background(), c, data, "")
		if err != nil {
			return errors.Wrapf(err,
				"failed to applying PostApply secret %q in namespace %q", addon.Name, addon.Namespace)
		}
	}
	return nil
}

func (r *PostApplyConfigReconciler) checkSecretsAreCorrect(postApplyConf *postapplyv1.PostApplyConfig) error {
	for _, addon := range postApplyConf.Spec.PostApplyAddons {
		typedName := types.NamespacedName{Name: addon.Name, Namespace: addon.Namespace}
		addonSecret, err := secret.GetAnySecretFromNamespacedName(context.Background(), r.Client, typedName)
		if err != nil {
			return errors.Wrapf(err,
				"failed to fetch PostApply secret %q in namespace %q", addon.Name, addon.Namespace)
		}

		_, ok := addonSecret.Data[PostApplyDataKey]
		if !ok {
			return errors.New("wrong secret format for addon")
		}
	}

	return nil
}

func (r *PostApplyConfigReconciler) clusterToPostApplyConfig(o handler.MapObject) []ctrl.Request {
	result := []ctrl.Request{}

	cluster, ok := o.Object.(*clusterv1.Cluster)
	if !ok {
		r.Log.Error(nil, fmt.Sprintf("Expected a Cluster but got a %T", o.Object))
		return nil
	}

	postApplyConfigList := &postapplyv1.PostApplyConfigList{}
	if err := r.Client.List(context.Background(), postApplyConfigList, client.MatchingLabels(cluster.GetLabels())); err != nil {
		r.Log.Error(err, "failed to list PostApplyConfigs")
		return nil
	}

	postApplyConfigs := []*postapplyv1.PostApplyConfig{}
	for i := range postApplyConfigList.Items {
		m := &postApplyConfigList.Items[i]
		if m.DeletionTimestamp.IsZero() {
			postApplyConfigs = append(postApplyConfigs, m)
		}
	}

	for _, pa := range postApplyConfigs {
		name := client.ObjectKey{Namespace: pa.Namespace, Name: pa.Name}
		result = append(result, ctrl.Request{NamespacedName: name})
	}

	return result
}

func (r *PostApplyConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := ctrl.NewControllerManagedBy(mgr).
		For(&postapplyv1.PostApplyConfig{}).
		Watches(
			&source.Kind{Type: &clusterv1.Cluster{}},
			&handler.EnqueueRequestsFromMapFunc{ToRequests: handler.ToRequestsFunc(r.clusterToPostApplyConfig)},
		).
		Complete(r)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}
	r.recorder = mgr.GetEventRecorderFor("cluster-controller")
	return err
}
