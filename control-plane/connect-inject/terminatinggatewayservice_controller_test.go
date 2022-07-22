package connectinject

import (
	"context"
	"errors"
	"fmt"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"testing"
)

// TestReconcileCreateTerminatingGatewayService creates a terminating gateway service.
func TestReconcileCreateTerminatingGatewayService(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec   *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled     bool
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: false,
		},
		{
			name: "New TerminatingGatewayService registers an external service with Consul and updates terminating gateway token with new write policy",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
				if tt.aclEnabled {
					// Start Consul server with ACLs enabled and default deny policy.
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.InitialManagement = masterToken
				}
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			if tt.aclEnabled {
				// Create fake consul client with fake terminating gateway policy.

				fakeTermGtwPolicy := &api.ACLPolicy{
					Name:        "1b9a40e2-4b80-0b45-4ee4-0a47592fe386-terminating-gateway",
					Description: "ACL Policy for terminating gateway",
				}

				termGtwPolicy, _, err := consulClient.ACL().PolicyCreate(fakeTermGtwPolicy, nil)
				require.NoError(t, err)

				fakeConsulClientPolicies := []*api.ACLRolePolicyLink{
					{
						ID:   termGtwPolicy.ID,
						Name: termGtwPolicy.Name,
					},
				}

				fakeConsulClientRole := &api.ACLRole{
					Description: "ACL Role for consul-client",
					Policies:    fakeConsulClientPolicies,
					Name:        "terminating-gateway-acl-role",
				}

				_, _, err = consulClient.ACL().RoleCreate(fakeConsulClientRole, nil)
				require.NoError(t, err)
			}

			// create the terminating gateway service controller.
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}
			namespacedName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}

			resp, _ := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})

			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			serviceName := "TerminatingGatewayServiceController-created-service"
			service, serviceExists := serviceFound("TerminatingGatewayServiceController-created-service", consulClient)

			require.True(t, serviceExists)

			policyName := ""
			if tt.aclEnabled {
				policyName = fmt.Sprintf("%s-write-policy", serviceName)
			}

			require.Equal(t, tt.expectedSpec.Service.ServiceName, service.ServiceName)
			require.Equal(t, tt.expectedSpec.Service.ServicePort, service.ServicePort)
			require.Equal(t, tt.expectedSpec.Service.Node, service.Node)
			require.Equal(t, tt.expectedSpec.Service.Address, service.Address)

			if tt.aclEnabled {
				aclToken, aclTokenFound := fetchTerminatingGatewayToken(consulClient)
				allPolicies := aclToken.Policies
				policyIndex, policyFound := findAclPolicy(policyName, allPolicies)

				require.True(t, policyFound)

				matchedPolicyFromToken := allPolicies[policyIndex]

				policyFromConsul, _, errWithPolicyFromConsul := consulClient.ACL().PolicyRead(matchedPolicyFromToken.ID, &api.QueryOptions{})
				require.NoError(t, errWithPolicyFromConsul)

				require.True(t, aclTokenFound)

				require.Equal(t, allPolicies[policyIndex].Name, policyFromConsul.Name)
			}

			require.Equal(t, tt.expectedStatus.ServiceInfoRef.ServiceName, service.ServiceName)
			require.Equal(t, tt.expectedStatus.ServiceInfoRef.PolicyName, policyName)
		})
	}
}

