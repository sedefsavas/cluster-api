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

package internal

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"k8s.io/apimachinery/pkg/types"
	"math/big"
	controlplanev1 "sigs.k8s.io/cluster-api/controlplane/kubeadm/api/v1alpha3"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/controlplane/kubeadm/internal/machinefilters"
	"sigs.k8s.io/cluster-api/util/certs"
	"sigs.k8s.io/cluster-api/util/kubeconfig"
	"sigs.k8s.io/cluster-api/util/secret"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestCheckStaticPodReadyCondition(t *testing.T) {
	table := []checkStaticPodReadyConditionTest{
		{
			name:       "pod is ready",
			conditions: []corev1.PodCondition{podReady(corev1.ConditionTrue)},
		},
	}
	for _, test := range table {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec:   corev1.PodSpec{},
				Status: corev1.PodStatus{Conditions: test.conditions},
			}
			g.Expect(checkStaticPodReadyCondition(pod)).To(Succeed())
		})
	}
}

func TestCheckStaticPodNotReadyCondition(t *testing.T) {
	table := []checkStaticPodReadyConditionTest{
		{
			name: "no pod status",
		},
		{
			name:       "not ready pod status",
			conditions: []corev1.PodCondition{podReady(corev1.ConditionFalse)},
		},
	}
	for _, test := range table {
		t.Run(test.name, func(t *testing.T) {
			g := NewWithT(t)

			pod := corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pod",
				},
				Spec:   corev1.PodSpec{},
				Status: corev1.PodStatus{Conditions: test.conditions},
			}
			g.Expect(checkStaticPodReadyCondition(pod)).NotTo(Succeed())
		})
	}
}

