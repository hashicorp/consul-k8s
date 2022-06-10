package connectinject

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
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
		expectedStatus         *v1alpha1.PeeringAcceptorStatus
		expectDeletedK8sSecret *types.NamespacedName
		initialConsulPeerName  string
	}{
		{
			name: "New PeeringAcceptor creates a peering in Consul and generates a token",
			k8sObjects: func() []runtime.Object {
				acceptor := &v1alpha1.PeeringAcceptor{
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
				return []runtime.Object{acceptor}
			},
			expectedStatus: &v1alpha1.PeeringAcceptorStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "acceptor-created-secret",
						Key:     "data",
						Backend: "kubernetes",
					},
				},
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
				acceptor := &v1alpha1.PeeringAcceptor{
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
				return []runtime.Object{acceptor, secret}
			},
			expectedStatus: &v1alpha1.PeeringAcceptorStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "acceptor-created-secret",
						Key:     "data",
						Backend: "kubernetes",
					},
				},
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
			name: "PeeringAcceptor status secret exists and has different contents",
			k8sObjects: func() []runtime.Object {
				acceptor := &v1alpha1.PeeringAcceptor{
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
						SecretRef: &v1alpha1.SecretRefStatus{
							Secret: v1alpha1.Secret{
								Name:    "acceptor-created-secret",
								Key:     "some-old-key",
								Backend: "kubernetes",
							},
							ResourceVersion: "some-old-sha",
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
				return []runtime.Object{acceptor, secret}
			},
			expectedStatus: &v1alpha1.PeeringAcceptorStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "acceptor-created-secret",
						Key:     "data",
						Backend: "kubernetes",
					},
				},
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
			initialConsulPeerName: "acceptor-created",
		},
		{
			name: "PeeringAcceptor status secret exists and there's no peering in Consul",
			k8sObjects: func() []runtime.Object {
				acceptor := &v1alpha1.PeeringAcceptor{
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
						SecretRef: &v1alpha1.SecretRefStatus{
							Secret: v1alpha1.Secret{
								Name:    "some-old-secret",
								Key:     "some-old-key",
								Backend: "kubernetes",
							},
							ResourceVersion: "some-old-sha",
						},
					},
				}
				secret := createSecret("some-old-secret", "default", "some-old-key", "some-old-data")
				return []runtime.Object{acceptor, secret}
			},
			expectedStatus: &v1alpha1.PeeringAcceptorStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "acceptor-created-secret",
						Key:     "data",
						Backend: "kubernetes",
					},
				},
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
			expectDeletedK8sSecret: &types.NamespacedName{
				Name:      "some-old-secret",
				Namespace: "default",
			},
		},
		{
			name: "PeeringAcceptor status secret name is changed when there is a peering in Consul",
			k8sObjects: func() []runtime.Object {
				acceptor := &v1alpha1.PeeringAcceptor{
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
						SecretRef: &v1alpha1.SecretRefStatus{
							Secret: v1alpha1.Secret{
								Name:    "some-old-secret",
								Key:     "some-old-key",
								Backend: "kubernetes",
							},
							ResourceVersion: "some-old-sha",
						},
					},
				}
				secret := createSecret("some-old-secret", "default", "some-old-key", "some-old-data")
				return []runtime.Object{acceptor, secret}
			},
			expectedStatus: &v1alpha1.PeeringAcceptorStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "acceptor-created-secret",
						Key:     "data",
						Backend: "kubernetes",
					},
				},
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
			expectDeletedK8sSecret: &types.NamespacedName{
				Name:      "some-old-secret",
				Namespace: "default",
			},
			initialConsulPeerName: "acceptor-created",
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

			if tt.initialConsulPeerName != "" {
				// Add the initial peerings into Consul by calling the Generate token endpoint.
				_, _, err = consulClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: tt.initialConsulPeerName}, nil)
				require.NoError(t, err)
			}

			// Create the peering acceptor controller
			controller := &PeeringAcceptorController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
			}
			namespacedName := types.NamespacedName{
				Name:      "acceptor-created",
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

			// After reconciliation, Consul should have the peering.
			peering, _, err := consulClient.Peerings().Read(context.Background(), "acceptor-created", nil)
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

			// Get the reconciled PeeringAcceptor and make assertions on the status
			acceptor := &v1alpha1.PeeringAcceptor{}
			err = fakeClient.Get(context.Background(), namespacedName, acceptor)
			require.NoError(t, err)
			if tt.expectedStatus != nil {
				require.Equal(t, tt.expectedStatus.SecretRef.Name, acceptor.SecretRef().Name)
				require.Equal(t, tt.expectedStatus.SecretRef.Key, acceptor.SecretRef().Key)
				require.Equal(t, tt.expectedStatus.SecretRef.Backend, acceptor.SecretRef().Backend)
			}
			// Check that old secret was deleted.
			if tt.expectDeletedK8sSecret != nil {
				oldSecret := &corev1.Secret{}
				err = fakeClient.Get(context.Background(), *tt.expectDeletedK8sSecret, oldSecret)
				t.Log(err)
				t.Log(oldSecret)
				if !k8serrors.IsNotFound(err) {
					t.Error("old secret should have been deleted but was not")
				}
			}

		})
	}
}

