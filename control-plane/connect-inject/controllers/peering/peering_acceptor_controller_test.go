// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package peering

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testr"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

// TestReconcile_CreateUpdatePeeringAcceptor creates a peering acceptor.
func TestReconcile_CreateUpdatePeeringAcceptor(t *testing.T) {
	t.Parallel()
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
			name: "PeeringAcceptor generates a token with expose server addresses",
			k8sObjects: func() []runtime.Object {
				service := &corev1.Service{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-expose-servers",
						Namespace: "default",
					},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeLoadBalancer,
					},
					Status: corev1.ServiceStatus{
						LoadBalancer: corev1.LoadBalancerStatus{
							Ingress: []corev1.LoadBalancerIngress{
								{
									IP: "1.1.1.1",
								},
							},
						},
					},
				}
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
				return []runtime.Object{acceptor, service}
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
			name: "PeeringAcceptor version annotation is updated",
			k8sObjects: func() []runtime.Object {
				acceptor := &v1alpha1.PeeringAcceptor{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "acceptor-created",
						Namespace: "default",
						Annotations: map[string]string{
							constants.AnnotationPeeringVersion: "2",
						},
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
								Key:     "data",
								Backend: "kubernetes",
							},
							ResourceVersion: "some-old-sha",
						},
					},
				}
				secret := createSecret("acceptor-created-secret", "default", "data", "some-old-data")
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
				LatestPeeringVersion: pointer.Uint64(2),
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
			name: "PeeringAcceptor status secret exists and doesn't match spec secret when there's no peering in Consul",
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
		{
			name: "Peering exists in Consul, but secret doesn't",
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
			initialConsulPeerName: "acceptor-created",
		},
		{
			name: "Peering exists in Consul, but secret doesn't and status is not set",
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
			initialConsulPeerName: "acceptor-created",
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			// Add the default namespace.
			ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			// Create fake k8s client
			k8sObjects := append(tt.k8sObjects(), &ns)

			s := runtime.NewScheme()
			corev1.AddToScheme(s)
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringAcceptor{}, &v1alpha1.PeeringAcceptorList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			// Create test consul server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient
			testClient.TestServer.WaitForActiveCARoot(t)

			if tt.initialConsulPeerName != "" {
				// Add the initial peerings into Consul by calling the Generate token endpoint.
				_, _, err := consulClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: tt.initialConsulPeerName}, nil)
				require.NoError(t, err)
			}

			// Create the peering acceptor controller
			controller := &AcceptorController{
				Client:                   fakeClient,
				ExposeServersServiceName: "test-expose-servers",
				ReleaseNamespace:         "default",
				Log:                      logrtest.New(t),
				ConsulClientConfig:       testClient.Cfg,
				ConsulServerConnMgr:      testClient.Watcher,
				Scheme:                   s,
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
			require.Contains(t, createdSecret.Labels, constants.LabelPeeringToken)
			require.Equal(t, createdSecret.Labels[constants.LabelPeeringToken], "true")
			// This assertion needs to be on StringData rather than Data because in the fake K8s client the contents are
			// stored in StringData if that's how the secret was initialized in the fake client. In a real cluster, this
			// StringData is an input only field, and shouldn't be read from.
			// Before failing at this case, the controller will error at reconcile with "secrets <SECRET> already
			// exists". Leaving this here documents that the entire contents of an existing secret should
			// be replaced.
			require.Equal(t, "", createdSecret.StringData["some-old-key"])
			decodedTokenData, err := base64.StdEncoding.DecodeString(string(createdSecret.Data["data"]))
			require.NoError(t, err)

			require.Contains(t, string(decodedTokenData), "\"CA\":")
			require.Contains(t, string(decodedTokenData), "\"ServerAddresses\"")
			require.Contains(t, string(decodedTokenData), "\"ServerName\":\"server.dc1.peering.11111111-2222-3333-4444-555555555555.consul\"")

			// Get the reconciled PeeringAcceptor and make assertions on the status
			acceptor := &v1alpha1.PeeringAcceptor{}
			err = fakeClient.Get(context.Background(), namespacedName, acceptor)
			require.NoError(t, err)
			require.Contains(t, acceptor.Finalizers, finalizerName)
			if tt.expectedStatus != nil {
				require.Equal(t, tt.expectedStatus.SecretRef.Name, acceptor.SecretRef().Name)
				require.Equal(t, tt.expectedStatus.SecretRef.Key, acceptor.SecretRef().Key)
				require.Equal(t, tt.expectedStatus.SecretRef.Backend, acceptor.SecretRef().Backend)
				require.Equal(t, tt.expectedStatus.LatestPeeringVersion, acceptor.Status.LatestPeeringVersion)
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

// TestReconcile_DeletePeeringAcceptor reconciles a PeeringAcceptor resource that is no longer in Kubernetes, but still
// exists in Consul.
func TestReconcile_DeletePeeringAcceptor(t *testing.T) {
	// Add the default namespace.
	ns := corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
	acceptor := &v1alpha1.PeeringAcceptor{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "acceptor-deleted",
			Namespace:         "default",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{finalizerName},
		},
		Spec: v1alpha1.PeeringAcceptorSpec{
			Peer: &v1alpha1.Peer{
				Secret: &v1alpha1.Secret{
					Name:    "acceptor-deleted-secret",
					Key:     "data",
					Backend: "kubernetes",
				},
			},
		},
	}
	k8sObjects := []runtime.Object{&ns, acceptor}

	// Add peering types to the scheme.
	s := runtime.NewScheme()
	corev1.AddToScheme(s)
	s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringAcceptor{}, &v1alpha1.PeeringAcceptorList{})
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

	// Create test consulServer server
	// Create test consul server.
	testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
	consulClient := testClient.APIClient
	testClient.TestServer.WaitForActiveCARoot(t)

	// Add the initial peerings into Consul by calling the Generate token endpoint.
	_, _, err := consulClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: "acceptor-deleted"}, nil)
	require.NoError(t, err)

	// Create the peering acceptor controller.
	controller := &AcceptorController{
		Client:              fakeClient,
		Log:                 logrtest.New(t),
		ConsulClientConfig:  testClient.Cfg,
		ConsulServerConnMgr: testClient.Watcher,
		Scheme:              s,
	}
	namespacedName := types.NamespacedName{
		Name:      "acceptor-deleted",
		Namespace: "default",
	}

	// Reconcile a resource that is not in K8s, but is still in Consul.
	resp, err := controller.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: namespacedName,
	})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	// After reconciliation, Consul should not have the peering.
	timer := &retry.Timer{Timeout: 5 * time.Second, Wait: 500 * time.Millisecond}
	retry.RunWith(timer, t, func(r *retry.R) {
		peering, _, err := consulClient.Peerings().Read(context.Background(), "acceptor-deleted", nil)
		require.Nil(r, peering)
		require.NoError(r, err)
	})

	err = fakeClient.Get(context.Background(), namespacedName, acceptor)
	require.EqualError(t, err, `peeringacceptors.consul.hashicorp.com "acceptor-deleted" not found`)

	oldSecret := &corev1.Secret{}
	err = fakeClient.Get(context.Background(), namespacedName, oldSecret)
	require.EqualError(t, err, `secrets "acceptor-deleted" not found`)
}