func TestControlPlaneIsHealthy(t *testing.T) {
	machines := []*clusterv1.Machine{
		controlPlaneMachine("first-control-plane"),
		controlPlaneMachine("second-control-plane"),
		controlPlaneMachine("third-control-plane"),
	}

	singleMachine := []*clusterv1.Machine{
		controlPlaneMachine("single-control-plane"),
	}

	tests := []struct {
		name                 string
		workloadCluster      *Workload
		controlPlaneMachines []*clusterv1.Machine
		expectedConditions   []clusterv1.Conditions // only checks important ones, not all
		expectErr            bool
	}{
		{
			name:                 "all control plane pods are healthy in a HA control plane",
			controlPlaneMachines: machines,
			workloadCluster: &Workload{
				Client: &fakeClient{
					list: nodeListForMachines(machines),
					get: podsForFakeWorkloadGet([]podStatus{
						{KubeAPIServerPodNamePrefix, machines[0].Name, []podOption{withReadyOption}},
						{KubeAPIServerPodNamePrefix, machines[1].Name, []podOption{withReadyOption}},
						{KubeAPIServerPodNamePrefix, machines[2].Name, []podOption{withReadyOption}},

						{KubeControllerManagerPodNamePrefix, machines[0].Name, []podOption{withReadyOption}},
						{KubeControllerManagerPodNamePrefix, machines[1].Name, []podOption{withReadyOption}},
						{KubeControllerManagerPodNamePrefix, machines[2].Name, []podOption{withReadyOption}},

						{KubeSchedulerHealthyPodNamePrefix, machines[0].Name, []podOption{withReadyOption}},
						{KubeSchedulerHealthyPodNamePrefix, machines[1].Name, []podOption{withReadyOption}},
						{KubeSchedulerHealthyPodNamePrefix, machines[2].Name, []podOption{withReadyOption}},
					}...),
				},
			},
			expectedConditions: []clusterv1.Conditions{
				{
					{
						Type:   controlplanev1.MachineAPIServerPodHealthyCondition,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   controlplanev1.MachineControllerManagerPodHealthyCondition,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   controlplanev1.MachineSchedulerPodHealthyCondition,
						Status: corev1.ConditionTrue,
					},
				},
			},
			expectErr: false,
		},
		{
			name:                 "some control plane pods are missing in a HA control plane, should return error",
			controlPlaneMachines: machines,
			workloadCluster: &Workload{
				Client: &fakeClient{
					list: nodeListForMachines(machines),
					get: podsForFakeWorkloadGet([]podStatus{
						{KubeAPIServerPodNamePrefix, machines[0].Name, []podOption{withReadyOption}},
						{KubeControllerManagerPodNamePrefix, machines[0].Name, []podOption{withReadyOption}},
						{KubeSchedulerHealthyPodNamePrefix, machines[0].Name, []podOption{withReadyOption}},
					}...),
				},
			},
			expectedConditions: []clusterv1.Conditions{
				{
					{
						Type:   controlplanev1.MachineAPIServerPodHealthyCondition,
						Status: corev1.ConditionTrue,
					},
				},
				{
					{
						Type:   controlplanev1.MachineAPIServerPodHealthyCondition,
						Status: corev1.ConditionFalse,
						Reason: controlplanev1.PodMissingReason,
					},
				},
				{
					{
						Type:   controlplanev1.MachineAPIServerPodHealthyCondition,
						Status: corev1.ConditionFalse,
						Reason: controlplanev1.PodMissingReason,
					},
				},
			},
			expectErr: true,
		},
		{
			name:                 "A control plane pod is in failed state, should return error",
			controlPlaneMachines: singleMachine,
			workloadCluster: &Workload{
				Client: &fakeClient{
					list: nodeListForMachines(singleMachine),
					get: podsForFakeWorkloadGet([]podStatus{
						{KubeAPIServerPodNamePrefix, singleMachine[0].Name, []podOption{withFailedStatus}},
						{KubeControllerManagerPodNamePrefix, singleMachine[0].Name, []podOption{withReadyOption}},
					}...),
				},
			},
			expectedConditions: []clusterv1.Conditions{
				{
					{
						Type:   controlplanev1.MachineAPIServerPodHealthyCondition,
						Status: corev1.ConditionFalse,
						Reason: controlplanev1.PodFailedReason,
					},
					{
						Type:   controlplanev1.MachineControllerManagerPodHealthyCondition,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   controlplanev1.MachineSchedulerPodHealthyCondition,
						Status: corev1.ConditionFalse,
						Reason: controlplanev1.PodMissingReason,
					},
				},
			},
			expectErr: true,
		},
		{
			name:                 "1 control plane pod is in pending state with control plane not initialized yet",
			controlPlaneMachines: singleMachine,
			workloadCluster: &Workload{
				Client: &fakeClient{
					list: nodeListForMachines(singleMachine),
					get: podsForFakeWorkloadGet([]podStatus{
						{KubeAPIServerPodNamePrefix, singleMachine[0].Name, []podOption{withPendingStatus, withPodInitializedOption(corev1.ConditionFalse)}},
						{KubeControllerManagerPodNamePrefix, singleMachine[0].Name, []podOption{withReadyOption}},
					}...),
				},
			},
			expectedConditions: []clusterv1.Conditions{
				{
					{
						Type:   controlplanev1.MachineAPIServerPodHealthyCondition,
						Status: corev1.ConditionFalse,
						Reason: controlplanev1.PodProvisioningReason,
					},
					{
						Type:   controlplanev1.MachineControllerManagerPodHealthyCondition,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   controlplanev1.MachineSchedulerPodHealthyCondition,
						Status: corev1.ConditionFalse,
						Reason: controlplanev1.PodMissingReason,
					},
				},
			},
			expectErr: true,
		},
		{
			name:                 "1 control plane pod is in pending state but provisioned (not ready)",
			controlPlaneMachines: singleMachine,
			workloadCluster: &Workload{
				Client: &fakeClient{
					list: nodeListForMachines(singleMachine),
					get: podsForFakeWorkloadGet([]podStatus{
						{KubeAPIServerPodNamePrefix, singleMachine[0].Name, []podOption{withPendingStatus}},
						{KubeControllerManagerPodNamePrefix, singleMachine[0].Name, []podOption{withReadyOption}},
					}...),
				},
			},
			expectedConditions: []clusterv1.Conditions{
				{
					{
						Type:   controlplanev1.MachineAPIServerPodHealthyCondition,
						Status: corev1.ConditionFalse,
						Reason: controlplanev1.PodIsNotReadyReason,
					},
					{
						Type:   controlplanev1.MachineControllerManagerPodHealthyCondition,
						Status: corev1.ConditionTrue,
					},
					{
						Type:   controlplanev1.MachineSchedulerPodHealthyCondition,
						Status: corev1.ConditionFalse,
						Reason: controlplanev1.PodMissingReason,
					},
				},
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			err := tt.workloadCluster.ControlPlaneIsHealthy(context.Background(), tt.controlPlaneMachines)

			g.Expect(checkConditions(tt.controlPlaneMachines, tt.expectedConditions)).To(BeTrue())
			if tt.expectErr {
				g.Expect(err).To(HaveOccurred())
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
		})
	}
}

type podStatus struct {
	podPrefix string
	nodeName  string
	options   []podOption
}

func getPodName(pod podStatus) string {
	return types.NamespacedName{Namespace: metav1.NamespaceSystem, Name: staticPodName(pod.podPrefix, pod.nodeName)}.String()
}

// podsForFakeWorkloadGet returns map of pod namespace/name with pod
// pod namespace/name looks like: "kube-system/kube-controller-manager-second-control-plane"
func podsForFakeWorkloadGet(podStatuses ...podStatus) map[string]interface{} {
	pods := make(map[string]interface{})
	for _, podStatus := range podStatuses {
		pods[getPodName(podStatus)] = getPod("", podStatus.options...)
	}
	return pods
}

// checkConditions checks if machineConditions have all the expectedConditions
// machineConditions may have more conditions that expectedConditions.
func checkConditions(machines []*clusterv1.Machine, expectedConditions []clusterv1.Conditions) bool {
	for i := range expectedConditions {
		if !checkMachineConditions(machines[i], expectedConditions[i]) {
			return false
		}
	}
	return true
}

// checkMachineConditions returns false if it does not have all the expected conditions.
func checkMachineConditions(machine *clusterv1.Machine, expectedConditions clusterv1.Conditions) bool {
	for i := range expectedConditions {
		isFound := false
		for j := range machine.Status.Conditions {
			if machine.Status.Conditions[j].Type == expectedConditions[i].Type {
				isFound = true
				if machine.Status.Conditions[j].Status != expectedConditions[i].Status ||
					machine.Status.Conditions[j].Reason != expectedConditions[i].Reason {
					return false
				}
			}
		}

		if !isFound {
			return false
		}
	}
	return true
}

func TestGetMachinesForCluster(t *testing.T) {
	g := NewWithT(t)

	m := Management{Client: &fakeClient{
		list: machineListForTestGetMachinesForCluster(),
	}}
	clusterKey := client.ObjectKey{
		Namespace: "my-namespace",
		Name:      "my-cluster",
	}
	machines, err := m.GetMachinesForCluster(context.Background(), clusterKey)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(machines).To(HaveLen(3))

	// Test the ControlPlaneMachines works
	machines, err = m.GetMachinesForCluster(context.Background(), clusterKey, machinefilters.ControlPlaneMachines("my-cluster"))
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(machines).To(HaveLen(1))

	// Test that the filters use AND logic instead of OR logic
	nameFilter := func(cluster *clusterv1.Machine) bool {
		return cluster.Name == "first-machine"
	}
	machines, err = m.GetMachinesForCluster(context.Background(), clusterKey, machinefilters.ControlPlaneMachines("my-cluster"), nameFilter)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(machines).To(HaveLen(1))
}

func TestGetWorkloadCluster(t *testing.T) {
	g := NewWithT(t)

	ns, err := testEnv.CreateNamespace(ctx, "workload-cluster2")
	g.Expect(err).ToNot(HaveOccurred())
	defer func() {
		g.Expect(testEnv.Cleanup(ctx, ns)).To(Succeed())
	}()

	// Create an etcd secret with valid certs
	key, err := certs.NewPrivateKey()
	g.Expect(err).ToNot(HaveOccurred())
	cert, err := getTestCACert(key)
	g.Expect(err).ToNot(HaveOccurred())
	etcdSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster-etcd",
			Namespace: ns.Name,
		},
		Data: map[string][]byte{
			secret.TLSCrtDataName: certs.EncodeCertPEM(cert),
			secret.TLSKeyDataName: certs.EncodePrivateKeyPEM(key),
		},
	}
	emptyCrtEtcdSecret := etcdSecret.DeepCopy()
	delete(emptyCrtEtcdSecret.Data, secret.TLSCrtDataName)
	emptyKeyEtcdSecret := etcdSecret.DeepCopy()
	delete(emptyKeyEtcdSecret.Data, secret.TLSKeyDataName)
	badCrtEtcdSecret := etcdSecret.DeepCopy()
	badCrtEtcdSecret.Data[secret.TLSCrtDataName] = []byte("bad cert")

	// Create kubeconfig secret
	// Store the envtest config as the contents of the kubeconfig secret.
	// This way we are using the envtest environment as both the
	// management and the workload cluster.
	testEnvKubeconfig := kubeconfig.FromEnvTestConfig(testEnv.GetConfig(), &clusterv1.Cluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster",
			Namespace: ns.Name,
		},
	})
	kubeconfigSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-cluster-kubeconfig",
			Namespace: ns.Name,
		},
		Data: map[string][]byte{
			secret.KubeconfigDataName: testEnvKubeconfig,
		},
	}
	clusterKey := client.ObjectKey{
		Name:      "my-cluster",
		Namespace: ns.Name,
	}

	tests := []struct {
		name       string
		clusterKey client.ObjectKey
		objs       []runtime.Object
		expectErr  bool
	}{
		{
			name:       "returns a workload cluster",
			clusterKey: clusterKey,
			objs:       []runtime.Object{etcdSecret.DeepCopy(), kubeconfigSecret.DeepCopy()},
			expectErr:  false,
		},
		{
			name:       "returns error if cannot get rest.Config from kubeconfigSecret",
			clusterKey: clusterKey,
			objs:       []runtime.Object{etcdSecret.DeepCopy()},
			expectErr:  true,
		},
		{
			name:       "returns error if unable to find the etcd secret",
			clusterKey: clusterKey,
			objs:       []runtime.Object{kubeconfigSecret.DeepCopy()},
			expectErr:  true,
		},
		{
			name:       "returns error if unable to find the certificate in the etcd secret",
			clusterKey: clusterKey,
			objs:       []runtime.Object{emptyCrtEtcdSecret.DeepCopy(), kubeconfigSecret.DeepCopy()},
			expectErr:  true,
		},
		{
			name:       "returns error if unable to find the key in the etcd secret",
			clusterKey: clusterKey,
			objs:       []runtime.Object{emptyKeyEtcdSecret.DeepCopy(), kubeconfigSecret.DeepCopy()},
			expectErr:  true,
		},
		{
			name:       "returns error if unable to generate client cert",
			clusterKey: clusterKey,
			objs:       []runtime.Object{badCrtEtcdSecret.DeepCopy(), kubeconfigSecret.DeepCopy()},
			expectErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)

			for _, o := range tt.objs {
				g.Expect(testEnv.CreateObj(ctx, o)).To(Succeed())
				defer func(do runtime.Object) {
					g.Expect(testEnv.Cleanup(ctx, do)).To(Succeed())
				}(o)
			}

			m := Management{
				Client: testEnv,
			}

			workloadCluster, err := m.GetWorkloadCluster(context.Background(), tt.clusterKey)
			if tt.expectErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(workloadCluster).To(BeNil())
				return
			}
			g.Expect(err).ToNot(HaveOccurred())
			g.Expect(workloadCluster).ToNot(BeNil())
		})
	}

}

