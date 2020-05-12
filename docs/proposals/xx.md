---
title: PostCreateConfig Design
authors:
 - "@sedefsavas"
reviewers:
 - "@vincepri"
 - "@detiber"
 - "@ncdc"
 - "@fabriziopandini"
creation-date: 2020-02-20
last-updated: 2020-05-04
status: provisional
---

# PostCreateConfig Design
## Table of Contents
TODO when opening PR

## Glossary
## Summary
Clusters created by Cluster API are minimally functional. They do not have a container networking interface (CNI), which is required for pod-to-pod networking. They do not have any StorageClasses, which are required for dynamic persistent volume provisioning.

Users today must first remember to add these things to every cluster they create, and then actually add them.

Having a mechanism to apply an initial set of default resources after clusters are created makes clusters created with Cluster API functional and ready for workloads from the beginning, without requiring additional user intervention. 

## Motivation
### Goals
Provide a means to specify a set of resources to apply automatically to newly-created Clusters
Apply all PostCreateConfig resources to each matching cluster only once.
### Non-Goals/Future Work
Continuously reconcile the set of resources
Replace or compete with the Cluster Addons subproject
## Proposal
### User Stories

Inserting a new PostCreateConfig (covers the case when there exist clusters with matching labels).
Inserting a new Cluster with a label that matches some of the PostCreateConfig targets
Applying a new label to an existing cluster that matches with a PostCreateConfig object 

### Implementation Details/Notes/Constraints
#### Data model changes
None. We are planning to implement this feature without modifying any of the existing structure to minimize the footprint of PostCreateConfig Controller. This enhancement will follow Kubernetes’s feature-gate structure and will be under the experimental package with its APIs, and enabled/disabled with a feature gate. 
#### PostCreateConfig CRD Scheme
This is the CRD that is used to have a set of components that will be applied to clusters that match the label selector in it.
resources field is a list of secrets (name/namespace). As cluster label selector, any key-value pair works as long as the same key-value label is assigned to clusters that the addon will be applied to. The reason not to use a predefined label here is to allow matching with multiple PostCreateConfig objects.


apiVersion: postcreate.cluster.x-k8s.io/v1alpha3
kind: PostCreateConfig
metadata:
 name: postcreate-conf
 namespace: default
spec:
 clusterSelector:
   matchLabels:
     postcreatelabelcni: calico
 resources:
   - name: calico-addon
     namespace: default
   - name: network-policy-addon
     namespace: nw-system
Sample PostCreateConfig YAML

Each secret in resources list includes yaml content as value. The key to that content is addon.yaml.

apiVersion: v1
kind: Secret
metadata:
  name:calico-addon
type: Opaque
stringData:
  addon.yaml: |-
    kind: ConfigMap
    apiVersion: v1
    metadata:
     name: calico-conf
    
Sample Secret Format

As an example, the following cluster matches “postcreatelabelcni: calico” in the PostCreateConfig above:
# kubectl get clusters postcreate-cluster  -o yaml
apiVersion: cluster.x-k8s.io/v1alpha3
kind: Cluster
  labels:
    postcreatelabelcni: calico
  name: postcreate-cluster
  namespace: default
An Example Cluster with Matching Label

The secrets in PostCreateConfig will be applied to that cluster if all the secrets exist. When successful, the cluster will be added to the status clusterRefList.
# kubectl get postcreateconfig postcreate-conf -o yaml
spec:
  clusterSelector:
    matchLabels:
      postcreatelabelcni: calico
  resources:
  - name: calico-addon
    namespace: default
   - name: network-policy-addon
     namespace: nw-system
status:
  clusterRefList:
  - kind: Cluster
    name: postcreate-cluster
    namespace: default
    uid: 6bfea78e-e305-48f0-9d74-7b521968e591
 - kind: Cluster
    name: postcreate-cluster
    namespace: default
    uid: 6bfea78e-e305-48f0-9d74-7b521968e591
A PostCreateConfig object example after applying the secrets to a cluster
#### PostCreateConfig Controller Design