// TestReconcile_AcceptorVersionAnnotation tests the behavior of Reconcile for various
// scenarios involving the user setting the version annotation.
func TestReconcile_VersionAnnotation(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		annotations    map[string]string
		expErr         string
		expectedStatus *v1alpha1.PeeringAcceptorStatus
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
			expectedStatus: &v1alpha1.PeeringAcceptorStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "acceptor-created-secret",
						Key:     "data",
						Backend: "kubernetes",
					},
					ResourceVersion: "some-old-sha",
				},
				LatestPeeringVersion: pointer.Uint64(3),
			},
		},
		"is no/op if annotation value is equal to value in status": {
			annotations: map[string]string{
				constants.AnnotationPeeringVersion: "3",
			},
			expectedStatus: &v1alpha1.PeeringAcceptorStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "acceptor-created-secret",
						Key:     "data",
						Backend: "kubernetes",
					},
					ResourceVersion: "some-old-sha",
				},
				LatestPeeringVersion: pointer.Uint64(3),
			},
		},
		"updates if annotation value is greater than value in status": {
			annotations: map[string]string{
				constants.AnnotationPeeringVersion: "4",
			},
			expectedStatus: &v1alpha1.PeeringAcceptorStatus{
				SecretRef: &v1alpha1.SecretRefStatus{
					Secret: v1alpha1.Secret{
						Name:    "acceptor-created-secret",
						Key:     "data",
						Backend: "kubernetes",
					},
				},
				LatestPeeringVersion: pointer.Uint64(4),
			},
		},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}
			acceptor := &v1alpha1.PeeringAcceptor{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "acceptor-created",
					Namespace:   "default",
					Annotations: tt.annotations,
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
							Key:     "data",
							Backend: "kubernetes",
						},
						ResourceVersion: "some-old-sha",
					},
					LatestPeeringVersion: pointer.Uint64(3),
				},
			}
			secret := createSecret("acceptor-created-secret", "default", "data", "some-data")
			// Create fake k8s client
			k8sObjects := []runtime.Object{acceptor, secret, ns}

			s := runtime.NewScheme()
			corev1.AddToScheme(s)
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringAcceptor{}, &v1alpha1.PeeringAcceptorList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()

			// Create test consul server.
			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			consulClient := testClient.APIClient
			testClient.TestServer.WaitForActiveCARoot(t)

			_, _, err := consulClient.Peerings().GenerateToken(context.Background(), api.PeeringGenerateTokenRequest{PeerName: "acceptor-created"}, nil)
			require.NoError(t, err)

			// Create the peering acceptor controller
			controller := &AcceptorController{
				Client:              fakeClient,
				Log:                 logrtest.New(t),
				ConsulClientConfig:  testClient.Cfg,
				ConsulServerConnMgr: testClient.Watcher,
				Scheme:              s,
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

			// Get the reconciled PeeringAcceptor and make assertions on the status
			acceptor = &v1alpha1.PeeringAcceptor{}
			err = fakeClient.Get(context.Background(), namespacedName, acceptor)
			require.NoError(t, err)
			require.Contains(t, acceptor.Finalizers, finalizerName)
			if tt.expectedStatus != nil {
				require.Equal(t, tt.expectedStatus.SecretRef.Name, acceptor.SecretRef().Name)
				require.Equal(t, tt.expectedStatus.SecretRef.Key, acceptor.SecretRef().Key)
				require.Equal(t, tt.expectedStatus.SecretRef.Backend, acceptor.SecretRef().Backend)
				require.Equal(t, tt.expectedStatus.LatestPeeringVersion, acceptor.Status.LatestPeeringVersion)
			}
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
				},
				Conditions: v1alpha1.Conditions{
					v1alpha1.Condition{
						Type:   v1alpha1.ConditionSynced,
						Status: corev1.ConditionTrue,
					},
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
				},
				Conditions: v1alpha1.Conditions{
					v1alpha1.Condition{
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
			k8sObjects = append(k8sObjects, tt.peeringAcceptor)

			// Add peering types to the scheme.
			s := runtime.NewScheme()
			corev1.AddToScheme(s)
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringAcceptor{}, &v1alpha1.PeeringAcceptorList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()
			// Create the peering acceptor controller.
			pac := &AcceptorController{
				Client: fakeClient,
				Log:    logrtest.New(t),
				Scheme: s,
			}

			err := pac.updateStatus(context.Background(), types.NamespacedName{Name: tt.peeringAcceptor.Name, Namespace: tt.peeringAcceptor.Namespace})
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
			require.Equal(t, tt.expStatus.Conditions[0].Message, acceptor.Status.Conditions[0].Message)
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
				Conditions: v1alpha1.Conditions{
					v1alpha1.Condition{
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
					Conditions: v1alpha1.Conditions{
						{
							Type:   v1alpha1.ConditionSynced,
							Status: corev1.ConditionTrue,
						},
					},
				},
			},
			reconcileErr: errors.New("this is an error"),
			expStatus: v1alpha1.PeeringAcceptorStatus{
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
			k8sObjects = append(k8sObjects, tt.acceptor)

			// Add peering types to the scheme.
			s := runtime.NewScheme()
			corev1.AddToScheme(s)
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringAcceptor{}, &v1alpha1.PeeringAcceptorList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(k8sObjects...).Build()
			// Create the peering acceptor controller.
			controller := &AcceptorController{
				Client: fakeClient,
				Log:    logrtest.New(t),
				Scheme: s,
			}

			controller.updateStatusError(context.Background(), tt.acceptor, internalError, tt.reconcileErr)

			acceptor := &v1alpha1.PeeringAcceptor{}
			acceptorName := types.NamespacedName{
				Name:      "acceptor",
				Namespace: "default",
			}
			err := fakeClient.Get(context.Background(), acceptorName, acceptor)
			require.NoError(t, err)
			require.Equal(t, tt.expStatus.Conditions[0].Message, acceptor.Status.Conditions[0].Message)

		})
	}
}

