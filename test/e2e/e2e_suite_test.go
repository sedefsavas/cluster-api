// +build e2e

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

package e2e

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/config"
	"github.com/onsi/ginkgo/reporters"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiFlag "sigs.k8s.io/cluster-api/test/helpers/flag"
	capde2e "sigs.k8s.io/cluster-api/test/infrastructure/docker/e2e"
)

var (
	artifactFolderFlag = capiFlag.DefineOrLookupStringFlag("e2e.artifacts-folder", "", "folder where e2e test artifact should be stored")
	configPathFlag     = capiFlag.DefineOrLookupStringFlag("e2e.config", "", "path to the e2e config file")
	skipCleanupFlag    = capiFlag.DefineOrLookupBoolFlag("e2e.skip-resource-cleanup", false, "if true, the resource cleanup after tests will be skipped")
)

func TestE2E(t *testing.T) {
	artifactFolder = *artifactFolderFlag
	configPath = *configPathFlag
	skipCleanup = *skipCleanupFlag

	RegisterFailHandler(Fail)
	junitPath := filepath.Join(artifactFolder, fmt.Sprintf("junit.e2e_suite.%d.xml", config.GinkgoConfig.ParallelNode))
	junitReporter := reporters.NewJUnitReporter(junitPath)
	RunSpecsWithDefaultAndCustomReporters(t, "capi-e2e", []Reporter{junitReporter})
}

var (
	// configPath to the e2e config file.
	configPath string

	// artifactFolder where to store e2e test artifact.
	artifactFolder string

	// skipCleanup prevent cleanup of test resources e.g. for debug purposes.
	skipCleanup bool

	// e2eConfig to be used for this test, read from configPath.
	e2eConfig *clusterctl.E2EConfig

	// clusterctlConfigPath to be used for this test, created by generating a clusterctl local repository
	// with the providers specified in the configPath.
	clusterctlConfigPath string

	// scheme with all the GVK relevant for this test.
	scheme *runtime.Scheme

	// managementCluster to be used for the e2e tests.
	managementCluster framework.ManagementCluster

	// kubeconfig file pointing to the managementCluster used for the e2e tests.
	managementClusterKubeConfigPath string
)

// Using a singleton for creating resources shared across nodes when running tests in parallel.
// In this case, the local clusterctl repository & the bootstrap cluster are shared across all the tests.
var _ = SynchronizedBeforeSuite(func() []byte {
	// Before all ParallelNodes

	Expect(configPath).To(BeAnExistingFile(), "invalid argument. e2e.config should be an existing file")
	Expect(artifactFolder).To(BeADirectory(), "invalid argument. e2e.artifacts-folder should be an existing folder")

	By("Initializing a runtime.Scheme with all the GVK relevant for this test")
	scheme = runtime.NewScheme()
	framework.TryAddDefaultSchemes(scheme)

	By(fmt.Sprintf("Loading the e2e test configuration from %s", configPath))
	e2eConfig = clusterctl.LoadE2EConfig(context.TODO(), clusterctl.LoadE2EConfigInput{ConfigPath: configPath})
	Expect(e2eConfig).ToNot(BeNil(), "Failed to load E2E config")

	By(fmt.Sprintf("Creating a clusterctl local repository into %s", artifactFolder))
	// NB. Creating the local clusterctl repository into the the artifact folder so it will be preserved at the end of the test
	clusterctlConfigPath = clusterctl.CreateRepository(context.TODO(), clusterctl.CreateRepositoryInput{
		E2EConfig:     e2eConfig,
		ArtifactsPath: artifactFolder,
	})
	Expect(clusterctlConfigPath).To(BeAnExistingFile(), "Failed to get a clusterctl config file")

	By("Initializing a management cluster")
	//TODO: manage "use existing cluster" scenario. Initial assumption: existing cluster should have providers
	//TODO: manage "use kind without CAPD" scenario.
	//TODO: stream logs out of controllers
	//TODO: define log folder organization (logs for the bootstrap cluster, logs for each spec
	managementCluster, managementClusterKubeConfigPath = clusterctl.InitManagementCluster(context.TODO(), &clusterctl.InitManagementClusterInput{
		E2EConfig:            e2eConfig,
		ClusterctlConfigPath: clusterctlConfigPath,
		Scheme:               scheme,
		NewManagementClusterFn: func(name string, scheme *runtime.Scheme) (cluster framework.ManagementCluster, kubeConfigPath string, err error) {
			cluster, err = capde2e.NewClusterForCAPD(context.TODO(), name, scheme)
			if err != nil {
				return nil, "", err
			}
			//TODO: this is hacky way to get the kubeconfig path. As soon as v1alpha4 opens, we should add GetKubeConfigPath method to the ManagementCluster interface and get rid of this
			//TODO: move th kubeconfig into the artifact folder
			kindCluster := cluster.(*capde2e.CAPDCluster)
			kubeConfigPath = kindCluster.KubeconfigPath
			return cluster, kubeConfigPath, err
		},
		LogsFolder: artifactFolder,
	})
	Expect(managementCluster).ToNot(BeNil(), "Failed to get a management cluster")
	Expect(managementClusterKubeConfigPath).To(BeAnExistingFile(), "Failed to get a clusterctl config file")

	return nil
}, func(_ []byte) {
	// Before each ParallelNode
})

// Using a singleton for deleting resources shared across nodes when running tests in parallel.
// In this case, deleting the bootstrap cluster shared across all the tests; instead the local clusterctl repository
// is preserved like everything else created into the artifact folder.
var _ = SynchronizedAfterSuite(func() {
	// After each ParallelNode
}, func() {
	// After all ParallelNodes
	if skipCleanup {
		return
	}

	// Tears down the management cluster
	By("Tearing down the management cluster")
	if managementCluster != nil {
		managementCluster.Teardown(context.TODO())
	}
})
