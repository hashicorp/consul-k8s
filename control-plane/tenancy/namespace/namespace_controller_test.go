// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package namespace

import (
	"context"
	"testing"
	"time"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul/proto-public/pbresource"
	pbtenancy "github.com/hashicorp/consul/proto-public/pbtenancy/v2beta1"
	"github.com/hashicorp/consul/sdk/testutil"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestReconcileCreateNamespace(t *testing.T) {
	testCases := []createTestCase{
		{
			name:                    "consul namespace is default/ns1",
			kubeNamespace:           "ns1",
			partition:               constants.DefaultConsulPartition,
			expectedConsulNamespace: "ns1",
		},
	}
	testReconcileCreateNamespace(t, testCases)
}

type createTestCase struct {
	name                    string
	kubeNamespace           string
	partition               string
	expectedConsulNamespace string
}

// testReconcileCreateNamespace ensures that a new k8s namespace is reconciled to a
// Consul namespace. The actual namespace in Consul depends on if the controller
// is configured with a destination namespace or mirroring enabled.
func testReconcileCreateNamespace(t *testing.T, testCases []createTestCase) {
	run := func(t *testing.T, tc createTestCase) {
		// Create the default kube namespace and kube namespace under test.
		kubeNS := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: tc.kubeNamespace}}
		kubeDefaultNS := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: metav1.NamespaceDefault}}
		kubeObjects := []client.Object{
			&kubeNS,
			&kubeDefaultNS,
		}
		fakeClient := fake.NewClientBuilder().
			WithObjects(kubeObjects...).
			WithStatusSubresource(kubeObjects...).
			Build()

		// Fire up consul server with v2tenancy enabled
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis", "v2tenancy"}
		})

		// Create partition if needed
		testClient.Cfg.APIClientConfig.Partition = tc.partition
		if tc.partition != "" && tc.partition != "default" {
			_, err := testClient.ResourceClient.Write(context.Background(), &pbresource.WriteRequest{Resource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: tc.partition,
					Type: pbtenancy.PartitionType,
				},
			}})
			require.NoError(t, err, "failed to create partition")
		}

		// Create the namespace controller injecting config from tc
		nc := &Controller{
			Client:              fakeClient,
			ConsulServerConnMgr: testClient.Watcher,
			K8sNamespaceConfig: common.K8sNamespaceConfig{
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			},
			ConsulTenancyConfig: common.ConsulTenancyConfig{
				ConsulPartition: tc.partition,
			},
			Log: logrtest.New(t),
		}

		// Reconcile the kube namespace under test
		resp, err := nc.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name: tc.kubeNamespace,
			},
		})
		require.NoError(t, err)
		require.False(t, resp.Requeue)

		// Verify consul namespace exists or was created during reconciliation
		_, err = testClient.ResourceClient.Read(context.Background(), &pbresource.ReadRequest{
			Id: &pbresource.ID{
				Name:    tc.expectedConsulNamespace,
				Type:    pbtenancy.NamespaceType,
				Tenancy: &pbresource.Tenancy{Partition: tc.partition},
			},
		})
		require.NoError(t, err, "expected partition/namespace %s/%s to exist", tc.partition, tc.expectedConsulNamespace)
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

func TestReconcileDeleteNamespace(t *testing.T) {
	testCases := []deleteTestCase{
		{
			name:                    "consul namespace ns1",
			kubeNamespace:           "ns1",
			partition:               "default",
			existingConsulNamespace: "ns1",
			expectNamespaceDeleted:  "ns1",
		},
		{
			name:                    "consul default namespace does not get deleted",
			kubeNamespace:           metav1.NamespaceDefault,
			partition:               "default",
			existingConsulNamespace: "",
			expectNamespaceExists:   "default",
		},
		{
			name:                    "namespace is already removed from Consul",
			kubeNamespace:           "ns1",
			partition:               "default",
			existingConsulNamespace: "",    // don't pre-create consul namespace
			expectNamespaceDeleted:  "ns1", // read as "was never created"
		},
	}
	testReconcileDeleteNamespace(t, testCases)
}

type deleteTestCase struct {
	name                    string
	kubeNamespace           string
	partition               string
	existingConsulNamespace string // If non-empty, this namespace is created in consul pre-reconcile

	// Pick one
	expectNamespaceExists  string // If non-empty, this namespace should exist in consul post-reconcile
	expectNamespaceDeleted string // If non-empty, this namespace should not exist in consul post-reconcile
}

