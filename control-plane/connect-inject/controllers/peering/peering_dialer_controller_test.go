// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package peering

import (
	"context"
	"errors"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

// TestReconcile_CreateUpdatePeeringDialer creates a peering dialer.
func TestReconcile_CreateUpdatePeeringDialer(t *testing.T) {
	t.Parallel()
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
		"Initiates peering when version annotation is set": {
			peeringName: "peering",
			k8sObjects: func() []runtime.Object {
				dialer := &v1alpha1.PeeringDialer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "peering",
						Namespace: "default",
						Annotations: map[string]string{
							constants.AnnotationPeeringVersion: "2",
						},
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
				LatestPeeringVersion: ptr.To(uint64(2)),
			},
			peeringExists: true,
		},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {

			// Create test consul server.
			acceptorPeerServer, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				// We set the datacenter because the server name, typically formatted as "server.<datacenter>.<domain>"
				// must be unique on the acceptor and dialer peers. Otherwise the following consul error will be thrown:
				// https://github.com/hashicorp/consul/blob/74b87d49d33069a048aead7a86d85d4b4b6461b5/agent/rpc/peering/service.go#L491.
				c.Datacenter = "acceptor-dc"
			})
			require.NoError(t, err)
			defer acceptorPeerServer.Stop()
			acceptorPeerServer.WaitForServiceIntentions(t)
			acceptorPeerServer.WaitForActiveCARoot(t)

			cfg := &api.Config{
				Address: acceptorPeerServer.HTTPAddr,
			}
			acceptorClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), &ns)

			// Generate a token.
			var encodedPeeringToken string
			if tt.peeringName != "" {
				// Create the initial token.
				retry.Run(t, func(r *retry.R) {
					baseToken, _, err := acceptorClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: tt.peeringName}, nil)
					require.NoError(r, err)
					encodedPeeringToken = baseToken.PeeringToken
				})
			}

			// If the peering is not supposed to already exist in Consul, then create a secret with the generated token.
			if !tt.peeringExists {
				secret := tt.peeringSecret(encodedPeeringToken)
				if secret != nil {
					secret.SetResourceVersion("latest-version")
					k8sObjects = append(k8sObjects, secret)
				}
			}

			// Create test consul server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			dialerClient := testClient.APIClient
			testClient.TestServer.WaitForActiveCARoot(t)

			// If the peering is supposed to already exist in Consul, then establish a peering with the existing token, so the peering will exist on the dialing side.
			if tt.peeringExists {
				retry.Run(t, func(r *retry.R) {
					_, _, err = dialerClient.Peerings().Establish(context.Background(), api.PeeringEstablishRequest{PeerName: tt.peeringName, PeeringToken: encodedPeeringToken}, nil)
					require.NoError(r, err)
				})

				k8sObjects = append(k8sObjects, createSecret("dialer-token-old", "default", "token", "old-token"))
				// Create a new token to be used by Reconcile(). The original token has already been
				// used once to simulate establishing an existing peering.
				baseToken, _, err := acceptorClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: tt.peeringName}, nil)
				require.NoError(t, err)
				secret := tt.peeringSecret(baseToken.PeeringToken)
				secret.SetResourceVersion("latest-version")
				k8sObjects = append(k8sObjects, secret)
			}
			s := runtime.NewScheme()
			corev1.AddToScheme(s)
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringDialer{}, &v1alpha1.PeeringDialerList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(k8sObjects...).
				WithStatusSubresource(&v1alpha1.PeeringDialer{}).
				Build()

			// Create the peering dialer controller
			controller := &PeeringDialerController{
				Client:              fakeClient,
				Log:                 logrtest.New(t),
				ConsulClientConfig:  testClient.Cfg,
				ConsulServerConnMgr: testClient.Watcher,
				Scheme:              s,
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
					require.Equal(t, tt.expectedStatus.LatestPeeringVersion, dialer.Status.LatestPeeringVersion)
					require.Contains(t, dialer.Finalizers, finalizerName)
					require.NotEmpty(t, dialer.SecretRef().ResourceVersion)
					require.NotEqual(t, "test-version", dialer.SecretRef().ResourceVersion)
				}
			}
		})
	}
}