func TestAcceptor_FilterPeeringAcceptor(t *testing.T) {
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
			controller := AcceptorController{}
			result := controller.filterPeeringAcceptors(tt.secret)
			require.Equal(t, tt.result, result)
		})
	}
}

func TestAcceptor_RequestsForPeeringTokens(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		secret    *corev1.Secret
		acceptors v1alpha1.PeeringAcceptorList
		result    []reconcile.Request
	}{
		"secret matches existing acceptor": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			acceptors: v1alpha1.PeeringAcceptorList{
				Items: []v1alpha1.PeeringAcceptor{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering",
							Namespace: "test",
						},
						Status: v1alpha1.PeeringAcceptorStatus{
							SecretRef: &v1alpha1.SecretRefStatus{
								Secret: v1alpha1.Secret{
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
			acceptors: v1alpha1.PeeringAcceptorList{
				Items: []v1alpha1.PeeringAcceptor{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering",
							Namespace: "test",
						},
						Status: v1alpha1.PeeringAcceptorStatus{
							SecretRef: &v1alpha1.SecretRefStatus{
								Secret: v1alpha1.Secret{
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
		"only matches with the correct acceptor": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			acceptors: v1alpha1.PeeringAcceptorList{
				Items: []v1alpha1.PeeringAcceptor{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering-1",
							Namespace: "test",
						},
						Status: v1alpha1.PeeringAcceptorStatus{
							SecretRef: &v1alpha1.SecretRefStatus{
								Secret: v1alpha1.Secret{
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
						Status: v1alpha1.PeeringAcceptorStatus{
							SecretRef: &v1alpha1.SecretRefStatus{
								Secret: v1alpha1.Secret{
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
						Status: v1alpha1.PeeringAcceptorStatus{
							SecretRef: &v1alpha1.SecretRefStatus{
								Secret: v1alpha1.Secret{
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
		"can match with zero acceptors": {
			secret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: "test",
				},
			},
			acceptors: v1alpha1.PeeringAcceptorList{
				Items: []v1alpha1.PeeringAcceptor{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "peering-1",
							Namespace: "test",
						},
						Status: v1alpha1.PeeringAcceptorStatus{
							SecretRef: &v1alpha1.SecretRefStatus{
								Secret: v1alpha1.Secret{
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
						Status: v1alpha1.PeeringAcceptorStatus{
							SecretRef: &v1alpha1.SecretRefStatus{
								Secret: v1alpha1.Secret{
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
						Status: v1alpha1.PeeringAcceptorStatus{
							SecretRef: &v1alpha1.SecretRefStatus{
								Secret: v1alpha1.Secret{
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
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.PeeringAcceptor{}, &v1alpha1.PeeringAcceptorList{})
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(tt.secret, &tt.acceptors).Build()
			controller := AcceptorController{
				Client: fakeClient,
				Log:    logrtest.New(t),
			}
			result := controller.requestsForPeeringTokens(tt.secret)

			require.Equal(t, tt.result, result)
		})
	}
}
