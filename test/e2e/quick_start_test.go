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
	"fmt"

	. "github.com/onsi/ginkgo"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1alpha3"
	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/cluster-api/util"
)

var _ = Describe("When following the Cluster API quick-start", func() {
	var (
		spec    *SpecContext
		cluster *clusterv1.Cluster
	)

	BeforeEach(func() {
		By("Creating the spec context")
		spec = initSpec(initSpecInput{
			specName:             "quick-start",
			managementCluster:    managementCluster,
			clusterctlConfigPath: clusterctlConfigPath,
			artifactFolder:       artifactFolder,
			getIntervals:         e2eConfig.GetIntervals,
		})
	})


	It("Should create a workload cluster", func() {
		settings := createClusterTemplateInput{getClusterTemplateInput{
			flavor:      clusterctl.DefaultFlavor,
			clusterName: fmt.Sprintf("cluster-%s", util.RandomString(6)),
			kubernetesVersion:        e2eConfig.GetKubernetesVersion(),
			controlPlaneMachineCount: e2eConfig.GetControlPlaneMachineCount(),
			workerMachineCount:       e2eConfig.GetWorkerMachineCount(),
		}}

		Byf("Creating the a cluster name %s using %s template (%s, %d control-planes, %d workers)",
			settings.clusterName, valueOrDefault(settings.flavor), settings.kubernetesVersion, settings.controlPlaneMachineCount, settings.workerMachineCount)
		cluster, _, _ = createClusterTemplate(spec, settings)
	})

	AfterEach(func() {
		Byf("Dumping all the Cluster API resources in the %s namespace", spec.namespace.Name)
		dumpResources(spec)

		Byf("Deleting cluster %s/%s", cluster.Namespace, cluster.Name)
		deleteCluster(spec, cluster)

		By("Deleting spec context")
		if !skipCleanup {
			spec.Cleanup()
		}
		spec.Close()
	})
})
