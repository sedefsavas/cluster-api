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
	"os"
	"path/filepath"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/onsi/ginkgo/config"
	"github.com/onsi/ginkgo/reporters"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/cluster-api/test/framework/management"
	capiFlag "sigs.k8s.io/cluster-api/test/helpers/flag"
	capde2e "sigs.k8s.io/cluster-api/test/infrastructure/docker/e2e"
)

var (
	//TODO: refactor this so we can avoid Flag variables
	artifactFolderFlag     = capiFlag.DefineOrLookupStringFlag("e2e.artifacts-folder", "", "folder where e2e test artifact should be stored")
	configPathFlag         = capiFlag.DefineOrLookupStringFlag("e2e.config", "", "path to the e2e config file")
	useExistingClusterFlag = capiFlag.DefineOrLookupBoolFlag("e2e.use-existing-cluster", false, "if true, the test uses the current cluster instead of creating a new one (default discovery rules apply)")
	skipCleanupFlag        = capiFlag.DefineOrLookupBoolFlag("e2e.skip-resource-cleanup", false, "if true, the resource cleanup after tests will be skipped")
)

func TestE2E(t *testing.T) {
	artifactFolder = *artifactFolderFlag
	configPath = *configPathFlag
	skipCleanup = *skipCleanupFlag
	useExistingCluster = *useExistingClusterFlag

	artifactFolder = "/Users/fpandini/go/src/sigs.k8s.io/cluster-api/_artifacts"
	configPath = "/Users/fpandini/go/src/sigs.k8s.io/cluster-api/test/e2e/config/docker-dev.conf"

	// If running in prow, make sure to output the junit files to the artifacts path
	//TODO: is this required? having an env variable override a flag (one and only) might create confusion
	if ap, exists := os.LookupEnv("ARTIFACTS"); exists {
		artifactFolder = ap
	}

	RegisterFailHandler(Fail)
	junitPath := filepath.Join(artifactFolder, fmt.Sprintf("junit.e2e_suite.%d.xml", config.GinkgoConfig.ParallelNode))
	junitReporter := reporters.NewJUnitReporter(junitPath)
	RunSpecsWithDefaultAndCustomReporters(t, "capi-e2e", []Reporter{junitReporter})
}

var (
	// configPath to the e2e config file.
	configPath string

	// useExistingCluster instruct the test to use the current cluster instead of creating a new one (default discovery rules apply).
	useExistingCluster bool

	// artifactFolder where to store e2e test artifact.
	artifactFolder string

	// skipCleanup prevent cleanup of test resources e.g. for debug purposes.
	skipCleanup bool

	// e2eConfig to be used for this test, read from configPath.
	e2eConfig *clusterctl.E2EConfig

	// clusterctlConfigPath to be used for this test, created by generating a clusterctl local repository
	// with the providers specified in the configPath.
	clusterctlConfigPath string

	// managementCluster to be used for the e2e tests.
	managementCluster framework.ManagementCluster
)

// Using a SynchronizedBeforeSuite for controlling how to create resources shared across ParallelNodes (~ginkgo threads).
// In this case, the local clusterctl repository & the bootstrap cluster are shared across all the tests.
var _ = SynchronizedBeforeSuite(func() []byte {
	// Before all ParallelNodes.

	validateBeforeSuiteInputs()

	By("Initializing a runtime.Scheme with all the GVK relevant for this test")
	scheme := initScheme()

	Byf("Loading the e2e test configuration from %s", configPath)
	e2eConfig = loadE2EConfig(configPath)

	Byf("Creating a clusterctl local repository into %s", artifactFolder)
	clusterctlConfigPath = createClusterctlLocalRepository(e2eConfig, filepath.Join(artifactFolder, "repository"))

	By("Getting the management cluster")
	if useExistingCluster {
		managementCluster = connectToManagementCluster(scheme)
	} else {
		managementCluster = createManagementCluster(e2eConfig, scheme)
	}

	By("Initializing the management cluster")
	initManagementCluster(managementCluster, clusterctlConfigPath, e2eConfig.InfraProviders(), filepath.Join(artifactFolder, "bootstrap-cluster"), e2eConfig.GetIntervals("bootstrap-cluster", "wait-controllers")...)

	return nil
}, func(_ []byte) {
	// Before each ParallelNode.
})

