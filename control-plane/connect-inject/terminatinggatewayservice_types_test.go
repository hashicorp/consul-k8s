package connectinject

import (
	"context"
	"errors"
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
				policyName = serviceName + "-write-policy"
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
		policyName := serviceName + "-write-policy"
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
