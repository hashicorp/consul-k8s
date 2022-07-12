package connectinject

import (
	"context"
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
		expErr         string
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
							ServiceName: "TerminatingGatewayServiceController-created service: test-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created service: test-service",
					PolicyName:  "",
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
							ServiceName: "TerminatingGatewayServiceController-created service: test-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created service: test-service",
					PolicyName:  "",
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

			// Create test consul server.
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
				if tt.aclEnabled {
					// Start Consul server with ACLs enabled and default deny policy.
					masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
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

			resp, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.False(t, resp.Requeue)

			// Get the reconciled TerminatingGatewayService and make assertions on the spec.
			terminatingGatewayService := &v1alpha1.TerminatingGatewayService{}
			err = fakeClient.Get(context.Background(), namespacedName, terminatingGatewayService)
			require.NoError(t, err)

			serviceName := "TerminatingGatewayServiceController-created service: test-service"
			policyName := serviceName + "-write-policy"
			service, serviceExists := serviceFound("TerminatingGatewayServiceController-created service: test-service", consulClient)

			require.True(t, serviceExists)

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
							ServiceName: "TerminatingGatewayServiceController-created service: test-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created service: test-service",
					PolicyName:  "",
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
							ServiceName: "TerminatingGatewayServiceController-created service: test-service",
							ServicePort: 9003,
						},
					},
				}
				return []runtime.Object{terminatingGatewayService}
			},
			expectedStatus: &v1alpha1.TerminatingGatewayServiceStatus{
				ServiceInfoRef: &v1alpha1.ServiceInfoRefStatus{
					ServiceName: "TerminatingGatewayServiceController-created service: test-service",
					PolicyName:  "",
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
					ServiceName: "TerminatingGatewayServiceController-created service: test-service",
					ServicePort: 9003,
				},
			},
		}
		k8sObjects := []runtime.Object{&ns, terminatingGatewayService}

		// Add terminating gateway service types to the scheme.
		s := scheme.Scheme
		s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.TerminatingGatewayService{}, &v1alpha1.TerminatingGatewayServiceList{})
		fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

		// Create test consul server.
		consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
			c.NodeName = nodeName
			if tt.aclEnabled {
				// Start Consul server with ACLs enabled and default deny policy.
				masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
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
		}
		consulClient, err := api.NewClient(cfg)
		require.NoError(t, err)

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

		serviceName := "TerminatingGatewayServiceController-created service: test-service"
		policyName := serviceName + "-write-policy"
		_, serviceExists := serviceFound("TerminatingGatewayServiceController-created service: test-service", consulClient)

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
