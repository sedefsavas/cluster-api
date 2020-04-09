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
	"path"
	"path/filepath"
	goRuntime "runtime"
	"strings"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/onsi/ginkgo/config"
	"github.com/onsi/ginkgo/reporters"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/cluster-api/test/framework"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	capiFlag "sigs.k8s.io/cluster-api/test/helpers/flag"
	infrav1 "sigs.k8s.io/cluster-api/test/infrastructure/docker/api/v1alpha3"
	capde2e "sigs.k8s.io/cluster-api/test/infrastructure/docker/e2e"
	"sigs.k8s.io/cluster-api/util"
)

var (
	artifactPathFlag = capiFlag.DefineOrLookupStringFlag("e2e.artifacts-path", "", "path to store e2e test artifacts")
	configPathFlag   = capiFlag.DefineOrLookupStringFlag("e2e.config", "", "path to the e2e config file")
	skipCleanupFlag  = capiFlag.DefineOrLookupBoolFlag("e2e.skip-resource-cleanup", false, "if true, the resource cleanup after tests will be skipped")
)

func TestE2E(t *testing.T) {
	artifactPath = *artifactPathFlag
	configPath = *configPathFlag
	skipCleanup = *skipCleanupFlag

	// If running in prow, make sure to output the junit files to the artifacts path
	if ap, exists := os.LookupEnv("ARTIFACTS"); exists {
		artifactPath = ap
	}

	RegisterFailHandler(Fail)
	junitPath := filepath.Join(artifactPath, fmt.Sprintf("junit.e2e_suite.%d.xml", config.GinkgoConfig.ParallelNode))
	junitReporter := reporters.NewJUnitReporter(junitPath)
	RunSpecsWithDefaultAndCustomReporters(t, "capi-e2e", []Reporter{junitReporter})
}

var (
	// configPath to the e2e config file.
	configPath string

	// artifactPath where to store e2e test artifact.
	artifactPath string

	// logsPath where to store e2e test logs.
	logsPath string

	// resourcesPath where to store e2e test resources.
	resourcesPath string

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
	artifactPath = os.Getenv("ARTIFACTS")

	By("creating the logs directory")
	logsPath = path.Join(artifactPath, "logs")
	Expect(os.MkdirAll(filepath.Dir(logsPath), 0755)).To(Succeed())

	By("creating the providers directory")
	providerLogsPath := path.Join(logsPath, "common")
	Expect(os.MkdirAll(filepath.Dir(providerLogsPath), 0755)).To(Succeed())

	By("creating the resources directory")
	resourcesPath = path.Join(artifactPath, "resources")
	Expect(os.MkdirAll(filepath.Dir(resourcesPath), 0755)).To(Succeed())

	Expect(configPath).To(BeAnExistingFile(), "invalid argument. e2e.config should be an existing file")
	Expect(artifactPath).To(BeADirectory(), "invalid argument. e2e.artifacts-folder should be an existing folder")

	By("Initializing a runtime.Scheme with all the GVK relevant for this test")
	scheme = runtime.NewScheme()
	framework.TryAddDefaultSchemes(scheme)
	// TODO make adding infra provider schemes generic (if it is possible)
	Expect(infrav1.AddToScheme(scheme)).To(Succeed())

	By(fmt.Sprintf("Loading the e2e test configuration from %s", configPath))
	e2eConfig = clusterctl.LoadE2EConfig(context.TODO(), clusterctl.LoadE2EConfigInput{ConfigPath: configPath})
	Expect(e2eConfig).ToNot(BeNil(), "Failed to load E2E config")

	By(fmt.Sprintf("Creating a clusterctl local repository into %s", artifactPath))
	// NB. Creating the local clusterctl repository into the the artifact folder so it will be preserved at the end of the test
	clusterctlConfigPath = clusterctl.CreateRepository(context.TODO(), clusterctl.CreateRepositoryInput{
		E2EConfig:     e2eConfig,
		ArtifactsPath: artifactPath,
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
		LogsFolder: providerLogsPath,
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

// TODO Move the functions below to the helper folder once it is moved under /test/e2e

// GenerateRandomNamespace generates a random namespace using the caller's file name
func GenerateRandomNamespace() string {
	testName := strings.ReplaceAll(GetSuiteName(), "_", "-")
	return fmt.Sprintf("%s-%s", testName, util.RandomString(6))
}

// GetSuiteName gets the currently running test's name and trims it. For example, for "/foo/bar.go" returns "bar"
func GetSuiteName() string {
	_, filename, _, ok := goRuntime.Caller(1)
	if !ok {
		return ""
	}
	filename = filepath.Base(filename)

	return filename[0 : len(filename)-len(filepath.Ext(filename))]
}
