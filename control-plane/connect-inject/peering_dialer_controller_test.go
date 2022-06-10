package connectinject

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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

// TestReconcileCreateUpdatePeeringDialer creates a peering dialer.
func TestReconcileCreateUpdatePeeringDialer(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	node2Name := "test-node2"
	cases := map[string]struct {
		peeringName            string
		k8sObjects             func() []runtime.Object
		expectedConsulPeerings *api.Peering
		peeringSecret          func(token string) *corev1.Secret
		expErr                 string
		expectedStatus         *v1alpha1.PeeringDialerStatus
		expectDeletedK8sSecret *types.NamespacedName
		peeringExists          bool
	}{
		"Errors when Secret is not set on the spec": {
			k8sObjects: func() []runtime.Object {
				dialer := &v1alpha1.PeeringDialer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "peering",
						Namespace: "default",
					},
					Spec: v1alpha1.PeeringDialerSpec{
						Peer: &v1alpha1.Peer{
							Secret: nil,
						},
					},
				}
				return []runtime.Object{dialer}
			},
			expErr:        "PeeringDialer spec.peer.secret was not set",
			peeringSecret: func(_ string) *corev1.Secret { return nil },
		},
		"Errors when Secret set on the spec does not exist in the cluster": {
			k8sObjects: func() []runtime.Object {
				dialer := &v1alpha1.PeeringDialer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "peering",
						Namespace: "default",
					},
					Spec: v1alpha1.PeeringDialerSpec{
						Peer: &v1alpha1.Peer{
							Secret: &v1alpha1.Secret{
								Name:    "dialer",
								Key:     "token",
								Backend: "kubernetes",
							},
						},
					},
				}
				return []runtime.Object{dialer}
			},
			expErr:        "PeeringDialer spec.peer.secret does not exist",
			peeringSecret: func(_ string) *corev1.Secret { return nil },
		},
		"Initiates peering when status secret is nil": {
			peeringName: "peering",
			k8sObjects: func() []runtime.Object {
				dialer := &v1alpha1.PeeringDialer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "peering",
						Namespace: "default",
					},
					Spec: v1alpha1.PeeringDialerSpec{
						Peer: &v1alpha1.Peer{
							Secret: &v1alpha1.Secret{
								Name:    "dialer-token",
								Key:     "token",
								Backend: "kubernetes",
							},
						},
					},
				}
				return []runtime.Object{dialer}
			},
			expectedConsulPeerings: &api.Peering{
				Name:  "peering",
				State: api.PeeringStateActive,
			},
			peeringSecret: func(token string) *corev1.Secret {
				return createSecret("dialer-token", "default", "token", token)
			},
			expectedStatus: &v1alpha1.PeeringDialerStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "dialer-token",
						Key:     "token",
						Backend: "kubernetes",
					},
				},
			},
		},
		"Initiates peering when status secret is set but peering is not found in Consul": {
			peeringName: "peering",
			k8sObjects: func() []runtime.Object {
				dialer := &v1alpha1.PeeringDialer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "peering",
						Namespace: "default",
					},
					Spec: v1alpha1.PeeringDialerSpec{
						Peer: &v1alpha1.Peer{
							Secret: &v1alpha1.Secret{
								Name:    "dialer-token",
								Key:     "token",
								Backend: "kubernetes",
							},
						},
					},
					Status: v1alpha1.PeeringDialerStatus{
						SecretRef: &v1alpha1.SecretRefStatus{
							Secret: v1alpha1.Secret{
								Name:    "dialer-token",
								Key:     "token",
								Backend: "kubernetes",
							},
							ResourceVersion: "test-version",
						},
					},
				}
				return []runtime.Object{dialer}
			},
			expectedConsulPeerings: &api.Peering{
				Name:  "peering",
				State: api.PeeringStateActive,
			},
			peeringSecret: func(token string) *corev1.Secret {
				return createSecret("dialer-token", "default", "token", token)
			},
			expectedStatus: &v1alpha1.PeeringDialerStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "dialer-token",
						Key:     "token",
						Backend: "kubernetes",
					},
				},
			},
		},
		"Initiates peering when status secret is set, peering is found, but out of date": {
			peeringName: "peering",
			k8sObjects: func() []runtime.Object {
				dialer := &v1alpha1.PeeringDialer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "peering",
						Namespace: "default",
					},
					Spec: v1alpha1.PeeringDialerSpec{
						Peer: &v1alpha1.Peer{
							Secret: &v1alpha1.Secret{
								Name:    "dialer-token",
								Key:     "token",
								Backend: "kubernetes",
							},
						},
					},
					Status: v1alpha1.PeeringDialerStatus{
						SecretRef: &v1alpha1.SecretRefStatus{
							Secret: v1alpha1.Secret{
								Name:    "dialer-token-old",
								Key:     "token",
								Backend: "kubernetes",
							},
							ResourceVersion: "test-version",
						},
					},
				}
				return []runtime.Object{dialer}
			},
			expectedConsulPeerings: &api.Peering{
				Name:  "peering",
				State: api.PeeringStateActive,
			},
			peeringSecret: func(token string) *corev1.Secret {
				return createSecret("dialer-token", "default", "token", token)
			},
			expectedStatus: &v1alpha1.PeeringDialerStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "dialer-token",
						Key:     "token",
						Backend: "kubernetes",
					},
				},
			},
			peeringExists: true,
		},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {

			// Create test consul server.
			acceptorPeerServer, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = nodeName
			})
			require.NoError(t, err)
			defer acceptorPeerServer.Stop()
			acceptorPeerServer.WaitForServiceIntentions(t)

			cfg := &api.Config{
				Address: acceptorPeerServer.HTTPAddr,
			}
			acceptorClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), &ns)

			// This is responsible for updating the token generated by the acceptor side with the IP
			// of the Consul server as the generated token currently does not have that set on it.
			var encodedPeeringToken string
			if tt.peeringName != "" {
				var token struct {
					CA              string
					ServerAddresses []string
					ServerName      string
					PeerID          string
				}
				// Create the initial token.
				baseToken, _, err := acceptorClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: tt.peeringName}, nil)
				require.NoError(t, err)
				// Decode the token to extract the ServerName and PeerID from the token. CA is always NULL.
				decodeBytes, err := base64.StdEncoding.DecodeString(baseToken.PeeringToken)
				require.NoError(t, err)
				err = json.Unmarshal(decodeBytes, &token)
				require.NoError(t, err)
				// Get the IP of the Consul server.
				addr := strings.Split(acceptorPeerServer.HTTPAddr, ":")[0]
				// Generate expected token for Peering Initiate.
				tokenString := fmt.Sprintf(`{"CA":null,"ServerAddresses":["%s:8300"],"ServerName":"%s","PeerID":"%s"}`, addr, token.ServerName, token.PeerID)
				// Create peering initiate secret in Kubernetes.
				encodedPeeringToken = base64.StdEncoding.EncodeToString([]byte(tokenString))
				secret := tt.peeringSecret(encodedPeeringToken)
				secret.SetResourceVersion("latest-version")
				k8sObjects = append(k8sObjects, secret)
			}

			// Create test consul server.
			dialerPeerServer, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.NodeName = node2Name
			})
			require.NoError(t, err)
			defer dialerPeerServer.Stop()
			dialerPeerServer.WaitForServiceIntentions(t)

			cfg = &api.Config{
				Address: dialerPeerServer.HTTPAddr,
			}
			dialerClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			if tt.peeringExists {
				_, _, err := dialerClient.Peerings().Establish(context.Background(), api.PeeringEstablishRequest{PeerName: tt.peeringName, PeeringToken: encodedPeeringToken}, nil)
				require.NoError(t, err)
				k8sObjects = append(k8sObjects, createSecret("dialer-token-old", "default", "token", "old-token"))
			}

			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringDialer{}, &v1alpha1.PeeringDialerList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			// Create the peering dialer controller
			controller := &PeeringDialerController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: dialerClient,
				Scheme:       s,
			}
			namespacedName := types.NamespacedName{
				Name:      "peering",
				Namespace: "default",
			}

			resp, err := controller.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
				require.False(t, resp.Requeue)

				// After reconciliation, Consul should have the peering.
				peering, _, err := dialerClient.Peerings().Read(context.Background(), "peering", nil)
				require.NoError(t, err)
				require.Equal(t, tt.expectedConsulPeerings.Name, peering.Name)
				// TODO(peering): update this assertion once peering states are supported.
				//require.Equal(t, api.PeeringStateActive, peering.State)
				require.NotEmpty(t, peering.ID)

				// Get the reconciled PeeringDialer and make assertions on the status
				dialer := &v1alpha1.PeeringDialer{}
				err = fakeClient.Get(context.Background(), namespacedName, dialer)
				require.NoError(t, err)
				if tt.expectedStatus != nil {
					require.Equal(t, tt.expectedStatus.SecretRef.Name, dialer.SecretRef().Name)
					require.Equal(t, tt.expectedStatus.SecretRef.Key, dialer.SecretRef().Key)
					require.Equal(t, tt.expectedStatus.SecretRef.Backend, dialer.SecretRef().Backend)
					require.Equal(t, "latest-version", dialer.SecretRef().ResourceVersion)
					require.NotEmpty(t, dialer.SecretRef().ResourceVersion)
					require.NotEqual(t, "test-version", dialer.SecretRef().ResourceVersion)
				}
			}
		})
	}
}