// TestReconcile_DeleteTerminatingGatewayService reconciles a TerminatingGatewayService resource that is no longer in Kubernetes, but still
// exists in Consul.
func TestReconcile_DeleteTerminatingGatewayService(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expErr         string
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec   *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled     bool
	}{
		{
			name: "Delete TerminatingGatewayService that registers an external service with Consul",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: false,
		},
		{
			name: "Delete TerminatingGatewayService that registers an external service with Consul and updates terminating gateway token with new write policy",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: true,
		},
	}
	for _, tt := range cases {
		// Add the default namespace.
		ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
		terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
			ObjectMeta: metav1.ObjectMeta{
				Name:              "Terminating gateway service-deleted",
				Namespace:         "default",
				DeletionTimestamp: &metav1.Time{Time: time.Now()},
				Finalizers:        []string{FinalizerName},
			},
			Spec: v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
		}
		k8sObjects := []runtime.Object{&ns, terminatingGatewayService}

		// Add terminating gateway service types to the scheme.
		s := scheme.Scheme
		s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
		fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

		masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

		// Create test consul server.
		consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
			c.NodeName = nodeName
			if tt.aclEnabled {
				// Start Consul server with ACLs enabled and default deny policy.
				c.ACL.Enabled = true
				c.ACL.DefaultPolicy = "deny"
				c.ACL.Tokens.InitialManagement = masterToken
			}

		})
		require.NoError(t, err)
		defer consul.Stop()
		consul.WaitForServiceIntentions(t)

		cfg := &api.Config{
			Address: consul.HTTPAddr,
			Token:   masterToken,
		}
		consulClient, err := api.NewClient(cfg)
		require.NoError(t, err)

		if tt.aclEnabled {
			// Create fake consul client with fake terminating gateway policy.

			fakeTermGtwPolicy := &api.ACLPolicy{
				Name:        "1b9a40e2-4b80-0b45-4ee4-0a47592fe386-terminating-gateway",
				Description: "ACL Policy for terminating gateway",
			}

			termGtwPolicy, _, err := consulClient.ACL().PolicyCreate(fakeTermGtwPolicy, nil)
			require.NoError(t, err)

			fakeConsulClientPolicies := []*api.ACLRolePolicyLink{
				{
					ID:   termGtwPolicy.ID,
					Name: termGtwPolicy.Name,
				},
			}

			fakeConsulClientRole := &api.ACLRole{
				Description: "ACL Role for consul-client",
				Policies:    fakeConsulClientPolicies,
				Name:        "terminating-gateway-acl-role",
			}

			_, _, err = consulClient.ACL().RoleCreate(fakeConsulClientRole, nil)
			require.NoError(t, err)
		}

		// Create the terminating gateway service controller.
		controller := &TerminatingGatewayServiceController{
			Client:       fakeClient,
			Log:          logrtest.TestLogger{T: t},
			ConsulClient: consulClient,
			Scheme:       s,
			AclEnabled:   tt.aclEnabled,
		}
		namespacedName := types.NamespacedName{
			Name:      "Terminating gateway service-deleted",
			Namespace: "default",
		}

		// Reconcile a resource that is not in K8s, but is still in Consul.
		resp, err := controller.Reconcile(context.Background(), ctrl.Request{
			NamespacedName: namespacedName,
		})
		require.NoError(t, err)
		require.False(t, resp.Requeue)

		// Get the reconciled TerminatingGatewayService and make assertions on the spec.
		err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
		require.EqualError(t, err, `terminatinggatewayservices.consul.hashicorp.com "Terminating gateway service-deleted" not found`)

		serviceName := "TerminatingGatewayServiceController-created-service"
		policyName := fmt.Sprintf("%s-write-policy", serviceName)
		_, serviceExists := serviceFound("TerminatingGatewayServiceController-created-service", consulClient)

		require.False(t, serviceExists)

		if tt.aclEnabled {
			aclToken, _ := fetchTerminatingGatewayToken(consulClient)
			allTokenPolicies := aclToken.Policies
			_, tokenPolicyFound := findAclPolicy(policyName, allTokenPolicies)

			require.False(t, tokenPolicyFound)

			allConsulPolicies, _, err := consulClient.ACL().PolicyList(&api.QueryOptions{})
			_, consulPolicyFound := findConsulPolicy(policyName, allConsulPolicies)

			require.NoError(t, err)
			require.False(t, consulPolicyFound)

		}
	}
}