func TestReconcile_VersionAnnotationPeeringDialer(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		annotations    map[string]string
		expErr         string
		expectedStatus *v1alpha1.PeeringDialerStatus
	}{
		"fails if annotation is not a number": {
			annotations: map[string]string{
				constants.AnnotationPeeringVersion: "foo",
			},
			expErr: `strconv.ParseUint: parsing "foo": invalid syntax`,
		},
		"is no/op if annotation value is less than value in status": {
			annotations: map[string]string{
				constants.AnnotationPeeringVersion: "2",
			},
			expectedStatus: &v1alpha1.PeeringDialerStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "dialer-token",
						Key:     "token",
						Backend: "kubernetes",
					},
				},
				LatestPeeringVersion: ptr.To(uint64(3)),
			},
		},
		"is no/op if annotation value is equal to value in status": {
			annotations: map[string]string{
				constants.AnnotationPeeringVersion: "3",
			},
			expectedStatus: &v1alpha1.PeeringDialerStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "dialer-token",
						Key:     "token",
						Backend: "kubernetes",
					},
				},
				LatestPeeringVersion: ptr.To(uint64(3)),
			},
		},
		"updates if annotation value is greater than value in status": {
			annotations: map[string]string{
				constants.AnnotationPeeringVersion: "4",
			},
			expectedStatus: &v1alpha1.PeeringDialerStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "dialer-token",
						Key:     "token",
						Backend: "kubernetes",
					},
				},
				LatestPeeringVersion: ptr.To(uint64(4)),
			},
		},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {

			// Create test consul server.
			acceptorPeerServer, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				// We set different cluster id for the connect CA because the server name,
				// typically formatted as server.dc1.peering.<cluster_id>.consul
				// must be unique on the acceptor and dialer peers.
				c.Connect["ca_config"] = map[string]interface{}{
					"cluster_id": "00000000-2222-3333-4444-555555555555",
				}
			})
			require.NoError(t, err)
			defer acceptorPeerServer.Stop()
			acceptorPeerServer.WaitForServiceIntentions(t)
			acceptorPeerServer.WaitForActiveCARoot(t)

			cfg := &api.Config{
				Address: acceptorPeerServer.HTTPAddr,
			}
			acceptorClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			dialer := &v1alpha1.PeeringDialer{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "peering",
					Namespace:   "default",
					Annotations: tt.annotations,
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
						ResourceVersion: "latest-version",
					},
					LatestPeeringVersion: ptr.To(uint64(3)),
				},
			}
			// Create fake k8s client
			k8sObjects := []runtime.Object{dialer, ns}

			// Create a peering connection in Consul by generating and establishing a peering connection before calling
			// Reconcile().
			// Generate a token.
			var generatedToken *api.PeeringGenerateTokenResponse
			retry.Run(t, func(r *retry.R) {
				generatedToken, _, err = acceptorClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: "peering"}, nil)
				require.NoError(r, err)
			})

			// Create test consul server.
			var testServerCfg *testutil.TestServerConfig
			dialerPeerServer, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				testServerCfg = c
			})
			require.NoError(t, err)
			defer dialerPeerServer.Stop()
			dialerPeerServer.WaitForServiceIntentions(t)
			dialerPeerServer.WaitForActiveCARoot(t)

			consulConfig := &consul.Config{
				APIClientConfig: &api.Config{Address: dialerPeerServer.HTTPAddr},
				HTTPPort:        testServerCfg.Ports.HTTP,
			}
			dialerClient, err := api.NewClient(consulConfig.APIClientConfig)
			require.NoError(t, err)

			ctx, cancelFunc := context.WithCancel(context.Background())
			t.Cleanup(cancelFunc)
			watcher, err := discovery.NewWatcher(ctx, discovery.Config{Addresses: "127.0.0.1", GRPCPort: testServerCfg.Ports.GRPC}, hclog.NewNullLogger())
			require.NoError(t, err)
			t.Cleanup(watcher.Stop)
			go watcher.Run()

			// Establish a peering with the generated token.
			retry.Run(t, func(r *retry.R) {
				_, _, err = dialerClient.Peerings().Establish(context.Background(), api.PeeringEstablishRequest{PeerName: "peering", PeeringToken: generatedToken.PeeringToken}, nil)
				require.NoError(r, err)
			})

			k8sObjects = append(k8sObjects, createSecret("dialer-token-old", "default", "token", "old-token"))

			// Create a new token to be potentially used by Reconcile(). The original token has already been
			// used once to simulate establishing an existing peering.
			token, _, err := acceptorClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: "peering"}, nil)
			require.NoError(t, err)
			secret := createSecret("dialer-token", "default", "token", token.PeeringToken)
			secret.SetResourceVersion("latest-version")
			k8sObjects = append(k8sObjects, secret)

			s := runtime.NewScheme()
			corev1.AddToScheme(s)
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringDialer{}, &v1alpha1.PeeringDialerList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(k8sObjects...).
				WithStatusSubresource(&v1alpha1.PeeringDialer{}).
				Build()

			// Create the peering dialer controller
			controller := &PeeringDialerController{
				Client:              fakeClient,
				Log:                 logrtest.New(t),
				ConsulClientConfig:  consulConfig,
				ConsulServerConnMgr: watcher,
				Scheme:              s,
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

				// Get the reconciled PeeringDialer and make assertions on the status
				dialer := &v1alpha1.PeeringDialer{}
				err = fakeClient.Get(context.Background(), namespacedName, dialer)
				require.NoError(t, err)
				if tt.expectedStatus != nil {
					require.Equal(t, tt.expectedStatus.SecretRef.Name, dialer.SecretRef().Name)
					require.Equal(t, tt.expectedStatus.SecretRef.Key, dialer.SecretRef().Key)
					require.Equal(t, tt.expectedStatus.SecretRef.Backend, dialer.SecretRef().Backend)
					require.Equal(t, "latest-version", dialer.SecretRef().ResourceVersion)
					require.Equal(t, tt.expectedStatus.LatestPeeringVersion, dialer.Status.LatestPeeringVersion)
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
	// Add the default namespace.
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}

	dialer := &v1alpha1.PeeringDialer{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "dialer-deleted",
			Namespace:         "default",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{finalizerName},
		},
		Spec: v1alpha1.PeeringDialerSpec{
			Peer: &v1alpha1.Peer{
				Secret: nil,
			},
		},
	}

	// Create fake k8s client.
	k8sObjects := []runtime.Object{ns, dialer}

	// Add peering types to the scheme.
	s := runtime.NewScheme()
	corev1.AddToScheme(s)
	s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringDialer{}, &v1alpha1.PeeringDialerList{})
	fakeClient := fake.NewClientBuilder().WithScheme(s).
		WithRuntimeObjects(k8sObjects...).
		WithStatusSubresource(&v1alpha1.PeeringDialer{}).
		Build()

	// Create test consul server.
	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	consulClient := testClient.APIClient
	testClient.TestServer.WaitForActiveCARoot(t)

	// Add the initial peerings into Consul by calling the Generate token endpoint.
	_, _, err := consulClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: "dialer-deleted"}, nil)
	require.NoError(t, err)

	// Create the peering dialer controller.
	pdc := &PeeringDialerController{
		Client:              fakeClient,
		Log:                 logrtest.New(t),
		ConsulClientConfig:  testClient.Cfg,
		ConsulServerConnMgr: testClient.Watcher,
		Scheme:              s,
	}
	namespacedName := types.NamespacedName{
		Name:      "dialer-deleted",
		Namespace: "default",
	}

	// Reconcile a resource that is not in K8s, but is still in Consul.
	resp, err := pdc.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: namespacedName,
	})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	// After reconciliation, Consul should not have the peering.
	timer := &retry.Timer{Timeout: 5 * time.Second, Wait: 500 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		peering, _, err := consulClient.Peerings().Read(context.Background(), "dialer-deleted", nil)
		require.Nil(r, peering)
		require.NoError(r, err)
	})

	err = fakeClient.Get(context.Background(), namespacedName, dialer)
	require.EqualError(t, err, `peeringdialers.consul.hashicorp.com "dialer-deleted" not found`)
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
				Conditions: v1alpha1.Conditions{
					{
						Type:   v1alpha1.ConditionSynced,
						Status: corev1.ConditionTrue,
					},
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
				Conditions: v1alpha1.Conditions{
					{
						Type:   v1alpha1.ConditionSynced,
						Status: corev1.ConditionTrue,
					},
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
			s := runtime.NewScheme()
			corev1.AddToScheme(s)
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringDialer{}, &v1alpha1.PeeringDialerList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(k8sObjects...).
				WithStatusSubresource(&v1alpha1.PeeringDialer{}).
				Build()
			// Create the peering dialer controller.
			controller := &PeeringDialerController{
				Client: fakeClient,
				Log:    logrtest.New(t),
				Scheme: s,
			}

			err := controller.updateStatus(context.Background(), types.NamespacedName{Name: tt.peeringDialer.Name, Namespace: tt.peeringDialer.Namespace}, tt.resourceVersion)
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
			require.Equal(t, tt.expStatus.Conditions[0].Message, dialer.Status.Conditions[0].Message)
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
				Conditions: v1alpha1.Conditions{
					{
						Type:    v1alpha1.ConditionSynced,
						Status:  corev1.ConditionFalse,
						Reason:  internalError,
						Message: "this is an error",
					},
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
					Conditions: v1alpha1.Conditions{
						{
							Type:   v1alpha1.ConditionSynced,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			reconcileErr: errors.New("this is an error"),
			expStatus: v1alpha1.PeeringDialerStatus{
				Conditions: v1alpha1.Conditions{
					{
						Type:    v1alpha1.ConditionSynced,
						Status:  corev1.ConditionFalse,
						Reason:  internalError,
						Message: "this is an error",
					},
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
			s := runtime.NewScheme()
			corev1.AddToScheme(s)
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringDialer{}, &v1alpha1.PeeringDialerList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(k8sObjects...).
				WithStatusSubresource(&v1alpha1.PeeringDialer{}).
				Build()

			// Create the peering dialer controller.
			controller := &PeeringDialerController{
				Client: fakeClient,
				Log:    logrtest.New(t),
				Scheme: s,
			}

			controller.updateStatusError(context.Background(), tt.dialer, internalError, tt.reconcileErr)

			dialer := &v1alpha1.PeeringDialer{}
			dialerName := types.NamespacedName{
				Name:      "dialer",
				Namespace: "default",
			}
			err := fakeClient.Get(context.Background(), dialerName, dialer)
			require.NoError(t, err)
			require.Len(t, dialer.Status.Conditions, 1)
			require.Equal(t, tt.expStatus.Conditions[0].Message, dialer.Status.Conditions[0].Message)

		})
	}
}

func TestDialer_FilterPeeringDialers(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		secret *corev1.Secret
		result bool
	}{
		"returns true if label is set to true": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						constants.LabelPeeringToken: "true",
					},
				},
			},
			result: true,
		},
		"returns false if label is set to false": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						constants.LabelPeeringToken: "false",
					},
				},
			},
			result: false,
		},
		"returns false if label is set to a non-true value": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
					Labels: map[string]string{
						constants.LabelPeeringToken: "foo",
					},
				},
			},
			result: false,
		},
		"returns false if label is not set": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			result: false,
		},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			controller := PeeringDialerController{}
			result := controller.filterPeeringDialers(tt.secret)
			require.Equal(t, tt.result, result)
		})
	}
}

