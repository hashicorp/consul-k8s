package connectinject

import (
	"context"
	"encoding/base64"
	"fmt"
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

// TestReconcileCreateUpdatePeeringAcceptor creates a peering acceptor.
func TestReconcileCreateUpdatePeeringAcceptor(t *testing.T) {
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
			name: "New PeeringAcceptor creates a peering in Consul and generates a token",
			k8sObjects: func() []runtime.Object {
				peeringAcceptor := &v1alpha1.PeeringAcceptor{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "acceptor-created",
						Namespace: "default",
					},
					Spec: v1alpha1.PeeringAcceptorSpec{
						Peer: &v1alpha1.Peer{
							Secret: &v1alpha1.Secret{
								Name:    "acceptor-created-secret",
								Key:     "data",
								Backend: "kubernetes",
							},
						},
					},
				}
				return []runtime.Object{peeringAcceptor}
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
		{
			name: "When the secret already exists (not created by controller), it is updated with the contents of the new peering token and an owner reference is added",
			k8sObjects: func() []runtime.Object {
				peeringAcceptor := &v1alpha1.PeeringAcceptor{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "acceptor-created",
						Namespace: "default",
					},
					Spec: v1alpha1.PeeringAcceptorSpec{
						Peer: &v1alpha1.Peer{
							Secret: &v1alpha1.Secret{
								Name:    "acceptor-created-secret",
								Key:     "data",
								Backend: "kubernetes",
							},
						},
					},
				}
				secret := createSecret("acceptor-created-secret", "default", "some-old-key", "some-old-data")
				return []runtime.Object{peeringAcceptor, secret}
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
		{
			name: "PeeringAcceptor status secret has different contents",
			k8sObjects: func() []runtime.Object {
				peeringAcceptor := &v1alpha1.PeeringAcceptor{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "acceptor-created",
						Namespace: "default",
					},
					Spec: v1alpha1.PeeringAcceptorSpec{
						Peer: &v1alpha1.Peer{
							Secret: &v1alpha1.Secret{
								Name:    "acceptor-created-secret",
								Key:     "data",
								Backend: "kubernetes",
							},
						},
					},
					Status: v1alpha1.PeeringAcceptorStatus{
						Secret: &v1alpha1.SecretStatus{
							Name:       "acceptor-created-secret",
							Key:        "some-old-key",
							Backend:    "kubernetes",
							LatestHash: "some-old-sha",
						},
					},
				}
				secret := createSecret("acceptor-created-secret", "default", "some-old-key", "some-old-data")
				secret.OwnerReferences = []metav1.OwnerReference{
					{
						APIVersion:         "consul.hashicorp.com/v1alpha1",
						Kind:               "PeeringAcceptor",
						Name:               "acceptor-created",
						UID:                "",
						Controller:         pointerToBool(true),
						BlockOwnerDeletion: pointerToBool(true),
					},
				}
				return []runtime.Object{peeringAcceptor, secret}
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
		{
			name: "PeeringAcceptor status secret name is changed",
			k8sObjects: func() []runtime.Object {
				peeringAcceptor := &v1alpha1.PeeringAcceptor{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "acceptor-created",
						Namespace: "default",
					},
					Spec: v1alpha1.PeeringAcceptorSpec{
						Peer: &v1alpha1.Peer{
							Secret: &v1alpha1.Secret{
								Name:    "acceptor-created-secret",
								Key:     "data",
								Backend: "kubernetes",
							},
						},
					},
					Status: v1alpha1.PeeringAcceptorStatus{
						Secret: &v1alpha1.SecretStatus{
							Name:       "some-old-secret",
							Key:        "some-old-key",
							Backend:    "kubernetes",
							LatestHash: "some-old-sha",
						},
					},
				}
				secret := createSecret("some-old-secret", "default", "some-old-key", "some-old-data")
				return []runtime.Object{peeringAcceptor, secret}
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
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringAcceptor{}, &v1alpha1.PeeringAcceptorList{})
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
			pac := &PeeringAcceptorController{
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
			// This assertion needs to be on StringData rather than Data because in the fake K8s client the contents are
			// stored in StringData if that's how the secret was initialized in the fake client. In a real cluster, this
			// StringData is an input only field, and shouldn't be read from.
			// Before failing at this case, the controller will error at reconcile with "secrets <SECRET> already
			// exists". Leaving this here documents that the entire contents of an existing secret should
			// be replaced.
			require.Equal(t, "", createdSecret.StringData["some-old-key"])
			decodedTokenData, err := base64.StdEncoding.DecodeString(createdSecret.StringData["data"])
			require.NoError(t, err)

			require.Contains(t, string(decodedTokenData), "\"CA\":null")
			require.Contains(t, string(decodedTokenData), "\"ServerAddresses\"")
			require.Contains(t, string(decodedTokenData), "\"ServerName\":\"server.dc1.consul\"")
			// Assert on the owner reference
			require.Len(t, createdSecret.OwnerReferences, 1)
			require.Equal(t, "consul.hashicorp.com/v1alpha1", createdSecret.OwnerReferences[0].APIVersion)
			require.Equal(t, "PeeringAcceptor", createdSecret.OwnerReferences[0].Kind)
			require.Equal(t, "acceptor-created", createdSecret.OwnerReferences[0].Name)
			require.Equal(t, true, *createdSecret.OwnerReferences[0].BlockOwnerDeletion)
			require.Equal(t, true, *createdSecret.OwnerReferences[0].Controller)
			fmt.Println("make status assertions, assert that old secret was deleted")
			// ********************************************
			//t.Fail()
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
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringAcceptor{}, &v1alpha1.PeeringAcceptorList{})
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
			pac := &PeeringAcceptorController{
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

func TestShouldGenerateToken(t *testing.T) {

}

func TestUpdateStatus(t *testing.T) {
	cases := []struct {
		name              string
		peeringAcceptor   *v1alpha1.PeeringAcceptor
		generateTokenResp *api.PeeringGenerateTokenResponse
		expStatus         v1alpha1.PeeringAcceptorStatus
	}{
		{
			name: "updates status when there's no existing status",
			peeringAcceptor: &v1alpha1.PeeringAcceptor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "acceptor",
					Namespace: "default",
				},
				Spec: v1alpha1.PeeringAcceptorSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "acceptor-secret",
							Key:     "data",
							Backend: "kubernetes",
						},
					},
				},
			},
			generateTokenResp: &api.PeeringGenerateTokenResponse{
				PeeringToken: "fake",
			},
			expStatus: v1alpha1.PeeringAcceptorStatus{
				Secret: &v1alpha1.SecretStatus{
					Name:       "acceptor-secret",
					Key:        "data",
					Backend:    "kubernetes",
					LatestHash: "b5d54c39e66671c9731b9f471e585d8262cd4f54963f0c93082d8dcf334d4c78",
				},
			},
		},
		{
			name: "updates status when there is an existing status",
			peeringAcceptor: &v1alpha1.PeeringAcceptor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "acceptor",
					Namespace: "default",
				},
				Spec: v1alpha1.PeeringAcceptorSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "acceptor-secret",
							Key:     "data",
							Backend: "kubernetes",
						},
					},
				},
				Status: v1alpha1.PeeringAcceptorStatus{
					Secret: &v1alpha1.SecretStatus{
						Name:       "old-name",
						Key:        "old-key",
						Backend:    "kubernetes",
						LatestHash: "old-sha",
					},
				},
			},
			generateTokenResp: &api.PeeringGenerateTokenResponse{
				PeeringToken: "fake",
			},
			expStatus: v1alpha1.PeeringAcceptorStatus{
				Secret: &v1alpha1.SecretStatus{
					Name:       "acceptor-secret",
					Key:        "data",
					Backend:    "kubernetes",
					LatestHash: "b5d54c39e66671c9731b9f471e585d8262cd4f54963f0c93082d8dcf334d4c78",
				},
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client.
			k8sObjects := []runtime.Object{&ns}
			k8sObjects = append(k8sObjects, tt.peeringAcceptor)

			// Add peering types to the scheme.
			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringAcceptor{}, &v1alpha1.PeeringAcceptorList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()
			// Create the peering acceptor controller.
			pac := &PeeringAcceptorController{
				Client: fakeClient,
				Log:    logrtest.TestLogger{T: t},
				Scheme: s,
			}

			err := pac.updateStatus(context.Background(), tt.peeringAcceptor, tt.generateTokenResp)
			require.NoError(t, err)

			peeringAcceptor := &v1alpha1.PeeringAcceptor{}
			peeringAcceptorName := types.NamespacedName{
				Name:      "acceptor",
				Namespace: "default",
			}
			err = fakeClient.Get(context.Background(), peeringAcceptorName, peeringAcceptor)
			require.NoError(t, err)
			require.Equal(t, tt.expStatus.Secret.Name, peeringAcceptor.Status.Secret.Name)
			require.Equal(t, tt.expStatus.Secret.Key, peeringAcceptor.Status.Secret.Key)
			require.Equal(t, tt.expStatus.Secret.Backend, peeringAcceptor.Status.Secret.Backend)
			require.Equal(t, tt.expStatus.Secret.LatestHash, peeringAcceptor.Status.Secret.LatestHash)

		})
	}
}

// test update status
// test should generate token and error cases
// test update reconcile