func getTestCACert(key *rsa.PrivateKey) (*x509.Certificate, error) {
	cfg := certs.Config{
		CommonName: "kubernetes",
	}

	now := time.Now().UTC()

	tmpl := x509.Certificate{
		SerialNumber: new(big.Int).SetInt64(0),
		Subject: pkix.Name{
			CommonName:   cfg.CommonName,
			Organization: cfg.Organization,
		},
		NotBefore:             now.Add(time.Minute * -5),
		NotAfter:              now.Add(time.Hour * 24), // 1 day
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		MaxPathLenZero:        true,
		BasicConstraintsValid: true,
		MaxPathLen:            0,
		IsCA:                  true,
	}

	b, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, key.Public(), key)
	if err != nil {
		return nil, err
	}

	c, err := x509.ParseCertificate(b)
	return c, err
}

func podReady(isReady corev1.ConditionStatus) corev1.PodCondition {
	return corev1.PodCondition{
		Type:   corev1.PodReady,
		Status: isReady,
	}
}

type checkStaticPodReadyConditionTest struct {
	name       string
	conditions []corev1.PodCondition
}

func nodeListForMachines(machines []*clusterv1.Machine) *corev1.NodeList {
	nodeList := []corev1.Node{}
	for _, machine := range machines {
		nodeList = append(nodeList, nodeNamed(machine.Name))
	}
	return &corev1.NodeList{
		Items: nodeList,
	}
}