// Tests deleting a Namespace object, with and without matching Consul namespace.
func testReconcileDeleteNamespace(t *testing.T, testCases []deleteTestCase) {
	run := func(t *testing.T, tc deleteTestCase) {
		// Don't seed with any kube namespaces since we're testing deletion.
		fakeClient := fake.NewClientBuilder().WithRuntimeObjects().Build()

		// Fire up consul server with v2tenancy enabled
		testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
			c.Experiments = []string{"resource-apis", "v2tenancy"}
		})

		// Create partition if needed
		testClient.Cfg.APIClientConfig.Partition = tc.partition
		if tc.partition != "" && tc.partition != "default" {
			_, err := testClient.ResourceClient.Write(context.Background(), &pbresource.WriteRequest{Resource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: tc.partition,
					Type: pbtenancy.PartitionType,
				},
			}})
			require.NoError(t, err, "failed to create partition")
		}

		// Create the consul namespace if needed
		if tc.existingConsulNamespace != "" && tc.existingConsulNamespace != "default" {
			id := &pbresource.ID{
				Name:    tc.existingConsulNamespace,
				Type:    pbtenancy.NamespaceType,
				Tenancy: &pbresource.Tenancy{Partition: tc.partition},
			}

			rsp, err := testClient.ResourceClient.Write(context.Background(), &pbresource.WriteRequest{Resource: &pbresource.Resource{Id: id}})
			require.NoError(t, err, "failed to create namespace")

			// TODO: Remove after https://hashicorp.atlassian.net/browse/NET-6719 implemented
			requireEventuallyAccepted(t, testClient.ResourceClient, rsp.Resource.Id)
		}

		// Create the namespace controller.
		nc := &Controller{
			Client:              fakeClient,
			ConsulServerConnMgr: testClient.Watcher,
			K8sNamespaceConfig: common.K8sNamespaceConfig{
				AllowK8sNamespacesSet: mapset.NewSetWith("*"),
				DenyK8sNamespacesSet:  mapset.NewSetWith(),
			},
			ConsulTenancyConfig: common.ConsulTenancyConfig{
				ConsulPartition: tc.partition,
			},
			Log: logrtest.New(t),
		}

		// Reconcile the kube namespace under test - imagine it has just been deleted
		resp, err := nc.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name: tc.kubeNamespace,
			},
		})
		require.NoError(t, err)
		require.False(t, resp.Requeue)

		// Verify appropriate action was taken on the counterpart consul namespace
		if tc.expectNamespaceExists != "" {
			// Verify consul namespace was not deleted
			_, err = testClient.ResourceClient.Read(context.Background(), &pbresource.ReadRequest{
				Id: &pbresource.ID{
					Name:    tc.expectNamespaceExists,
					Type:    pbtenancy.NamespaceType,
					Tenancy: &pbresource.Tenancy{Partition: tc.partition},
				},
			})
			require.NoError(t, err, "expected partition/namespace %s/%s to exist", tc.partition, tc.expectNamespaceExists)
		} else if tc.expectNamespaceDeleted != "" {
			// Verify consul namespace was deleted
			id := &pbresource.ID{
				Name:    tc.expectNamespaceDeleted,
				Type:    pbtenancy.NamespaceType,
				Tenancy: &pbresource.Tenancy{Partition: tc.partition},
			}
			requireEventuallyNotFound(t, testClient.ResourceClient, id)
		} else {
			panic("tc.expectedNamespaceExists or tc.expectedNamespaceDeleted must be set")
		}
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			run(t, tc)
		})
	}
}

// RequireStatusAccepted waits for a recently created resource to have a resource status of accepted so that
// attempts to delete it by the single-shot controller under test's reconcile will not fail with a CAS error.
//
// Remove refs to this after https://hashicorp.atlassian.net/browse/NET-6719 is implemented.
func requireEventuallyAccepted(t *testing.T, resourceClient pbresource.ResourceServiceClient, id *pbresource.ID) {
	require.Eventuallyf(t,
		func() bool {
			rsp, err := resourceClient.Read(context.Background(), &pbresource.ReadRequest{Id: id})
			if err != nil {
				return false
			}
			if len(rsp.Resource.Status) == 0 {
				return false
			}

			for _, status := range rsp.Resource.Status {
				for _, condition := range status.Conditions {
					// common.ConditionAccepted in consul namespace controller
					if condition.Type == "accepted" && condition.State == pbresource.Condition_STATE_TRUE {
						return true
					}
				}
			}
			return false
		},
		time.Second*5,
		time.Millisecond*100,
		"timed out out waiting for %s to have status accepted",
		id,
	)
}

func requireEventuallyNotFound(t *testing.T, resourceClient pbresource.ResourceServiceClient, id *pbresource.ID) {
	// allow both "not found" and "marked for deletion" so we're not waiting around unnecessarily
	require.Eventuallyf(t, func() bool {
		rsp, err := resourceClient.Read(context.Background(), &pbresource.ReadRequest{Id: id})
		if err == nil {
			return isMarkedForDeletion(rsp.Resource)
		}
		if status.Code(err) == codes.NotFound {
			return true
		}
		return false
	},
		time.Second*5,
		time.Millisecond*100,
		"timed out waiting for %s to not be found",
		id,
	)
}