func TestTerminatingGatewayServiceUpdateStatus(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name                      string
		terminatingGatewayService *v1alpha1.TerminatingGatewayService
		expStatus                 v1alpha1.TerminatingGatewayServiceStatus
		aclEnabled                bool
	}{
		{
			name: "updates status when there's no existing status",
			terminatingGatewayService: &v1alpha1.TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Terminating gateway service-created",
					Namespace: "default",
				},
				Spec: v1alpha1.TerminatingGatewayServiceSpec{
					Service: &v1alpha1.CatalogService{
						Node:        "legacy_node",
						Address:     "10.20.10.22",
						ServiceName: "TerminatingGatewayServiceController-created-service",
						ServicePort: 9003,
					},
				},
			},
			expStatus: v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
				Conditions: v1alpha1.Conditions{
					{
						Type:   v1alpha1.ConditionSynced,
						Status: corev1.ConditionTrue,
					},
				},
			},
			aclEnabled: true,
		},
		{
			name: "updates status when there is an existing status. ACLs have been enabled",
			terminatingGatewayService: &v1alpha1.TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Terminating gateway service-created",
					Namespace: "default",
				},
				Spec: v1alpha1.TerminatingGatewayServiceSpec{
					Service: &v1alpha1.CatalogService{
						Node:        "legacy_node",
						Address:     "10.20.10.22",
						ServiceName: "TerminatingGatewayServiceController-created-service",
						ServicePort: 9003,
					},
				},
				Status: v1alpha1.TerminatingGatewayServiceStatus{
					ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
						ServiceName: "TerminatingGatewayServiceController-created-service",
						PolicyName:  "",
					},
				},
			},
			expStatus: v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
				Conditions: v1alpha1.Conditions{
					{
						Type:   v1alpha1.ConditionSynced,
						Status: corev1.ConditionTrue,
					},
				},
			},
			aclEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := []runtime.Object{&ns}
			k8sObjects = append(k8sObjects, tt.terminatingGatewayService)

			// Add peering types to the scheme.
			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
				if tt.aclEnabled {
					// Start Consul server with ACLs enabled and default deny policy.
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.InitialManagement = masterToken
				}
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			if tt.aclEnabled {
				// Create fake consul client with fake terminating gateway policy.

				fakeTermGtwPolicy := &api.ACLPolicy{
					Name:        "1b9a40e2-4b80-0b45-4ee4-0a47592fe386-terminating-gateway",
					Description: "ACL Policy for terminating gateway",
				}

				termGtwPolicy, _, err := consulClient.ACL().PolicyCreate(fakeTermGtwPolicy, nil)
				require.NoError(t, err)

				fakeConsulClientPolicies := []*api.ACLRolePolicyLink{
					{
						ID:   termGtwPolicy.ID,
						Name: termGtwPolicy.Name,
					},
				}

				fakeConsulClientRole := &api.ACLRole{
					Description: "ACL Role for consul-client",
					Policies:    fakeConsulClientPolicies,
					Name:        "terminating-gateway-acl-role",
				}

				_, _, err = consulClient.ACL().RoleCreate(fakeConsulClientRole, nil)
				require.NoError(t, err)
			}

			// create the terminating gateway service controller.
			tas := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}

			err = tas.updateStatus(context.Background(), tt.terminatingGatewayService)
			require.NoError(t, err)

			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			terminatingGatewayServiceName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}
			err = fakeClient.Get(context.Background(), terminatingGatewayServiceName, terminatingGatewayService)
			require.NoError(t, err)

			require.Equal(t, tt.expStatus.ServiceInfoRef.ServiceName, terminatingGatewayService.ServiceInfoRef().ServiceName)
			require.Equal(t, tt.expStatus.ServiceInfoRef.PolicyName, terminatingGatewayService.ServiceInfoRef().PolicyName)
			require.Equal(t, tt.expStatus.Conditions[0].Message, terminatingGatewayService.Status.Conditions[0].Message)

		})
	}
}

func TestTerminatingGatewayServiceUpdateStatusError(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name                      string
		terminatingGatewayService *v1alpha1.TerminatingGatewayService
		reconcileErr              error
		expStatus                 v1alpha1.TerminatingGatewayServiceStatus
		aclEnabled                bool
	}{
		{
			name: "updates status when there's no existing status",
			terminatingGatewayService: &v1alpha1.TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Terminating gateway service-created",
					Namespace: "default",
				},
				Spec: v1alpha1.TerminatingGatewayServiceSpec{
					Service: &v1alpha1.CatalogService{
						Node:        "legacy_node",
						Address:     "10.20.10.22",
						ServiceName: "TerminatingGatewayServiceController-created-service",
						ServicePort: 9003,
					},
				},
			},
			reconcileErr: errors.New("this is an error"),
			expStatus: v1alpha1.TerminatingGatewayServiceStatus{
				Conditions: v1alpha1.Conditions{
					{
						Type:    v1alpha1.ConditionSynced,
						Status:  corev1.ConditionFalse,
						Reason:  "InternalError",
						Message: "this is an error",
					},
				},
			},
			aclEnabled: true,
		},
		{
			name: "updates status when there is an existing status",
			terminatingGatewayService: &v1alpha1.TerminatingGatewayService{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "Terminating gateway service-created",
					Namespace: "default",
				},
				Spec: v1alpha1.TerminatingGatewayServiceSpec{
					Service: &v1alpha1.CatalogService{
						Node:        "legacy_node",
						Address:     "10.20.10.22",
						ServiceName: "TerminatingGatewayServiceController-created-service",
						ServicePort: 9003,
					},
				},
			},
			reconcileErr: errors.New("this is an error"),
			expStatus: v1alpha1.TerminatingGatewayServiceStatus{
				Conditions: v1alpha1.Conditions{
					{
						Type:    v1alpha1.ConditionSynced,
						Status:  corev1.ConditionFalse,
						Reason:  "InternalError",
						Message: "this is an error",
					},
				},
			},
			aclEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := []runtime.Object{&ns}
			k8sObjects = append(k8sObjects, tt.terminatingGatewayService)

			// Add peering types to the scheme.
			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
				if tt.aclEnabled {
					// Start Consul server with ACLs enabled and default deny policy.
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.InitialManagement = masterToken
				}
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			if tt.aclEnabled {
				// Create fake consul client with fake terminating gateway policy.

				fakeTermGtwPolicy := &api.ACLPolicy{
					Name:        "1b9a40e2-4b80-0b45-4ee4-0a47592fe386-terminating-gateway",
					Description: "ACL Policy for terminating gateway",
				}

				termGtwPolicy, _, err := consulClient.ACL().PolicyCreate(fakeTermGtwPolicy, nil)
				require.NoError(t, err)

				fakeConsulClientPolicies := []*api.ACLRolePolicyLink{
					{
						ID:   termGtwPolicy.ID,
						Name: termGtwPolicy.Name,
					},
				}

				fakeConsulClientRole := &api.ACLRole{
					Description: "ACL Role for consul-client",
					Policies:    fakeConsulClientPolicies,
					Name:        "terminating-gateway-acl-role",
				}

				_, _, err = consulClient.ACL().RoleCreate(fakeConsulClientRole, nil)
				require.NoError(t, err)
			}

			// create the terminating gateway service controller.
			tas := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}

			tas.updateStatusError(context.Background(), tt.terminatingGatewayService, tt.reconcileErr)

			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			terminatingGatewayServiceName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}
			err = fakeClient.Get(context.Background(), terminatingGatewayServiceName, terminatingGatewayService)
			require.NoError(t, err)
			require.Equal(t, tt.expStatus.Conditions[0].Message, terminatingGatewayService.Status.Conditions[0].Message)

		})
	}
}

