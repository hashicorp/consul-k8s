// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serviceaccount

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/proto"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	pbauth "github.com/hashicorp/consul/proto-public/pbauth/v2beta1"
	"github.com/hashicorp/consul/proto-public/pbresource"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	inject "github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

type reconcileCase struct {
	name                  string
	svcAccountName        string
	k8sObjects            func() []runtime.Object
	existingResource      *pbresource.Resource
	expectedResource      *pbresource.Resource
	targetConsulNs        string
	targetConsulPartition string
	expErr                string
}

// TODO(NET-5719): Allow/deny namespaces for reconcile tests

// TestReconcile_CreateWorkloadIdentity ensures that a new ServiceAccount is reconciled
// to a Consul WorkloadIdentity.
func TestReconcile_CreateWorkloadIdentity(t *testing.T) {
	t.Parallel()
	cases := []reconcileCase{
		{
			name:           "Default ServiceAccount not synced",
			svcAccountName: "default",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{createServiceAccount("default", "default")}
			},
		},
		{
			name:           "Custom ServiceAccount",
			svcAccountName: "my-svc-account",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{
					createServiceAccount("default", "default"),
					createServiceAccount("my-svc-account", "default"),
				}
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "my-svc-account",
					Type: pbauth.WorkloadIdentityType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: getWorkloadIdentityData(),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByServiceAccountValue,
				},
			},
		},
		{
			name:           "Already exists",
			svcAccountName: "my-svc-account",
			k8sObjects: func() []runtime.Object {
				return []runtime.Object{
					createServiceAccount("default", "default"),
					createServiceAccount("my-svc-account", "default"),
				}
			},
			existingResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "my-svc-account",
					Type: pbauth.WorkloadIdentityType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: getWorkloadIdentityData(),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByServiceAccountValue,
				},
			},
			expectedResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "my-svc-account",
					Type: pbauth.WorkloadIdentityType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: getWorkloadIdentityData(),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByServiceAccountValue,
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runReconcileCase(t, tc)
		})
	}
}

// Tests deleting a WorkloadIdentity object, with and without matching Consul resources.
func TestReconcile_DeleteWorkloadIdentity(t *testing.T) {
	t.Parallel()
	cases := []reconcileCase{
		{
			name:           "Basic ServiceAccount not found (deleted)",
			svcAccountName: "my-svc-account",
			k8sObjects: func() []runtime.Object {
				// Only default exists (always exists).
				return []runtime.Object{createServiceAccount("default", "default")}
			},
			existingResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "my-svc-account",
					Type: pbauth.WorkloadIdentityType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: getWorkloadIdentityData(),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByServiceAccountValue,
				},
			},
		},
		{
			name:           "Other ServiceAccount exists",
			svcAccountName: "my-svc-account",
			k8sObjects: func() []runtime.Object {
				// Default and other ServiceAccount exist
				return []runtime.Object{
					createServiceAccount("default", "default"),
					createServiceAccount("other-svc-account", "default"),
				}
			},
			existingResource: &pbresource.Resource{
				Id: &pbresource.ID{
					Name: "my-svc-account",
					Type: pbauth.WorkloadIdentityType,
					Tenancy: &pbresource.Tenancy{
						Namespace: constants.DefaultConsulNS,
						Partition: constants.DefaultConsulPartition,
					},
				},
				Data: getWorkloadIdentityData(),
				Metadata: map[string]string{
					constants.MetaKeyKubeNS:    constants.DefaultConsulNS,
					constants.MetaKeyManagedBy: constants.ManagedByServiceAccountValue,
				},
			},
		},
		{
			name:           "Already deleted",
			svcAccountName: "my-svc-account",
			k8sObjects: func() []runtime.Object {
				// Only default exists (always exists).
				return []runtime.Object{createServiceAccount("default", "default")}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runReconcileCase(t, tc)
		})
	}
}