// TestReconcileDeletePeeringAcceptor reconciles a PeeringAcceptor resource that is no longer in Kubernetes, but still
// exists in Consul.
func TestReconcileDeletePeeringAcceptor(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                  string
		initialConsulPeerName string
		expErr                string
	}{
		{
			name:                  "PeeringAcceptor ",
			initialConsulPeerName: "acceptor-deleted",
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
			_, _, err = consulClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: tt.initialConsulPeerName}, nil)
			require.NoError(t, err)

			// Create the peering acceptor controller.
			controller := &PeeringAcceptorController{
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
			resp, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should not have the peering.
			peering, _, err := consulClient.Peerings().Read(context.Background(), "acceptor-deleted", nil)
			require.Nil(t, peering)
			require.NoError(t, err)
		})
	}
}

func TestShouldGenerateToken(t *testing.T) {
	cases := []struct {
		name              string
		peeringAcceptor   *v1alpha1.PeeringAcceptor
		existingSecret    func() *corev1.Secret
		expShouldGenerate bool
		expNameChanged    bool
		expErr            error
	}{
		{
			name: "No changes",
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
					SecretRef: &v1alpha1.SecretRefStatus{
						Secret: v1alpha1.Secret{
							Name:    "acceptor-secret",
							Key:     "data",
							Backend: "kubernetes",
						},
						ResourceVersion: "1",
					},
				},
			},
			existingSecret: func() *corev1.Secret {
				secret := createSecret("acceptor-secret", "default", "data", "foo")
				secret.ResourceVersion = "1"
				return secret
			},
			expShouldGenerate: false,
			expNameChanged:    false,
			expErr:            nil,
		},
		{
			name: "Key was changed",
			peeringAcceptor: &v1alpha1.PeeringAcceptor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "acceptor",
					Namespace: "default",
				},
				Spec: v1alpha1.PeeringAcceptorSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "acceptor-secret",
							Key:     "data-new",
							Backend: "kubernetes",
						},
					},
				},
				Status: v1alpha1.PeeringAcceptorStatus{
					SecretRef: &v1alpha1.SecretRefStatus{
						Secret: v1alpha1.Secret{
							Name:    "acceptor-secret",
							Key:     "data-old",
							Backend: "kubernetes",
						},
						ResourceVersion: "1",
					},
				},
			},
			existingSecret: func() *corev1.Secret {
				secret := createSecret("acceptor-secret", "default", "data-old", "foo")
				secret.ResourceVersion = "1"
				return secret
			},
			expShouldGenerate: true,
			expNameChanged:    false,
			expErr:            nil,
		},
		{
			name: "Name changed",
			peeringAcceptor: &v1alpha1.PeeringAcceptor{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "acceptor",
					Namespace: "default",
				},
				Spec: v1alpha1.PeeringAcceptorSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "acceptor-secret-new",
							Key:     "data",
							Backend: "kubernetes",
						},
					},
				},
				Status: v1alpha1.PeeringAcceptorStatus{
					SecretRef: &v1alpha1.SecretRefStatus{
						Secret: v1alpha1.Secret{
							Name:    "acceptor-secret-old",
							Key:     "data",
							Backend: "kubernetes",
						},
						ResourceVersion: "1",
					},
				},
			},
			existingSecret: func() *corev1.Secret {
				secret := createSecret("acceptor-secret-old", "default", "data", "foo")
				secret.ResourceVersion = "1"
				return secret
			},
			expShouldGenerate: true,
			expNameChanged:    true,
			expErr:            nil,
		},
		{
			name: "Contents changed",
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
					SecretRef: &v1alpha1.SecretRefStatus{
						Secret: v1alpha1.Secret{
							Name:    "acceptor-secret",
							Key:     "data",
							Backend: "kubernetes",
						},
						ResourceVersion: "1",
					},
				},
			},
			// existingSecret resource version is different from status, signalling the contents have changed.
			existingSecret: func() *corev1.Secret {
				secret := createSecret("acceptor-secret", "default", "data", "foo")
				secret.ResourceVersion = "12345"
				return secret
			},
			expShouldGenerate: true,
			expNameChanged:    false,
			expErr:            nil,
		},
		{
			name: "Error case",
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
							Backend: "different-backend",
						},
					},
				},
				Status: v1alpha1.PeeringAcceptorStatus{
					SecretRef: &v1alpha1.SecretRefStatus{
						Secret: v1alpha1.Secret{
							Name:    "acceptor-secret",
							Key:     "data",
							Backend: "kubernetes",
						},
						ResourceVersion: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
					},
				},
			},
			existingSecret: func() *corev1.Secret {
				secret := createSecret("acceptor-secret", "default", "data", "foo")
				return secret
			},
			expShouldGenerate: false,
			expNameChanged:    false,
			expErr:            errors.New("PeeringAcceptor backend cannot be changed"),
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			shouldGenerate, nameChanged, err := shouldGenerateToken(tt.peeringAcceptor, tt.existingSecret())
			if tt.expErr == nil {
				require.NoError(t, err)
				require.Equal(t, shouldGenerate, tt.expShouldGenerate)
				require.Equal(t, nameChanged, tt.expNameChanged)
			} else {
				require.EqualError(t, err, tt.expErr.Error())
			}

		})
	}
}