func TestServiceFoundFunctions(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec   *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled     bool
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			// create the terminating gateway service controller.
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}
			namespacedName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}

			resp, _ := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})

			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			// Search for nonexistent service.
			nonexistentService := "nonexistentService"
			_, serviceExists := serviceFound(nonexistentService, consulClient)
			require.False(t, serviceExists)
			_, serviceExists, _ = controller.serviceFound(nonexistentService)
			require.False(t, serviceExists)

			// Search for existing service
			serviceName := "TerminatingGatewayServiceController-created-service"
			_, serviceExists = serviceFound(serviceName, consulClient)
			require.True(t, serviceExists)
			_, serviceExists, _ = controller.serviceFound(serviceName)
			require.True(t, serviceExists)
		})
	}
}

func TestTerminatingGatewayACLRole(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec   *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled     bool
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul and updates terminating gateway token with new write policy",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
				if tt.aclEnabled {
					// Start Consul server with ACLs enabled and default deny policy.
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.InitialManagement = masterToken
				}
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			if tt.aclEnabled {
				// Create fake consul client with fake terminating gateway policy.

				fakeTermGtwPolicy := &api.ACLPolicy{
					Name:        "1b9a40e2-4b80-0b45-4ee4-0a47592fe386-terminating-gateway",
					Description: "ACL Policy for terminating gateway",
				}

				termGtwPolicy, _, err := consulClient.ACL().PolicyCreate(fakeTermGtwPolicy, nil)
				require.NoError(t, err)

				fakeConsulClientPolicies := []*api.ACLRolePolicyLink{
					{
						ID:   termGtwPolicy.ID,
						Name: termGtwPolicy.Name,
					},
				}

				fakeConsulClientRole := &api.ACLRole{
					Description: "ACL Role for consul-client",
					Policies:    fakeConsulClientPolicies,
					Name:        "terminating-gateway-acl-role",
				}

				_, _, err = consulClient.ACL().RoleCreate(fakeConsulClientRole, nil)
				require.NoError(t, err)
			}

			// create the terminating gateway service controller.
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}
			namespacedName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}

			resp, _ := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})

			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			// Search empty list.
			emptyArr := []*api.ACLRole{}
			_, err = terminatingGatewayACLRole(emptyArr)
			require.Error(t, err)

			// Search list containing terminating gateway acl role.
			aclRoles, _, _ := consulClient.ACL().RoleList(nil)
			_, err = terminatingGatewayACLRole(aclRoles)
			require.NoError(t, err)
		})
	}
}