func nodeNamed(name string, options ...func(n corev1.Node) corev1.Node) corev1.Node {
	node := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	for _, opt := range options {
		node = opt(node)
	}
	return node
}

type podOption func(*corev1.Pod)

func getPod(name string, options ...podOption) *corev1.Pod {
	p := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: metav1.NamespaceSystem,
		},
	}
	for _, opt := range options {
		opt(p)
	}
	return p
}

func withReadyOption(pod *corev1.Pod) {
	readyCondition := corev1.PodCondition{
		Type:   corev1.PodReady,
		Status: corev1.ConditionTrue,
	}
	pod.Status.Conditions = append(pod.Status.Conditions, readyCondition)
}

func withPodInitializedOption(status corev1.ConditionStatus) podOption {
	initializedCondition := corev1.PodCondition{
		Type:   corev1.PodInitialized,
		Status: status,
	}

	return func(pod *corev1.Pod) {
		pod.Status.Conditions = append(pod.Status.Conditions, initializedCondition)
	}
}

func withFailedStatus(pod *corev1.Pod) {
	pod.Status.Phase = corev1.PodFailed
}

func withPendingStatus(pod *corev1.Pod) {
	pod.Status.Phase = corev1.PodPending
}

func machineListForTestGetMachinesForCluster() *clusterv1.MachineList {
	owned := true
	ownedRef := []metav1.OwnerReference{
		{
			Kind:       "KubeadmControlPlane",
			Name:       "my-control-plane",
			Controller: &owned,
		},
	}
	machine := func(name string) clusterv1.Machine {
		return clusterv1.Machine{
			TypeMeta: metav1.TypeMeta{},
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "my-namespace",
				Labels: map[string]string{
					clusterv1.ClusterLabelName: "my-cluster",
				},
			},
		}
	}
	controlPlaneMachine := machine("first-machine")
	controlPlaneMachine.ObjectMeta.Labels[clusterv1.MachineControlPlaneLabelName] = ""
	controlPlaneMachine.OwnerReferences = ownedRef

	return &clusterv1.MachineList{
		Items: []clusterv1.Machine{
			controlPlaneMachine,
			machine("second-machine"),
			machine("third-machine"),
		},
	}
}

