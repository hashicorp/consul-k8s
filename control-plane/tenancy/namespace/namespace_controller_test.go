// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package namespace

import (
	"context"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/proto-public/pbresource"
	pbtenancy "github.com/hashicorp/consul/proto-public/pbtenancy/v2beta1"
	"github.com/hashicorp/consul/sdk/testutil"
)

const (
	testNamespaceName  = "foo"
	testCrossACLPolicy = "cross-namespace-policy"
)

// Tests deleting a Namespace object, with and without matching Consul resources.
func TestReconcileDeleteNamespace(t *testing.T) {
	//t.Parallel()

	type testCase struct {
		name              string
		kubeNamespaceName string // this will default to "foo"
		partition         string

		destinationNamespace string
		enableNSMirroring    bool
		nsMirrorPrefix       string

		existingConsulNamespace string
		expectedConsulNamespace string
		//existingConsulNamespace *capi.Namespace
		//expectedConsulNamespace *capi.Namespace
	}

	run := func(t *testing.T, tc testCase) {
		fakeClient := fake.NewClientBuilder().WithRuntimeObjects().Build()

		// Create test consulServer server.
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis", "v2tenancy"}
		})

		resourceClient, err := consul.NewResourceServiceClient(testClient.Watcher)
		require.NoError(t, err)

		// Create the partition
		if tc.partition != "" && tc.partition != "default" {
			testClient.Cfg.APIClientConfig.Partition = tc.partition

			_, err := resourceClient.Write(context.Background(), &pbresource.WriteRequest{Resource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: tc.partition,
					Type: pbtenancy.PartitionType,
				},
			}})
			require.NoError(t, err, "failed to create partition")
			// TODO: require.ResourceExists()
		}

		// Create the namespace
		if tc.existingConsulNamespace != "" && tc.existingConsulNamespace != "default" {
			_, err = resourceClient.Write(context.Background(), &pbresource.WriteRequest{Resource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name:    tc.existingConsulNamespace,
					Type:    pbtenancy.NamespaceType,
					Tenancy: &pbresource.Tenancy{Partition: tc.partition},
				},
			}})
			require.NoError(t, err, "failed to create namespace")
			// TODO: require.ResourceExists()
		}

		// Create the namespace controller.
		nc := &Controller{
			Client:                     fakeClient,
			Log:                        logrtest.New(t),
			ConsulClientConfig:         testClient.Cfg,
			ConsulServerConnMgr:        testClient.Watcher,
			AllowK8sNamespacesSet:      mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:       mapset.NewSetWith(),
			EnableNSMirroring:          tc.enableNSMirroring,
			NSMirroringPrefix:          tc.nsMirrorPrefix,
			ConsulDestinationNamespace: tc.destinationNamespace,
		}

		if tc.kubeNamespaceName == "" {
			tc.kubeNamespaceName = testNamespaceName
		}

		namespacedName := types.NamespacedName{
			Name: tc.kubeNamespaceName,
		}

		resp, err := nc.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: namespacedName,
		})
		require.NoError(t, err)
		require.False(t, resp.Requeue)

		if tc.existingConsulNamespace != "" {
			expectedNamespaceMatches2(t, resourceClient, tc.partition, tc.existingConsulNamespace, tc.expectedConsulNamespace)
		} else {
			expectedNamespaceMatches2(t, resourceClient, tc.partition, testNamespaceName, tc.expectedConsulNamespace)
		}
	}

	testCases := []testCase{
		// {
		// 	name:                    "destination namespace with default is not cleaned up",
		// 	partition:               "default",
		// 	existingConsulNamespace: "default",
		// 	expectedConsulNamespace: "default",
		// },
		// {
		// 	name:                    "destination namespace with non-default is not cleaned up",
		// 	partition:               "default",
		// 	destinationNamespace:    "ns1",
		// 	existingConsulNamespace: "ns1",
		// 	expectedConsulNamespace: "ns1",
		// },
		// {
		// 	name:                    "destination namespace with non-default is not cleaned up, non-default partition",
		// 	partition:               "ap1",
		// 	destinationNamespace:    "ns1",
		// 	existingConsulNamespace: "ns1",
		// 	expectedConsulNamespace: "ns1",
		// },
		{
			name:                    "mirrored namespaces",
			partition:               "default",
			enableNSMirroring:       true,
			existingConsulNamespace: "foo",
		},
		// {
		// 	name:                    "mirrored namespaces but it's the default namespace",
		// 	kubeNamespaceName:       metav1.NamespaceDefault,
		// 	enableNSMirroring:       true,
		// 	existingConsulNamespace: getNamespace(constants.DefaultConsulNS, "", false),
		// 	expectedConsulNamespace: getNamespace(constants.DefaultConsulNS, "", false), // Don't ever delete the Consul default NS
		// },
		// {
		// 	name:                    "mirrored namespaces, non-default partition",
		// 	partition:               "baz",
		// 	enableNSMirroring:       true,
		// 	existingConsulNamespace: getNamespace(testNamespaceName, "baz", false),
		// },
		// {
		// 	name:                    "mirrored namespaces with prefix",
		// 	nsMirrorPrefix:          "k8s-",
		// 	enableNSMirroring:       true,
		// 	existingConsulNamespace: getNamespace("k8s-foo", "", false),
		// },
		// {
		// 	name:                    "mirrored namespaces with prefix, non-default partition",
		// 	partition:               "baz",
		// 	nsMirrorPrefix:          "k8s-",
		// 	enableNSMirroring:       true,
		// 	existingConsulNamespace: getNamespace("k8s-foo", "baz", false),
		// },
		// {
		// 	name:                    "mirrored namespaces overrides destination namespace",
		// 	enableNSMirroring:       true,
		// 	destinationNamespace:    "baz",
		// 	existingConsulNamespace: getNamespace(testNamespaceName, "", false),
		// },
		// {
		// 	name:              "mirrored namespace, but the namespace is already removed from Consul",
		// 	enableNSMirroring: true,
		// },
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

// getNamespace return a basic Consul V1 namespace for testing setup and comparison
// func getNamespace(name string, partition string, acls bool) *capi.Namespace {
// 	ns := &capi.Namespace{
// 		Name:      name,
// 		Partition: partition,
// 	}

// 	if name != constants.DefaultConsulNS {
// 		ns.Description = "Auto-generated by consul-k8s"
// 		ns.Meta = map[string]string{"external-source": "kubernetes"}
// 		ns.ACLs = &capi.NamespaceACLConfig{}
// 	} else {
// 		ns.Description = "Builtin Default Namespace"
// 	}

// 	if acls && name != constants.DefaultConsulNS {
// 		// Create the ACLs config for the cross-Consul-namespace
// 		// default policy that needs to be attached
// 		ns.ACLs = &capi.NamespaceACLConfig{
// 			PolicyDefaults: []capi.ACLLink{
// 				{Name: testCrossACLPolicy},
// 			},
// 		}
// 	}

// 	return ns
// }

func getNamespace2(ap, ns string) *pbresource.Resource {
	return &pbresource.Resource{Id: &pbresource.ID{
		Name:    ns,
		Type:    pbtenancy.NamespaceType,
		Tenancy: &pbresource.Tenancy{Partition: ap},
	}}
}

func expectedNamespaceMatches(t *testing.T, client *capi.Client, name string, partition string, expectedNamespace *capi.Namespace) {
	namespaceInfo, _, err := client.Namespaces().Read(name, &capi.QueryOptions{Partition: partition})

	require.NoError(t, err)

	if expectedNamespace == nil {
		require.True(t, namespaceInfo == nil || namespaceInfo.DeletedAt != nil)
		return
	}

	require.NotNil(t, namespaceInfo)
	// Zero out the Raft Index, in this case it is irrelevant.
	namespaceInfo.CreateIndex = 0
	namespaceInfo.ModifyIndex = 0
	if namespaceInfo.ACLs != nil && len(namespaceInfo.ACLs.PolicyDefaults) > 0 {
		namespaceInfo.ACLs.PolicyDefaults[0].ID = "" // Zero out the ID for ACLs enabled to facilitate testing.
	}
	require.Equal(t, *expectedNamespace, *namespaceInfo)
}

func expectedNamespaceMatches2(t *testing.T, resourceClient pbresource.ResourceServiceClient, ap, ns, expectedNamespace string) {
	_, err := resourceClient.Read(context.Background(), &pbresource.ReadRequest{Id: &pbresource.ID{
		Name:    ns,
		Type:    pbtenancy.NamespaceType,
		Tenancy: &pbresource.Tenancy{Partition: ap},
	}})
	//require.NoError(t, err)

	//namespaceInfo, _, err := client.Namespaces().Read(name, &capi.QueryOptions{Partition: partition})
	//require.NoError(t, err)

	if expectedNamespace == "" {
		require.Error(t, err)
		require.True(t, status.Code(err) == codes.NotFound)
		return
	}

	require.NoError(t, err)

	// Zero out the Raft Index, in this case it is irrelevant.
	//namespaceInfo.CreateIndex = 0
	//namespaceInfo.ModifyIndex = 0
	//if namespaceInfo.ACLs != nil && len(namespaceInfo.ACLs.PolicyDefaults) > 0 {
	//	namespaceInfo.ACLs.PolicyDefaults[0].ID = "" // Zero out the ID for ACLs enabled to facilitate testing.
	//}
	//require.Equal(t, *expectedNamespace, *namespaceInfo)
}