PostCreateConfig controller reconciles PostCreateConfig while watching Cluster objects. It is running in capi-contoller-manager binary and enabled by --feature-gates=PostCreate=true
Below is a pseudocode for the reconcile logic:

```yaml
Reconcile() {
Fetch all clusters that match PostCreateConfig object's label
if all secrets in PostCreateConfig object exist
	for each cluster in clusters in the matching cluster list
	    if cluster is not in ClusterRef list of PostCreateConfig
		for each secret in the resources list
		apply secret to cluster
	Add cluster to ClusterRef in PostCreateConfig's status
}
```

When a cluster object comes to the map function in PostCreateConfig controller, all matching PostCreateConfig objects are fetched and returned for reconciliation.
An implementation of this can be here.
### Risks and Mitigations

## Alternatives
The Alternatives section is used to highlight and record other possible approaches to delivering the value proposed by a proposal.
## Upgrade Strategy
If applicable, how will the component be upgraded? Make sure this is in the test plan.
Consider the following in developing an upgrade strategy for this enhancement:
What changes (in invocations, configurations, API use, etc.) is an existing cluster required to make on upgrade in order to keep previous behavior?
What changes (in invocations, configurations, API use, etc.) is an existing cluster required to make on upgrade in order to make use of the enhancement?
## Additional Details
Test Plan [optional]
Note: Section not required until targeted at a release.
Consider the following in developing a test plan for this enhancement:
Will there be e2e and integration tests, in addition to unit tests?
How will it be tested in isolation vs with other components?
No need to outline all of the test cases, just the general strategy. Anything that would count as tricky in the implementation and anything particularly challenging to test should be called out.
All code is expected to have adequate tests (eventually with coverage expectations). Please adhere to the Kubernetes testing guidelines when drafting this test plan.
Graduation Criteria [optional]
Note: Section not required until targeted at a release.
Define graduation milestones.
These may be defined in terms of API maturity, or as something else. Initial proposal should keep this high-level with a focus on what signals will be looked at to determine graduation.
Consider the following in developing the graduation criteria for this enhancement:
Maturity levels (alpha, beta, stable)
Deprecation policy
Clearly define what graduation means by either linking to the API doc definition, or by redefining what graduation means.
In general, we try to use the same stages (alpha, beta, GA), regardless how the functionality is accessed.

Version Skew Strategy [optional]
If applicable, how will the component handle version skew with other components? What are the guarantees? Make sure this is in the test plan.
Consider the following in developing a version skew strategy for this enhancement:
Does this enhancement involve coordinating behavior in the control plane and in the kubelet? How does an n-2 kubelet without this feature available behave when this feature is used?
Will any other components on the node change? For example, changes to CSI, CRI or CNI may require updating that component before the kubelet.

## Implementation History
- [ ] 02/26/2020: Proposed idea in an issue or [community meeting]
- [ ] MM/DD/YYYY: Compile a [CAEP Google Doc] following the CAEP template
- [ ] MM/DD/YYYY: First round of feedback from community
- [ ] MM/DD/YYYY: Present proposal at a [community meeting]
- [ ] MM/DD/YYYY: Open proposal PR
<!-- Links -->

Open Questions
We aim to use this document as the discussion environment for open issues.

Deletion of object: Should we clean up the addons applied to the clusters?
Instead of keeping a list of references to show which clusters are processed by a PostCreateConfig object, annotations can be used.
Current POC is applying addons to a matching cluster only if all secrets exist. Should we do best-effort apply instead? In that case, the ClusterRef will be added to the PostCreateConfig object and hence even if some missing addons are added in the future, controller will never retry to apply those to the cluster.
Another way of matching clusters is:  if PostCreateConfig object is in namespace A, apply this only to the clusters that are created in namespace A.
What becomes the source of truth for manifests that are added through a PostCreateConfig?  For example, if I want to upgrade Calico on a workload cluster, must I always do it through this mechanism?  What if different personas own what’s in this config versus what’s on a workload cluster?