func TestCreateOrUpdateService(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec   *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled     bool
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul and updates terminating gateway token with new write policy",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			// create the terminating gateway service controller.
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}
			namespacedName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}

			ctx := context.Background()
			resp, _ := controller.Reconcile(ctx, ctrl.Request{
				NamespacedName: namespacedName,
			})

			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(ctx, namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			// Service does not exist. A new one should be created.
			nonExistentService := "nonexistent-service"

			newTermGtwService := &v1alpha1.TerminatingGatewayService{
				Spec: v1alpha1.TerminatingGatewayServiceSpec{
					Service: &v1alpha1.CatalogService{
						Node:        "legacy_node",
						Address:     "10.22.10.10",
						ServiceName: nonExistentService,
						ServiceID:   nonExistentService,
						ServicePort: 10,
					},
				},
			}
			_, serviceExists := serviceFound(nonExistentService, consulClient)
			require.False(t, serviceExists)

			controller.createOrUpdateService(newTermGtwService, ctx)

			_, serviceExists = serviceFound(nonExistentService, consulClient)
			require.True(t, serviceExists)

			// Service exists and has been updated.
			newTermGtwService.Spec.Service.Address = "10.20.10.22"
			controller.createOrUpdateService(newTermGtwService, ctx)

			service, _ := serviceFound(nonExistentService, consulClient)
			require.Equal(t, newTermGtwService.Spec.Service.Address, service.Address)
		})
	}
}

func TestUpdateTerminatingGatewayTokenWithWritePolicy(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec   *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled     bool
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: false,
		},
		{
			name: "New TerminatingGatewayService registers an external service with Consul and updates terminating gateway token with new write policy",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
				if tt.aclEnabled {
					// Start Consul server with ACLs enabled and default deny policy.
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.InitialManagement = masterToken
				}
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			if tt.aclEnabled {
				// Create fake consul client with fake terminating gateway policy.

				fakeTermGtwPolicy := &api.ACLPolicy{
					Name:        "1b9a40e2-4b80-0b45-4ee4-0a47592fe386-terminating-gateway",
					Description: "ACL Policy for terminating gateway",
				}

				termGtwPolicy, _, err := consulClient.ACL().PolicyCreate(fakeTermGtwPolicy, nil)
				require.NoError(t, err)

				fakeConsulClientPolicies := []*api.ACLRolePolicyLink{
					{
						ID:   termGtwPolicy.ID,
						Name: termGtwPolicy.Name,
					},
				}

				fakeConsulClientRole := &api.ACLRole{
					Description: "ACL Role for consul-client",
					Policies:    fakeConsulClientPolicies,
					Name:        "terminating-gateway-acl-role",
				}

				_, _, err = consulClient.ACL().RoleCreate(fakeConsulClientRole, nil)
				require.NoError(t, err)
			}

			// create the terminating gateway service controller.
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}
			namespacedName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}

			resp, _ := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})

			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			serviceName := "TerminatingGatewayServiceController-created-service"

			// when terminating gateway token does not exist.
			if !tt.aclEnabled {
				err := controller.updateTerminatingGatewayTokenWithWritePolicy(terminatingGatewayService)
				require.Error(t, err)
			} else {
				// when terminating gateway token exists.
				err := controller.updateTerminatingGatewayTokenWithWritePolicy(terminatingGatewayService)
				require.NoError(t, err)
				// search for write policy.
				policyName := fmt.Sprintf("%s-write-policy", serviceName)
				retrievedPol, _, _ := consulClient.ACL().PolicyReadByName(policyName, nil)
				require.Equal(t, retrievedPol.Name, policyName)

				// search terminating gateway token for the write policy.
				matchedRole, _, err := controller.fetchTerminatingGatewayToken()
				require.NoError(t, err)
				termGwRole, _, err := consulClient.ACL().RoleRead(matchedRole.ID, nil)
				require.NoError(t, err)
				_, policyFound := findAclPolicy(policyName, termGwRole.Policies)
				require.True(t, policyFound)
			}
		})
	}
}