type fakeClient struct {
	client.Client
	list interface{}

	createErr    error
	get          map[string]interface{}
	getCalled    bool
	updateCalled bool
	getErr       error
	patchErr     error
	updateErr    error
	listErr      error
}

func (f *fakeClient) Get(_ context.Context, key client.ObjectKey, obj runtime.Object) error {
	f.getCalled = true
	if f.getErr != nil {
		return f.getErr
	}
	item := f.get[key.String()]
	switch l := item.(type) {
	case *corev1.Pod:
		l.DeepCopyInto(obj.(*corev1.Pod))
	case *rbacv1.RoleBinding:
		l.DeepCopyInto(obj.(*rbacv1.RoleBinding))
	case *rbacv1.Role:
		l.DeepCopyInto(obj.(*rbacv1.Role))
	case *appsv1.DaemonSet:
		l.DeepCopyInto(obj.(*appsv1.DaemonSet))
	case *corev1.ConfigMap:
		l.DeepCopyInto(obj.(*corev1.ConfigMap))
	case *corev1.Secret:
		l.DeepCopyInto(obj.(*corev1.Secret))
	case nil:
		return apierrors.NewNotFound(schema.GroupResource{}, key.Name)
	default:
		return fmt.Errorf("unknown type: %s", l)
	}
	return nil
}

func (f *fakeClient) List(_ context.Context, list runtime.Object, _ ...client.ListOption) error {
	if f.listErr != nil {
		return f.listErr
	}
	switch l := f.list.(type) {
	case *clusterv1.MachineList:
		l.DeepCopyInto(list.(*clusterv1.MachineList))
	case *corev1.NodeList:
		l.DeepCopyInto(list.(*corev1.NodeList))
	case *corev1.PodList:
		l.DeepCopyInto(list.(*corev1.PodList))
	default:
		return fmt.Errorf("unknown type: %s", l)
	}
	return nil
}

func (f *fakeClient) Create(_ context.Context, _ runtime.Object, _ ...client.CreateOption) error {
	if f.createErr != nil {
		return f.createErr
	}
	return nil
}

func (f *fakeClient) Patch(_ context.Context, _ runtime.Object, _ client.Patch, _ ...client.PatchOption) error {
	if f.patchErr != nil {
		return f.patchErr
	}
	return nil
}

func (f *fakeClient) Update(_ context.Context, _ runtime.Object, _ ...client.UpdateOption) error {
	f.updateCalled = true
	if f.updateErr != nil {
		return f.updateErr
	}
	return nil
}