func runReconcileCase(t *testing.T, tc reconcileCase) {
	t.Helper()

	// Create fake k8s client
	var k8sObjects []runtime.Object
	if tc.k8sObjects != nil {
		k8sObjects = tc.k8sObjects()
	}
	fakeClient := fake.NewClientBuilder().WithRuntimeObjects(k8sObjects...).Build()

	// Create test Consul server.
	testClient := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.Experiments = []string{"resource-apis"}
	})

	// Create the ServiceAccount controller.
	sa := &Controller{
		Client:              fakeClient,
		Log:                 logrtest.New(t),
		ConsulServerConnMgr: testClient.Watcher,
		K8sNamespaceConfig: common.K8sNamespaceConfig{
			AllowK8sNamespacesSet: mapset.NewSetWith("*"),
			DenyK8sNamespacesSet:  mapset.NewSetWith(),
		},
	}

	// Default ns and partition if not specified in test.
	if tc.targetConsulNs == "" {
		tc.targetConsulNs = constants.DefaultConsulNS
	}
	if tc.targetConsulPartition == "" {
		tc.targetConsulPartition = constants.DefaultConsulPartition
	}

	// If existing resource specified, create it and ensure it exists.
	if tc.existingResource != nil {
		writeReq := &pbresource.WriteRequest{Resource: tc.existingResource}
		_, err := testClient.ResourceClient.Write(context.Background(), writeReq)
		require.NoError(t, err)
		test.ResourceHasPersisted(t, context.Background(), testClient.ResourceClient, tc.existingResource.Id)
	}

	// Run actual reconcile and verify results.
	resp, err := sa.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      tc.svcAccountName,
			Namespace: tc.targetConsulNs,
		},
	})
	if tc.expErr != "" {
		require.ErrorContains(t, err, tc.expErr)
	} else {
		require.NoError(t, err)
	}
	require.False(t, resp.Requeue)

	expectedWorkloadIdentityMatches(t, testClient.ResourceClient, tc.svcAccountName, tc.targetConsulNs, tc.targetConsulPartition, tc.expectedResource)
}

func expectedWorkloadIdentityMatches(t *testing.T, client pbresource.ResourceServiceClient, name, namespace, partition string, expectedResource *pbresource.Resource) {
	req := &pbresource.ReadRequest{Id: getWorkloadIdentityID(name, namespace, partition)}

	res, err := client.Read(context.Background(), req)

	if expectedResource == nil {
		require.Error(t, err)
		s, ok := status.FromError(err)
		require.True(t, ok)
		require.Equal(t, codes.NotFound, s.Code())
		return
	}

	require.NoError(t, err)
	require.NotNil(t, res)
	require.NotNil(t, res.GetResource().GetData())

	// This equality check isn't technically necessary because WorkloadIdentity is an empty message,
	// but this supports the addition of fields in the future.
	expectedWorkloadIdentity := &pbauth.WorkloadIdentity{}
	err = anypb.UnmarshalTo(expectedResource.Data, expectedWorkloadIdentity, proto.UnmarshalOptions{})
	require.NoError(t, err)

	actualWorkloadIdentity := &pbauth.WorkloadIdentity{}
	err = res.GetResource().GetData().UnmarshalTo(actualWorkloadIdentity)
	require.NoError(t, err)

	if diff := cmp.Diff(expectedWorkloadIdentity, actualWorkloadIdentity, test.CmpProtoIgnoreOrder()...); diff != "" {
		t.Errorf("unexpected difference:\n%v", diff)
	}
}

// getWorkloadIdentityData returns a WorkloadIdentity resource payload.
// This function takes no arguments because WorkloadIdentity is currently an empty proto message.
func getWorkloadIdentityData() *anypb.Any {
	return inject.ToProtoAny(&pbauth.WorkloadIdentity{})
}

func createServiceAccount(name, namespace string) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		// Other fields exist, but we ignore them in this controller.
	}
}