func TestDialer_RequestsForPeeringTokens(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		secret  *corev1.Secret
		dialers v1alpha1.PeeringDialerList
		result  []reconcile.Request
	}{
		"secret matches existing acceptor": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			dialers: v1alpha1.PeeringDialerList{
				Items: []v1alpha1.PeeringDialer{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering",
							Namespace: "test",
						},
						Spec: v1alpha1.PeeringDialerSpec{
							Peer: &v1alpha1.Peer{
								Secret: &v1alpha1.Secret{
									Name:    "test",
									Key:     "test",
									Backend: "kubernetes",
								},
							},
						},
					},
				},
			},
			result: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: "test",
						Name:      "peering",
					},
				},
			},
		},
		"does not match if backend is not kubernetes": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			dialers: v1alpha1.PeeringDialerList{
				Items: []v1alpha1.PeeringDialer{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering",
							Namespace: "test",
						},
						Spec: v1alpha1.PeeringDialerSpec{
							Peer: &v1alpha1.Peer{
								Secret: &v1alpha1.Secret{
									Name:    "test",
									Key:     "test",
									Backend: "vault",
								},
							},
						},
					},
				},
			},
			result: []reconcile.Request{},
		},
		"only matches with the correct dialer": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			dialers: v1alpha1.PeeringDialerList{
				Items: []v1alpha1.PeeringDialer{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering-1",
							Namespace: "test",
						},
						Spec: v1alpha1.PeeringDialerSpec{
							Peer: &v1alpha1.Peer{
								Secret: &v1alpha1.Secret{
									Name:    "test",
									Key:     "test",
									Backend: "kubernetes",
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering-2",
							Namespace: "test-2",
						},
						Spec: v1alpha1.PeeringDialerSpec{
							Peer: &v1alpha1.Peer{
								Secret: &v1alpha1.Secret{
									Name:    "test",
									Key:     "test",
									Backend: "kubernetes",
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering-3",
							Namespace: "test",
						},
						Spec: v1alpha1.PeeringDialerSpec{
							Peer: &v1alpha1.Peer{
								Secret: &v1alpha1.Secret{
									Name:    "test-2",
									Key:     "test",
									Backend: "kubernetes",
								},
							},
						},
					},
				},
			},
			result: []reconcile.Request{
				{
					NamespacedName: types.NamespacedName{
						Namespace: "test",
						Name:      "peering-1",
					},
				},
			},
		},
		"can match with zero dialer": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			dialers: v1alpha1.PeeringDialerList{
				Items: []v1alpha1.PeeringDialer{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering-1",
							Namespace: "test",
						},
						Spec: v1alpha1.PeeringDialerSpec{
							Peer: &v1alpha1.Peer{
								Secret: &v1alpha1.Secret{
									Name:    "fest",
									Key:     "test",
									Backend: "kubernetes",
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering-2",
							Namespace: "test-2",
						},
						Spec: v1alpha1.PeeringDialerSpec{
							Peer: &v1alpha1.Peer{
								Secret: &v1alpha1.Secret{
									Name:    "test",
									Key:     "test",
									Backend: "kubernetes",
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering-3",
							Namespace: "test",
						},
						Spec: v1alpha1.PeeringDialerSpec{
							Peer: &v1alpha1.Peer{
								Secret: &v1alpha1.Secret{
									Name:    "test-2",
									Key:     "test",
									Backend: "kubernetes",
								},
							},
						},
					},
				},
			},
			result: []reconcile.Request{},
		},
	}

	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			s := runtime.NewScheme()
			corev1.AddToScheme(s)
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringDialer{}, &v1alpha1.PeeringDialerList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithRuntimeObjects(tt.secret, &tt.dialers).
				WithStatusSubresource(&v1alpha1.PeeringDialer{}).
				Build()
			controller := PeeringDialerController{
				Client: fakeClient,
				Log:    logrtest.New(t),
			}
			result := controller.requestsForPeeringTokens(context.Background(), tt.secret)

			require.Equal(t, tt.result, result)
		})
	}
}