func TestAcceptorUpdateStatus(t *testing.T) {
	cases := []struct {
		name            string
		peeringAcceptor *v1alpha1.PeeringAcceptor
		resourceVersion string
		expStatus       v1alpha1.PeeringAcceptorStatus
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
			resourceVersion: "1234",
			expStatus: v1alpha1.PeeringAcceptorStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "acceptor-secret",
						Key:     "data",
						Backend: "kubernetes",
					},
					ResourceVersion: "1234",
				},
				ReconcileError: &v1alpha1.ReconcileErrorStatus{
					Error:   pointerToBool(false),
					Message: pointerToString(""),
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
					SecretRef: &v1alpha1.SecretRefStatus{
						Secret: v1alpha1.Secret{
							Name:    "old-name",
							Key:     "old-key",
							Backend: "kubernetes",
						},
						ResourceVersion: "old-resource-version",
					},
				},
			},
			resourceVersion: "1234",
			expStatus: v1alpha1.PeeringAcceptorStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "acceptor-secret",
						Key:     "data",
						Backend: "kubernetes",
					},
					ResourceVersion: "1234",
				},
				ReconcileError: &v1alpha1.ReconcileErrorStatus{
					Error:   pointerToBool(false),
					Message: pointerToString(""),
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

			err := pac.updateStatus(context.Background(), tt.peeringAcceptor, tt.resourceVersion)
			require.NoError(t, err)

			acceptor := &v1alpha1.PeeringAcceptor{}
			acceptorName := types.NamespacedName{
				Name:      "acceptor",
				Namespace: "default",
			}
			err = fakeClient.Get(context.Background(), acceptorName, acceptor)
			require.NoError(t, err)
			require.Equal(t, tt.expStatus.SecretRef.Name, acceptor.SecretRef().Name)
			require.Equal(t, tt.expStatus.SecretRef.Key, acceptor.SecretRef().Key)
			require.Equal(t, tt.expStatus.SecretRef.Backend, acceptor.SecretRef().Backend)
			require.Equal(t, tt.expStatus.SecretRef.ResourceVersion, acceptor.SecretRef().ResourceVersion)
			require.Equal(t, *tt.expStatus.ReconcileError.Error, *acceptor.Status.ReconcileError.Error)

		})
	}
}

func TestAcceptorUpdateStatusError(t *testing.T) {
	cases := []struct {
		name         string
		acceptor     *v1alpha1.PeeringAcceptor
		reconcileErr error
		expStatus    v1alpha1.PeeringAcceptorStatus
	}{
		{
			name: "updates status when there's no existing status",
			acceptor: &v1alpha1.PeeringAcceptor{
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
			reconcileErr: errors.New("this is an error"),
			expStatus: v1alpha1.PeeringAcceptorStatus{
				ReconcileError: &v1alpha1.ReconcileErrorStatus{
					Error:   pointerToBool(true),
					Message: pointerToString("this is an error"),
				},
			},
		},
		{
			name: "updates status when there is an existing status",
			acceptor: &v1alpha1.PeeringAcceptor{
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
					ReconcileError: &v1alpha1.ReconcileErrorStatus{
						Error:   pointerToBool(false),
						Message: pointerToString(""),
					},
				},
			},
			reconcileErr: errors.New("this is an error"),
			expStatus: v1alpha1.PeeringAcceptorStatus{
				ReconcileError: &v1alpha1.ReconcileErrorStatus{
					Error:   pointerToBool(true),
					Message: pointerToString("this is an error"),
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
			k8sObjects = append(k8sObjects, tt.acceptor)

			// Add peering types to the scheme.
			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringAcceptor{}, &v1alpha1.PeeringAcceptorList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()
			// Create the peering acceptor controller.
			controller := &PeeringAcceptorController{
				Client: fakeClient,
				Log:    logrtest.TestLogger{T: t},
				Scheme: s,
			}

			controller.updateStatusError(context.Background(), tt.acceptor, tt.reconcileErr)

			acceptor := &v1alpha1.PeeringAcceptor{}
			acceptorName := types.NamespacedName{
				Name:      "acceptor",
				Namespace: "default",
			}
			err := fakeClient.Get(context.Background(), acceptorName, acceptor)
			require.NoError(t, err)
			require.Equal(t, *tt.expStatus.ReconcileError.Error, *acceptor.Status.ReconcileError.Error)

		})
	}
}