func TestUpdateServiceIfDifferent(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec   *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled     bool
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: false,
		},
		{
			name: "New TerminatingGatewayService registers an external service with Consul and updates terminating gateway token with new write policy",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
				if tt.aclEnabled {
					// Start Consul server with ACLs enabled and default deny policy.
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.InitialManagement = masterToken
				}
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			if tt.aclEnabled {
				// Create fake consul client with fake terminating gateway policy.

				fakeTermGtwPolicy := &api.ACLPolicy{
					Name:        "1b9a40e2-4b80-0b45-4ee4-0a47592fe386-terminating-gateway",
					Description: "ACL Policy for terminating gateway",
				}

				termGtwPolicy, _, err := consulClient.ACL().PolicyCreate(fakeTermGtwPolicy, nil)
				require.NoError(t, err)

				fakeConsulClientPolicies := []*api.ACLRolePolicyLink{
					{
						ID:   termGtwPolicy.ID,
						Name: termGtwPolicy.Name,
					},
				}

				fakeConsulClientRole := &api.ACLRole{
					Description: "ACL Role for consul-client",
					Policies:    fakeConsulClientPolicies,
					Name:        "terminating-gateway-acl-role",
				}

				_, _, err = consulClient.ACL().RoleCreate(fakeConsulClientRole, nil)
				require.NoError(t, err)
			}

			// create the terminating gateway service controller.
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}
			namespacedName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}

			ctx := context.Background()
			resp, _ := controller.Reconcile(ctx, ctrl.Request{
				NamespacedName: namespacedName,
			})

			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(ctx, namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			service, serviceExists := serviceFound(terminatingGatewayService.Spec.Service.ServiceName, consulClient)
			require.True(t, serviceExists)

			// no changes made to the controller.
			err = controller.updateServiceIfDifferent(service, terminatingGatewayService, ctx)
			require.NoError(t, err)
			require.Equal(t, terminatingGatewayService.Spec.Service.Address, service.Address)

			// changes have been made to the controller.
			terminatingGatewayService.Spec.Service.ServiceAddress = "10.20.10.11"
			terminatingGatewayService.Spec.Service.Node = "legacy_node"
			terminatingGatewayService.Spec.Service.Datacenter = "dc1"
			terminatingGatewayService.Spec.Service.ServicePort = 10
			terminatingGatewayService.Spec.Service.ServiceEnableTagOverride = true

			err = controller.updateServiceIfDifferent(service, terminatingGatewayService, ctx)
			require.NoError(t, err)

			service, serviceExists = serviceFound(terminatingGatewayService.Spec.Service.ServiceName, consulClient)
			require.True(t, serviceExists)

			require.Equal(t, terminatingGatewayService.Spec.Service.Node, service.Node)
			require.Equal(t, terminatingGatewayService.Spec.Service.Datacenter, service.Datacenter)
			require.Equal(t, terminatingGatewayService.Spec.Service.ServicePort, service.ServicePort)
			require.Equal(t, terminatingGatewayService.Spec.Service.ServiceEnableTagOverride, service.ServiceEnableTagOverride)

			// write policy needs to be updated.
			if tt.aclEnabled {
				// policy needs to be created.
				expectedPolicyName := fmt.Sprintf("%s-write-policy", service.ServiceName)
				require.Equal(t, expectedPolicyName, terminatingGatewayService.Status.ServiceInfoRef.PolicyName)
			}
		})
	}
}

func TestDeleteService(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec   *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled     bool
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: false,
		},
		{
			name: "New TerminatingGatewayService registers an external service with Consul and updates terminating gateway token with new write policy",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
				if tt.aclEnabled {
					// Start Consul server with ACLs enabled and default deny policy.
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.InitialManagement = masterToken
				}
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			if tt.aclEnabled {
				// Create fake consul client with fake terminating gateway policy.

				fakeTermGtwPolicy := &api.ACLPolicy{
					Name:        "1b9a40e2-4b80-0b45-4ee4-0a47592fe386-terminating-gateway",
					Description: "ACL Policy for terminating gateway",
				}

				termGtwPolicy, _, err := consulClient.ACL().PolicyCreate(fakeTermGtwPolicy, nil)
				require.NoError(t, err)

				fakeConsulClientPolicies := []*api.ACLRolePolicyLink{
					{
						ID:   termGtwPolicy.ID,
						Name: termGtwPolicy.Name,
					},
				}

				fakeConsulClientRole := &api.ACLRole{
					Description: "ACL Role for consul-client",
					Policies:    fakeConsulClientPolicies,
					Name:        "terminating-gateway-acl-role",
				}

				_, _, err = consulClient.ACL().RoleCreate(fakeConsulClientRole, nil)
				require.NoError(t, err)
			}

			// create the terminating gateway service controller.
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}
			namespacedName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}

			resp, _ := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})

			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			serviceName := terminatingGatewayService.Spec.Service.ServiceName
			nonExistentService := "nonexistent-service"
			// delete nonexistent service
			serviceDeleted, _ := controller.deleteService(nonExistentService)
			require.False(t, serviceDeleted)

			// delete existing service
			serviceDeleted, _ = controller.deleteService(serviceName)
			require.True(t, serviceDeleted)

			_, serviceExists := serviceFound(serviceName, consulClient)
			require.False(t, serviceExists)
		})
	}
}

func TestOnlyDeleteServiceEntry(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec   *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled     bool
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			// create the terminating gateway service controller.
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}
			namespacedName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}

			resp, _ := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})

			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			// Search for nonexistent service.
			nonexistentService := "nonexistentService"
			serviceDeleted, _ := controller.onlyDeleteServiceEntry(nonexistentService)
			require.False(t, serviceDeleted)

			// Search for existing service
			serviceName := terminatingGatewayService.Spec.Service.ServiceName
			serviceDeleted, _ = controller.onlyDeleteServiceEntry(serviceName)
			require.True(t, serviceDeleted)
		})
	}
}