// TestSpecStatusSecretsDifferent tests that the correct result is returned
// when comparing the secret in the status against the existing secret.
func TestSpecStatusSecretsDifferent(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		dialer      *v1alpha1.PeeringDialer
		secret      *corev1.Secret
		isDifferent bool
	}{
		"different secret name in spec and status": {
			dialer: &v1alpha1.PeeringDialer{
				Spec: v1alpha1.PeeringDialerSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "foo",
							Key:     "token",
							Backend: "kubernetes",
						},
					},
				},
				Status: v1alpha1.PeeringDialerStatus{
					SecretRef: &v1alpha1.SecretRefStatus{
						Secret: v1alpha1.Secret{
							Name:    "bar",
							Key:     "token",
							Backend: "kubernetes",
						},
					},
				},
			},
			secret:      nil,
			isDifferent: true,
		},
		"different secret key in spec and status": {
			dialer: &v1alpha1.PeeringDialer{
				Spec: v1alpha1.PeeringDialerSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "foo",
							Key:     "token",
							Backend: "kubernetes",
						},
					},
				},
				Status: v1alpha1.PeeringDialerStatus{
					SecretRef: &v1alpha1.SecretRefStatus{
						Secret: v1alpha1.Secret{
							Name:    "foo",
							Key:     "key",
							Backend: "kubernetes",
						},
					},
				},
			},
			secret:      nil,
			isDifferent: true,
		},
		"different secret backend in spec and status": {
			dialer: &v1alpha1.PeeringDialer{
				Spec: v1alpha1.PeeringDialerSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "foo",
							Key:     "token",
							Backend: "kubernetes",
						},
					},
				},
				Status: v1alpha1.PeeringDialerStatus{
					SecretRef: &v1alpha1.SecretRefStatus{
						Secret: v1alpha1.Secret{
							Name:    "foo",
							Key:     "token",
							Backend: "vault",
						},
					},
				},
			},
			secret:      nil,
			isDifferent: true,
		},
		"different secret ref in status and saved secret": {
			dialer: &v1alpha1.PeeringDialer{
				Spec: v1alpha1.PeeringDialerSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "foo",
							Key:     "token",
							Backend: "kubernetes",
						},
					},
				},
				Status: v1alpha1.PeeringDialerStatus{
					SecretRef: &v1alpha1.SecretRefStatus{
						Secret: v1alpha1.Secret{
							Name:    "foo",
							Key:     "token",
							Backend: "kubernetes",
						},
						ResourceVersion: "version1",
					},
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: "version2",
				},
			},
			isDifferent: true,
		},
		"same secret ref in status and saved secret": {
			dialer: &v1alpha1.PeeringDialer{
				Spec: v1alpha1.PeeringDialerSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "foo",
							Key:     "token",
							Backend: "kubernetes",
						},
					},
				},
				Status: v1alpha1.PeeringDialerStatus{
					SecretRef: &v1alpha1.SecretRefStatus{
						Secret: v1alpha1.Secret{
							Name:    "foo",
							Key:     "token",
							Backend: "kubernetes",
						},
						ResourceVersion: "version1",
					},
				},
			},
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					ResourceVersion: "version1",
				},
			},
			isDifferent: false,
		},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			controller := PeeringDialerController{}
			isDifferent := controller.specStatusSecretsDifferent(tt.dialer, tt.secret)
			require.Equal(t, tt.isDifferent, isDifferent)
		})
	}
}

