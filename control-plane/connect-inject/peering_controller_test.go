package connectinject

import (
	"context"
	"strings"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// TestReconcileCreateEndpoint tests the logic to create service instances in Consul from the addresses in the Endpoints
// object. The cases test an empty endpoints object, a basic endpoints object with one address, a basic endpoints object
// with two addresses, and an endpoints object with every possible customization.
// This test covers EndpointsController.createServiceRegistrations.
func TestReconcileCreatePeeringAcceptor(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                   string
		k8sObjects             func() []runtime.Object
		expectedConsulPeerings []*api.Peering
		expectedK8sSecrets     func() []runtime.Object
		expErr                 string
	}{
		{
			name: "PeeringAcceptor creates a peering in Consul and generates a token",
			k8sObjects: func() []runtime.Object {
				endpoint := &v1alpha1.Peering{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "acceptor-created",
						Namespace: "default",
					},
					Spec: v1alpha1.PeeringSpec{
						Peer: &v1alpha1.Peer{
							Secret: &v1alpha1.Secret{
								Name:    "acceptor-created-secret",
								Key:     "data",
								Backend: "kubernetes",
							},
						},
					},
				}
				return []runtime.Object{endpoint}
			},
			expectedConsulPeerings: []*api.Peering{
				{
					Name: "acceptor-created",
				},
			},
			expectedK8sSecrets: func() []runtime.Object {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "acceptor-created-secret",
						Namespace: "default",
					},
					StringData: map[string]string{
						"data": "tokenstub",
					},
				}
				return []runtime.Object{secret}
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.Peering{}, &v1alpha1.PeeringList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			// Create test consul server
			consul, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer consul.Stop()
			consul.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: consul.HTTPAddr,
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)
			addr := strings.Split(consul.HTTPAddr, ":")
			consulPort := addr[1]

			// Create the peering acceptor controller
			pac := &PeeringController{
				Client:          fakeClient,
				Log:             logrtest.TestLogger{T: t},
				ConsulClient:    consulClient,
				ConsulPort:      consulPort,
				ConsulScheme:    "http",
				ConsulClientCfg: cfg,
			}
			namespacedName := types.NamespacedName{
				Namespace: "default",
				Name:      "acceptor-created",
			}

			resp, err := pac.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should have the peering.
			peering, _, err := consulClient.Peerings().Read(context.Background(), "acceptor-created", nil)
			require.NoError(t, err)
			require.Equal(t, tt.expectedConsulPeerings[0], peering)

		})
	}
}