func TestDeleteTerminatingGatewayTokenWritePolicy(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec   *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled     bool
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: false,
		},
		{
			name: "New TerminatingGatewayService registers an external service with Consul and updates terminating gateway token with new write policy",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
				if tt.aclEnabled {
					// Start Consul server with ACLs enabled and default deny policy.
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.InitialManagement = masterToken
				}
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			if tt.aclEnabled {
				// Create fake consul client with fake terminating gateway policy.

				fakeTermGtwPolicy := &api.ACLPolicy{
					Name:        "1b9a40e2-4b80-0b45-4ee4-0a47592fe386-terminating-gateway",
					Description: "ACL Policy for terminating gateway",
				}

				termGtwPolicy, _, err := consulClient.ACL().PolicyCreate(fakeTermGtwPolicy, nil)
				require.NoError(t, err)

				fakeConsulClientPolicies := []*api.ACLRolePolicyLink{
					{
						ID:   termGtwPolicy.ID,
						Name: termGtwPolicy.Name,
					},
				}

				fakeConsulClientRole := &api.ACLRole{
					Description: "ACL Role for consul-client",
					Policies:    fakeConsulClientPolicies,
					Name:        "terminating-gateway-acl-role",
				}

				_, _, err = consulClient.ACL().RoleCreate(fakeConsulClientRole, nil)
				require.NoError(t, err)
			}

			// create the terminating gateway service controller.
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}
			namespacedName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}

			resp, _ := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})

			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			serviceName := "TerminatingGatewayServiceController-created-service"

			// when terminating gateway token does not exist.
			if !tt.aclEnabled {
				err := controller.deleteTerminatingGatewayTokenWritePolicy(serviceName)
				require.Error(t, err)
			} else {
				// when terminating gateway token exists
				terminatingGatewayToken, _, err := controller.fetchTerminatingGatewayToken()
				require.NoError(t, err)

				policyName := fmt.Sprintf("%s-write-policy", serviceName)
				policies := terminatingGatewayToken.Policies
				_, policyFound := findAclPolicy(policyName, policies)
				require.True(t, policyFound)

				err = controller.deleteTerminatingGatewayTokenWritePolicy(serviceName)
				require.NoError(t, err)

				// find actual policy in consul.
				_, _, err = consulClient.ACL().PolicyReadByName(policyName, nil)
				require.Error(t, err)

				// find policy within terminating gateway
				terminatingGatewayToken, _, err = controller.fetchTerminatingGatewayToken()
				require.NoError(t, err)
				updatedPolicies := terminatingGatewayToken.Policies

				policyStillInTermGtw := false
				for _, policy := range updatedPolicies {
					if policy.Name == policyName {
						policyStillInTermGtw = true
						break
					}
				}

				require.False(t, policyStillInTermGtw)
			}

		})
	}
}

func TestFetchTerminatingGatewayToken(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name                       string
		k8sObjects                 func() []runtime.Object
		expectedStatus             *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec               *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled                 bool
		beforeCreatingTermGtwToken bool
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul and updates terminating gateway token with new write policy",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled:                 true,
			beforeCreatingTermGtwToken: true,
		},
		{
			name: "New TerminatingGatewayService registers an external service with Consul and updates terminating gateway token with new write policy",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled:                 true,
			beforeCreatingTermGtwToken: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
				if tt.aclEnabled {
					// Start Consul server with ACLs enabled and default deny policy.
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.InitialManagement = masterToken
				}
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			fakeTermGtwPolicyName := "1b9a40e2-4b80-0b45-4ee4-0a47592fe386-terminating-gateway"
			if tt.aclEnabled && !tt.beforeCreatingTermGtwToken {
				// Create fake consul client with fake terminating gateway policy.

				fakeTermGtwPolicy := &api.ACLPolicy{
					Name:        fakeTermGtwPolicyName,
					Description: "ACL Policy for terminating gateway",
				}

				termGtwPolicy, _, err := consulClient.ACL().PolicyCreate(fakeTermGtwPolicy, nil)
				require.NoError(t, err)

				fakeConsulClientPolicies := []*api.ACLRolePolicyLink{
					{
						ID:   termGtwPolicy.ID,
						Name: termGtwPolicy.Name,
					},
				}

				fakeConsulClientRole := &api.ACLRole{
					Description: "ACL Role for consul-client",
					Policies:    fakeConsulClientPolicies,
					Name:        "terminating-gateway-acl-role",
				}

				_, _, err = consulClient.ACL().RoleCreate(fakeConsulClientRole, nil)
				require.NoError(t, err)
			}

			// create the terminating gateway service controller.
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}
			namespacedName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}

			resp, _ := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})

			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			if tt.aclEnabled && tt.beforeCreatingTermGtwToken {
				// Before creating fake terminating gateway policy. Check if it exists
				_, _, err = controller.fetchTerminatingGatewayToken()
				require.Error(t, err)
				_, found := fetchTerminatingGatewayToken(consulClient)
				require.False(t, found)

			} else {
				termGtwRole, _, err := controller.fetchTerminatingGatewayToken()
				require.NoError(t, err)

				_, termGtwPolicyFound := findAclPolicy(fakeTermGtwPolicyName, termGtwRole.Policies)
				require.True(t, termGtwPolicyFound)
				_, found := fetchTerminatingGatewayToken(consulClient)
				require.True(t, found)
			}

		})
	}
}

