/*
Copyright 2019 The Kubernetes Authors.

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
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"k8s.io/klog/klogr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	"sigs.k8s.io/controller-runtime/pkg/log"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	clusterresourcesetv1 "sigs.k8s.io/cluster-api/exp/clusterresourceset/api/v1alpha3"
	// +kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

func init() {
	klog.InitFlags(nil)
	logf.SetLogger(klogr.New())

	// Register required object kinds with global scheme.
	_ = apiextensionsv1.AddToScheme(scheme.Scheme)
	_ = clusterv1.AddToScheme(scheme.Scheme)
	_ = clusterresourcesetv1.AddToScheme(scheme.Scheme)
}

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	mgr       manager.Manager
	doneMgr   = make(chan struct{})
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"Controller Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = BeforeSuite(func(done Done) {
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{filepath.Join("..", "..", "..", "config", "crd", "bases")},
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	// +kubebuilder:scaffold:scheme

	By("setting up a new manager")
	mgr, err = manager.New(cfg, manager.Options{
		Scheme:             scheme.Scheme,
		MetricsBindAddress: "0",
		NewCache: func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
			syncPeriod := 1 * time.Second
			opts.Resync = &syncPeriod
			return cache.New(config, opts)
		},
	})
	Expect(err).NotTo(HaveOccurred())

	k8sClient = mgr.GetClient()

	Expect((&ClusterResourceSetReconciler{
		Client:   k8sClient,
		Log:      log.Log,
		recorder: mgr.GetEventRecorderFor("clusterresourceset-controller"),
	}).SetupWithManager(mgr, controller.Options{MaxConcurrentReconciles: 1})).To(Succeed())

	By("starting the manager")
	go func() {
		Expect(mgr.Start(doneMgr)).To(Succeed())
	}()

	close(done)
}, 60)

var _ = AfterSuite(func() {
	By("closing the manager")
	close(doneMgr)
	By("tearing down the test environment")
	Expect(testEnv.Stop()).To(Succeed())
})
