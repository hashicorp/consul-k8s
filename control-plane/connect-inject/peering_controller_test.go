package connectinject

import (
	"context"
	"net/http"
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

// TestReconcileCreatePeeringAcceptor creates a peering acceptor
func TestReconcileCreatePeeringAcceptor(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                   string
		k8sObjects             func() []runtime.Object
		expectedConsulPeerings []*api.Peering
		expectedK8sSecrets     func() []*corev1.Secret
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
			expectedK8sSecrets: func() []*corev1.Secret {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "acceptor-created-secret",
						Namespace: "default",
					},
					StringData: map[string]string{
						"data": "tokenstub",
					},
				}
				return []*corev1.Secret{secret}
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

			// Create the peering acceptor controller
			pac := &PeeringController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
			}
			namespacedName := types.NamespacedName{
				Name:      "acceptor-created",
				Namespace: "default",
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
			readReq := api.PeeringReadRequest{Name: "acceptor-created"}
			peering, _, err := consulClient.Peerings().Read(context.Background(), readReq, nil)
			require.NoError(t, err)
			require.Equal(t, tt.expectedConsulPeerings[0].Name, peering.Name)
			require.NotEmpty(t, peering.ID)

			// Make assertions on the created secret.
			createdSecret := &corev1.Secret{}
			createdSecretName := types.NamespacedName{
				Name:      "acceptor-created-secret",
				Namespace: "default",
			}
			err = fakeClient.Get(context.Background(), createdSecretName, createdSecret)
			require.NoError(t, err)
			expSecrets := tt.expectedK8sSecrets()
			require.Equal(t, expSecrets[0].Name, createdSecret.Name)

		})
	}
}

// TestReconcileDeletePeeringAcceptor reconciles a PeeringAcceptor resource that is no longer in Kubernetes, but still
// exists in Consul.
func TestReconcileDeletePeeringAcceptor(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                   string
		initialConsulPeerNames []string
		expErr                 string
	}{
		{
			name: "PeeringAcceptor ",
			initialConsulPeerNames: []string{
				"acceptor-deleted",
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}

			// Create fake k8s client.
			k8sObjects := []runtime.Object{&ns}

			// Add peering types to the scheme.
			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.Peering{}, &v1alpha1.PeeringList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			// Create test consul server.
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

			// Add the initial peerings into Consul by calling the Generate token endpoint.
			_, _, err = consulClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: tt.initialConsulPeerNames[0]}, nil)
			require.NoError(t, err)

			// Create the peering acceptor controller.
			pac := &PeeringController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
			}
			namespacedName := types.NamespacedName{
				Name:      "acceptor-deleted",
				Namespace: "default",
			}

			// Reconcile a resource that is not in K8s, but is still in Consul.
			resp, err := pac.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should not have the peering.
			readReq := api.PeeringReadRequest{Name: "acceptor-deleted"}
			peering, _, err := consulClient.Peerings().Read(context.Background(), readReq, nil)
			var statusErr api.StatusError
			require.ErrorAs(t, err, &statusErr)
			require.Equal(t, http.StatusNotFound, statusErr.Code)
			require.Nil(t, peering)
		})
	}
}

// test update status
// test should generate token and error cases
// test update reconcile