func TestFindPolicyFunctions(t *testing.T) {
	nodeName := "test-node"
	cases := []struct {
		name           string
		k8sObjects     func() []runtime.Object
		expectedStatus *v1alpha1.TerminatingGatewayServiceStatus
		expectedSpec   *v1alpha1.TerminatingGatewayServiceSpec
		aclEnabled     bool
	}{
		{
			name: "New TerminatingGatewayService registers an external service with Consul and updates terminating gateway token with new write policy",
			k8sObjects: func() []runtime.Object {
				terminatingGatewayService := &v1alpha1.TerminatingGatewayService{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "Terminating gateway service-created",
						Namespace: "default",
					},
					Spec: v1alpha1.TerminatingGatewayServiceSpec{
						Service: &v1alpha1.CatalogService{
							Node:        "legacy_node",
							Address:     "10.20.10.22",
							ServiceName: "TerminatingGatewayServiceController-created-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created-service",
					PolicyName:  "TerminatingGatewayServiceController-created-service-write-policy",
				},
			},
			expectedSpec: &v1alpha1.TerminatingGatewayServiceSpec{
				Service: &v1alpha1.CatalogService{
					Node:        "legacy_node",
					Address:     "10.20.10.22",
					ServiceName: "TerminatingGatewayServiceController-created-service",
					ServicePort: 9003,
				},
			},
			aclEnabled: true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
				if tt.aclEnabled {
					// Start Consul server with ACLs enabled and default deny policy.
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.InitialManagement = masterToken
				}
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
				Token:   masterToken,
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			if tt.aclEnabled {
				// Create fake consul client with fake terminating gateway policy.

				fakeTermGtwPolicy := &api.ACLPolicy{
					Name:        "1b9a40e2-4b80-0b45-4ee4-0a47592fe386-terminating-gateway",
					Description: "ACL Policy for terminating gateway",
				}

				termGtwPolicy, _, err := consulClient.ACL().PolicyCreate(fakeTermGtwPolicy, nil)
				require.NoError(t, err)

				fakeConsulClientPolicies := []*api.ACLRolePolicyLink{
					{
						ID:   termGtwPolicy.ID,
						Name: termGtwPolicy.Name,
					},
				}

				fakeConsulClientRole := &api.ACLRole{
					Description: "ACL Role for consul-client",
					Policies:    fakeConsulClientPolicies,
					Name:        "terminating-gateway-acl-role",
				}

				_, _, err = consulClient.ACL().RoleCreate(fakeConsulClientRole, nil)
				require.NoError(t, err)
			}

			// create the terminating gateway service controller.
			controller := &TerminatingGatewayServiceController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
				AclEnabled:   tt.aclEnabled,
			}
			namespacedName := types.NamespacedName{
				Name:      "Terminating gateway service-created",
				Namespace: "default",
			}

			resp, _ := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})

			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			serviceName := "TerminatingGatewayServiceController-created-service"
			policyName := fmt.Sprintf("%s-write-policy", serviceName)

			if tt.aclEnabled {
				aclToken, _ := fetchTerminatingGatewayToken(consulClient)
				allTokenPolicies := aclToken.Policies
				allConsulPolicies, _, err := consulClient.ACL().PolicyList(&api.QueryOptions{})
				require.NoError(t, err)

				// test findAclPolicy.
				emptyArrTokenPolicies := []*api.ACLRolePolicyLink{}
				_, tokenPolicyFound := findAclPolicy(policyName, emptyArrTokenPolicies)
				require.False(t, tokenPolicyFound)

				_, tokenPolicyFound = findAclPolicy(policyName, allTokenPolicies)
				require.True(t, tokenPolicyFound)

				// test findConsulPolicy.
				emptyArrConsulPolicies := []*api.ACLPolicyListEntry{}
				_, tokenPolicyFound = findConsulPolicy(policyName, emptyArrConsulPolicies)
				require.False(t, tokenPolicyFound)

				_, tokenPolicyFound = findConsulPolicy(policyName, allConsulPolicies)
				require.True(t, tokenPolicyFound)
			}
		})
	}
}