// TestReconcileDeletePeeringDialer reconciles a PeeringDialer resource that is no longer in Kubernetes, but still
// exists in Consul.
func TestReconcileDeletePeeringDialer(t *testing.T) {
	t.Parallel()
	nodeName := "test-node"
	cases := []struct {
		name                   string
		initialConsulPeerNames []string
		expErr                 string
	}{
		{
			name: "PeeringDialer no longer in K8s, still exists in Consul",
			initialConsulPeerNames: []string{
				"dialer-deleted",
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
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringDialer{}, &v1alpha1.PeeringDialerList{})
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

			// Create the peering dialer controller.
			pdc := &PeeringDialerController{
				Client:       fakeClient,
				Log:          logrtest.TestLogger{T: t},
				ConsulClient: consulClient,
				Scheme:       s,
			}
			namespacedName := types.NamespacedName{
				Name:      "dialer-deleted",
				Namespace: "default",
			}

			// Reconcile a resource that is not in K8s, but is still in Consul.
			resp, err := pdc.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: namespacedName,
			})
			if tt.expErr != "" {
				require.EqualError(t, err, tt.expErr)
			} else {
				require.NoError(t, err)
			}
			require.False(t, resp.Requeue)

			// After reconciliation, Consul should not have the peering.
			peering, _, err := consulClient.Peerings().Read(context.Background(), "dialer-deleted", nil)
			require.Nil(t, peering)
			require.NoError(t, err)
		})
	}
}