// Using a SynchronizedAfterSuite for controlling how to delete resources shared across ParallelNodes (~ginkgo threads).
// In this case, deleting the bootstrap cluster shared across all the tests; instead the local clusterctl repository
// is preserved like everything else created into the artifact folder.
var _ = SynchronizedAfterSuite(func() {
	// After each ParallelNode.
}, func() {
	// After all ParallelNodes.

	By("Tearing down the management cluster")
	if !skipCleanup {
		tearDown(managementCluster)
	}
})

func Byf(format string, a ...interface{}) {
	By(fmt.Sprintf(format, a...))
}

func validateBeforeSuiteInputs() {
	Expect(configPath).To(BeAnExistingFile(), "Invalid argument. e2e.config should be an existing file")
	Expect(os.MkdirAll(artifactFolder, 0755)).To(Succeed(), "Invalid argument. can't create e2e.artifacts-folder %s", artifactFolder)
}

func initScheme() *runtime.Scheme {
	sc := runtime.NewScheme()
	framework.TryAddDefaultSchemes(sc)
	return sc
}

func loadE2EConfig(configPath string) *clusterctl.E2EConfig {
	config := clusterctl.LoadE2EConfig(context.TODO(), clusterctl.LoadE2EConfigInput{ConfigPath: configPath})
	Expect(config).ToNot(BeNil(), "Failed to load E2E config from %s", configPath)
	return config
}

func createClusterctlLocalRepository(config *clusterctl.E2EConfig, repositoryPath string) string {
	clusterctlConfig := clusterctl.CreateRepository(context.TODO(), clusterctl.CreateRepositoryInput{
		E2EConfig:        config,
		RepositoryFolder: repositoryPath,
	})
	Expect(clusterctlConfig).To(BeAnExistingFile(), "The clusterctl config file does not exists in the local repository %s", repositoryPath)
	return clusterctlConfig
}

func connectToManagementCluster(scheme *runtime.Scheme) framework.ManagementCluster {
	//TODO: in case the Management cluster is kind, we should inject something that handles workload clients properly on mac
	//TODO: make base cluster to manage GetWorkloadKubeconfig for self hosted scenario
	return management.NewBaseCluster("", scheme)
}

func createManagementCluster(config *clusterctl.E2EConfig, scheme *runtime.Scheme) framework.ManagementCluster {
	cluster, err := clusterctl.CreateManagementCluster(context.TODO(), &clusterctl.CreateManagementClusterInput{
		Name:   config.ManagementClusterName,
		Images: config.Images,
		Scheme: scheme,
		NewManagementClusterFn: func(name string, scheme *runtime.Scheme) (framework.ManagementCluster, error) {
			//TODO: make base cluster to manage GetWorkloadKubeconfig for self hosted scenario
			//TODO: manage "use kind without CAPD" scenario.
			return capde2e.NewClusterForCAPD(context.TODO(), name, scheme)
		},
	})
	Expect(err).NotTo(HaveOccurred(), "Failed to create a management cluster")
	Expect(cluster).ToNot(BeNil(), "Failed to create a management cluster")
	Expect(cluster.GetKubeconfigPath()).To(BeAnExistingFile(), "Failed to get a clusterctl config file")
	return cluster
}

func initManagementCluster(cluster framework.ManagementCluster, clusterctlConfigPath string, infraProviders []string, artifactFolder string, waitForProviderIntervals ...interface{}) {
	//TODO: skip init if already initialized
	//TODO: split attach watches to a management cluster (attach watches should be executed no matter if we are skipping init or not)
	clusterctl.InitManagementCluster(context.TODO(), &clusterctl.InitManagementClusterInput{
		ManagementCluster:        cluster,
		InfrastructureProviders:  infraProviders,
		ClusterctlConfigPath:     clusterctlConfigPath,
		WaitForProviderIntervals: waitForProviderIntervals,
		LogsFolder:               artifactFolder,
	})
}

func tearDown(cluster framework.ManagementCluster) {
	if cluster != nil {
		cluster.Teardown(context.TODO())
	}
}
