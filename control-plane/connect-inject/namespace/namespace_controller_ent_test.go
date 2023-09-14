// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package namespace

import (
	"context"
	"testing"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

const (
	testNamespaceName  = "foo"
	testCrossACLPolicy = "cross-namespace-policy"
)

// TestReconcileCreateNamespace ensures that a new namespace is reconciled to a
// Consul namespace. The actual namespace in Consul depends on if the controller
// is configured with a destination namespace or mirroring enabled.
func TestReconcileCreateNamespace(t *testing.T) {
	t.Parallel()

	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: testNamespaceName,
	}}
	nsDefault := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: metav1.NamespaceDefault,
	}}

	type testCase struct {
		name              string
		kubeNamespaceName string // this will default to "foo"
		partition         string

		consulDestinationNamespace string
		enableNSMirroring          bool
		nsMirrorPrefix             string

		expectedConsulNamespaceName string
		expectedConsulNamespace     *capi.Namespace

		acls   bool
		expErr string
	}

	run := func(t *testing.T, tc testCase) {
		k8sObjects := []runtime.Object{
			&ns,
			&nsDefault,
		}
		fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

		// Create test consulServer server.
		adminToken := "123e4567-e89b-12d3-a456-426614174000"
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis"}
			if tc.acls {
				c.ACL.Enabled = tc.acls
				c.ACL.Tokens.InitialManagement = adminToken
			}
		})

		if tc.partition != "" {
			testClient.Cfg.APIClientConfig.Partition = tc.partition

			partition := &capi.Partition{
				Name: tc.partition,
			}
			_, _, err := testClient.APIClient.Partitions().Create(context.Background(), partition, nil)
			require.NoError(t, err)
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
			ConsulDestinationNamespace: tc.consulDestinationNamespace,
		}
		if tc.acls {
			nc.CrossNamespaceACLPolicy = testCrossACLPolicy

			policy := &capi.ACLPolicy{Name: testCrossACLPolicy}
			_, _, err := testClient.APIClient.ACL().PolicyCreate(policy, nil)
			require.NoError(t, err)
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
		if tc.expErr != "" {
			require.EqualError(t, err, tc.expErr)
		} else {
			require.NoError(t, err)
		}
		require.False(t, resp.Requeue)

		expectedNamespaceMatches(t, testClient.APIClient, tc.expectedConsulNamespaceName, tc.partition, tc.expectedConsulNamespace)
	}

	testCases := []testCase{
		{
			// This also tests that we don't overwrite anything about the default Consul namespace,
			// because the original description is maintained.
			name:                        "destination namespace default",
			expectedConsulNamespaceName: constants.DefaultConsulNS,
			expectedConsulNamespace:     getNamespace(constants.DefaultConsulNS, "", false),
		},
		{
			name:                        "destination namespace, non-default",
			consulDestinationNamespace:  "bar",
			expectedConsulNamespaceName: "bar",
			expectedConsulNamespace:     getNamespace("bar", "", false),
		},
		{
			name:                        "destination namespace, non-default with ACLs enabled",
			consulDestinationNamespace:  "bar",
			acls:                        true,
			expectedConsulNamespaceName: "bar",
			expectedConsulNamespace:     getNamespace("bar", constants.DefaultConsulPartition, true), // For some reason, we the partition is returned by Consul in this case, even though it is default
		},
		{
			name:                        "destination namespace, non-default, non-default partition",
			partition:                   "baz",
			consulDestinationNamespace:  "bar",
			expectedConsulNamespaceName: "bar",
			expectedConsulNamespace:     getNamespace("bar", "baz", false),
		},
		{
			name:                        "mirrored namespaces",
			enableNSMirroring:           true,
			expectedConsulNamespaceName: testNamespaceName,
			expectedConsulNamespace:     getNamespace(testNamespaceName, "", false),
		},
		{
			name:                        "mirrored namespaces, non-default partition",
			partition:                   "baz",
			enableNSMirroring:           true,
			expectedConsulNamespaceName: testNamespaceName,
			expectedConsulNamespace:     getNamespace(testNamespaceName, "baz", false),
		},
		{
			name:                        "mirrored namespaces with acls",
			acls:                        true,
			enableNSMirroring:           true,
			expectedConsulNamespaceName: testNamespaceName,
			expectedConsulNamespace:     getNamespace(testNamespaceName, constants.DefaultConsulPartition, true), // For some reason, we the partition is returned by Consul in this case, even though it is default
		},
		{
			name:                        "mirrored namespaces with prefix",
			nsMirrorPrefix:              "k8s-",
			enableNSMirroring:           true,
			expectedConsulNamespaceName: "k8s-foo",
			expectedConsulNamespace:     getNamespace("k8s-foo", "", false),
		},
		{
			name:                        "mirrored namespaces with prefix, non-default partition",
			nsMirrorPrefix:              "k8s-",
			partition:                   "baz",
			enableNSMirroring:           true,
			expectedConsulNamespaceName: "k8s-foo",
			expectedConsulNamespace:     getNamespace("k8s-foo", "baz", false),
		},
		{
			name:                        "mirrored namespaces with prefix and acls",
			nsMirrorPrefix:              "k8s-",
			acls:                        true,
			enableNSMirroring:           true,
			expectedConsulNamespaceName: "k8s-foo",
			expectedConsulNamespace:     getNamespace("k8s-foo", constants.DefaultConsulPartition, true), // For some reason, we the partition is returned by Consul in this case, even though it is default
		},
		{
			name:                        "mirrored namespaces overrides destination namespace",
			enableNSMirroring:           true,
			consulDestinationNamespace:  "baz",
			expectedConsulNamespaceName: testNamespaceName,
			expectedConsulNamespace:     getNamespace(testNamespaceName, "", false),
		},
		{
			name:                        "ignore kube-system",
			kubeNamespaceName:           metav1.NamespaceSystem,
			consulDestinationNamespace:  "bar",
			expectedConsulNamespaceName: "bar", // we make sure that this doesn't get created from the kube-system space by not providing the actual struct
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

// Tests deleting a Namespace object, with and without matching Consul resources.
func TestReconcileDeleteNamespace(t *testing.T) {
	t.Parallel()

	type testCase struct {
		name              string
		kubeNamespaceName string // this will default to "foo"
		partition         string

		destinationNamespace string
		enableNSMirroring    bool
		nsMirrorPrefix       string

		existingConsulNamespace *capi.Namespace

		expectedConsulNamespace *capi.Namespace
	}

	run := func(t *testing.T, tc testCase) {
		fakeClient := fake.NewClientBuilder().WithRuntimeObjects().Build()

		// Create test consulServer server.
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis"}
		})

		if tc.partition != "" {
			testClient.Cfg.APIClientConfig.Partition = tc.partition

			partition := &capi.Partition{
				Name: tc.partition,
			}
			_, _, err := testClient.APIClient.Partitions().Create(context.Background(), partition, nil)
			require.NoError(t, err)
		}

		if tc.existingConsulNamespace != nil {
			_, _, err := testClient.APIClient.Namespaces().Create(tc.existingConsulNamespace, nil)
			require.NoError(t, err)
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

		if tc.existingConsulNamespace != nil {
			expectedNamespaceMatches(t, testClient.APIClient, tc.existingConsulNamespace.Name, tc.partition, tc.expectedConsulNamespace)
		} else {
			expectedNamespaceMatches(t, testClient.APIClient, testNamespaceName, tc.partition, tc.expectedConsulNamespace)
		}
	}

	testCases := []testCase{
		{
			name:                    "destination namespace with default is not cleaned up",
			existingConsulNamespace: getNamespace(constants.DefaultConsulNS, "", false),
			expectedConsulNamespace: getNamespace(constants.DefaultConsulNS, "", false),
		},
		{
			name:                    "destination namespace with non-default is not cleaned up",
			destinationNamespace:    "bar",
			existingConsulNamespace: getNamespace("bar", "", false),
			expectedConsulNamespace: getNamespace("bar", "", false),
		},
		{
			name:                    "destination namespace with non-default is not cleaned up, non-default partition",
			destinationNamespace:    "bar",
			partition:               "baz",
			existingConsulNamespace: getNamespace("bar", "baz", false),
			expectedConsulNamespace: getNamespace("bar", "baz", false),
		},
		{
			name:                    "mirrored namespaces",
			enableNSMirroring:       true,
			existingConsulNamespace: getNamespace(testNamespaceName, "", false),
		},
		{
			name:                    "mirrored namespaces but it's the default namespace",
			kubeNamespaceName:       metav1.NamespaceDefault,
			enableNSMirroring:       true,
			existingConsulNamespace: getNamespace(constants.DefaultConsulNS, "", false),
			expectedConsulNamespace: getNamespace(constants.DefaultConsulNS, "", false), // Don't ever delete the Consul default NS
		},
		{
			name:                    "mirrored namespaces, non-default partition",
			partition:               "baz",
			enableNSMirroring:       true,
			existingConsulNamespace: getNamespace(testNamespaceName, "baz", false),
		},
		{
			name:                    "mirrored namespaces with prefix",
			nsMirrorPrefix:          "k8s-",
			enableNSMirroring:       true,
			existingConsulNamespace: getNamespace("k8s-foo", "", false),
		},
		{
			name:                    "mirrored namespaces with prefix, non-default partition",
			partition:               "baz",
			nsMirrorPrefix:          "k8s-",
			enableNSMirroring:       true,
			existingConsulNamespace: getNamespace("k8s-foo", "baz", false),
		},
		{
			name:                    "mirrored namespaces overrides destination namespace",
			enableNSMirroring:       true,
			destinationNamespace:    "baz",
			existingConsulNamespace: getNamespace(testNamespaceName, "", false),
		},
		{
			name:              "mirrored namespace, but the namespace is already removed from Consul",
			enableNSMirroring: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

// getNamespace return a basic Consul V1 namespace for testing setup and comparison
func getNamespace(name string, partition string, acls bool) *capi.Namespace {
	ns := &capi.Namespace{
		Name:      name,
		Partition: partition,
	}

	if name != constants.DefaultConsulNS {
		ns.Description = "Auto-generated by consul-k8s"
		ns.Meta = map[string]string{"external-source": "kubernetes"}
		ns.ACLs = &capi.NamespaceACLConfig{}
	} else {
		ns.Description = "Builtin Default Namespace"
	}

	if acls && name != constants.DefaultConsulNS {
		// Create the ACLs config for the cross-Consul-namespace
		// default policy that needs to be attached
		ns.ACLs = &capi.NamespaceACLConfig{
			PolicyDefaults: []capi.ACLLink{
				{Name: testCrossACLPolicy},
			},
		}
	}

	return ns
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