func TestDialerUpdateStatus(t *testing.T) {
	cases := []struct {
		name            string
		peeringDialer   *v1alpha1.PeeringDialer
		resourceVersion string
		expStatus       v1alpha1.PeeringDialerStatus
	}{
		{
			name: "updates status when there's no existing status",
			peeringDialer: &v1alpha1.PeeringDialer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dialer",
					Namespace: "default",
				},
				Spec: v1alpha1.PeeringDialerSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "dialer-secret",
							Key:     "data",
							Backend: "kubernetes",
						},
					},
				},
			},
			resourceVersion: "1234",
			expStatus: v1alpha1.PeeringDialerStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "dialer-secret",
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
			peeringDialer: &v1alpha1.PeeringDialer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dialer",
					Namespace: "default",
				},
				Spec: v1alpha1.PeeringDialerSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "dialer-secret",
							Key:     "data",
							Backend: "kubernetes",
						},
					},
				},
				Status: v1alpha1.PeeringDialerStatus{
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
			expStatus: v1alpha1.PeeringDialerStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "dialer-secret",
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
			k8sObjects = append(k8sObjects, tt.peeringDialer)

			// Add peering types to the scheme.
			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringDialer{}, &v1alpha1.PeeringDialerList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()
			// Create the peering dialer controller.
			controller := &PeeringDialerController{
				Client: fakeClient,
				Log:    logrtest.TestLogger{T: t},
				Scheme: s,
			}

			err := controller.updateStatus(context.Background(), tt.peeringDialer, tt.resourceVersion)
			require.NoError(t, err)

			dialer := &v1alpha1.PeeringDialer{}
			dialerName := types.NamespacedName{
				Name:      "dialer",
				Namespace: "default",
			}
			err = fakeClient.Get(context.Background(), dialerName, dialer)
			require.NoError(t, err)
			require.Equal(t, tt.expStatus.SecretRef.Name, dialer.SecretRef().Name)
			require.Equal(t, tt.expStatus.SecretRef.Key, dialer.SecretRef().Key)
			require.Equal(t, tt.expStatus.SecretRef.Backend, dialer.SecretRef().Backend)
			require.Equal(t, tt.expStatus.SecretRef.ResourceVersion, dialer.SecretRef().ResourceVersion)
			require.Equal(t, *tt.expStatus.ReconcileError.Error, *dialer.Status.ReconcileError.Error)
		})
	}
}

func TestDialerUpdateStatusError(t *testing.T) {
	cases := []struct {
		name         string
		dialer       *v1alpha1.PeeringDialer
		reconcileErr error
		expStatus    v1alpha1.PeeringDialerStatus
	}{
		{
			name: "updates status when there's no existing status",
			dialer: &v1alpha1.PeeringDialer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dialer",
					Namespace: "default",
				},
				Spec: v1alpha1.PeeringDialerSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "dialer-secret",
							Key:     "data",
							Backend: "kubernetes",
						},
					},
				},
			},
			reconcileErr: errors.New("this is an error"),
			expStatus: v1alpha1.PeeringDialerStatus{
				ReconcileError: &v1alpha1.ReconcileErrorStatus{
					Error:   pointerToBool(true),
					Message: pointerToString("this is an error"),
				},
			},
		},
		{
			name: "updates status when there is an existing status",
			dialer: &v1alpha1.PeeringDialer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "dialer",
					Namespace: "default",
				},
				Spec: v1alpha1.PeeringDialerSpec{
					Peer: &v1alpha1.Peer{
						Secret: &v1alpha1.Secret{
							Name:    "dialer-secret",
							Key:     "data",
							Backend: "kubernetes",
						},
					},
				},
				Status: v1alpha1.PeeringDialerStatus{
					ReconcileError: &v1alpha1.ReconcileErrorStatus{
						Error:   pointerToBool(false),
						Message: pointerToString(""),
					},
				},
			},
			reconcileErr: errors.New("this is an error"),
			expStatus: v1alpha1.PeeringDialerStatus{
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
			k8sObjects = append(k8sObjects, tt.dialer)

			// Add peering types to the scheme.
			s := scheme.Scheme
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringDialer{}, &v1alpha1.PeeringDialerList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()
			// Create the peering dialer controller.
			controller := &PeeringDialerController{
				Client: fakeClient,
				Log:    logrtest.TestLogger{T: t},
				Scheme: s,
			}

			controller.updateStatusError(context.Background(), tt.dialer, tt.reconcileErr)

			dialer := &v1alpha1.PeeringDialer{}
			dialerName := types.NamespacedName{
				Name:      "dialer",
				Namespace: "default",
			}
			err := fakeClient.Get(context.Background(), dialerName, dialer)
			require.NoError(t, err)
			require.Equal(t, *tt.expStatus.ReconcileError.Error, *dialer.Status.ReconcileError.Error)

		})
	}
}
