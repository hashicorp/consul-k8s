package controllers

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/api/common"
	"github.com/hashicorp/consul-k8s/api/v1alpha1"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type testReconciler interface {
	Reconcile(req ctrl.Request) (ctrl.Result, error)
}

func TestConfigEntryControllers_createsConfigEntry(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		kubeKind            string
		consulKind          string
		configEntryResource common.ConfigEntryResource
		reconciler          func(client.Client, *capi.Client, logr.Logger) testReconciler
		compare             func(t *testing.T, consul capi.ConfigEntry)
	}{
		{
			kubeKind:   "ServiceDefaults",
			consulKind: capi.ServiceDefaults,
			configEntryResource: &v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "http",
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceDefaultsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				svcDefault, ok := consulEntry.(*capi.ServiceConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, "http", svcDefault.Protocol)
			},
		},
		{
			kubeKind:   "ServiceResolver",
			consulKind: capi.ServiceResolver,
			configEntryResource: &v1alpha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceResolverSpec{
					Redirect: &v1alpha1.ServiceResolverRedirect{
						Service: "redirect",
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceResolverController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				svcDefault, ok := consulEntry.(*capi.ServiceResolverConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, "redirect", svcDefault.Redirect.Service)
			},
		},
		{
			kubeKind:   "ProxyDefaults",
			consulKind: capi.ProxyDefaults,
			configEntryResource: &v1alpha1.ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.Global,
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ProxyDefaultsSpec{
					MeshGateway: v1alpha1.MeshGatewayConfig{
						Mode: "remote",
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ProxyDefaultsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				proxyDefault, ok := consulEntry.(*capi.ProxyConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, capi.MeshGatewayModeRemote, proxyDefault.MeshGateway.Mode)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResource)
			client := fake.NewFakeClientWithScheme(s, c.configEntryResource)

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			r := c.reconciler(client, consulClient, logrtest.TestLogger{T: t})
			namespacedName := types.NamespacedName{
				Namespace: kubeNS,
				Name:      c.configEntryResource.Name(),
			}
			resp, err := r.Reconcile(ctrl.Request{
				NamespacedName: namespacedName,
			})
			req.NoError(err)
			req.False(resp.Requeue)

			cfg, _, err := consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResource.Name(), nil)
			req.NoError(err)
			req.Equal(c.configEntryResource.Name(), cfg.GetName())
			c.compare(t, cfg)

			// Check that the status is "synced".
			err = client.Get(ctx, namespacedName, c.configEntryResource)
			req.NoError(err)
			req.Equal(corev1.ConditionTrue, c.configEntryResource.SyncedConditionStatus())

			// Check that the finalizer is added.
			req.Contains(c.configEntryResource.Finalizers(), FinalizerName)
		})
	}
}

func TestConfigEntryControllers_updatesConfigEntry(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		kubeKind            string
		consulKind          string
		configEntryResource common.ConfigEntryResource
		reconciler          func(client.Client, *capi.Client, logr.Logger) testReconciler
		updateF             func(common.ConfigEntryResource)
		compare             func(t *testing.T, consul capi.ConfigEntry)
	}{
		{
			kubeKind:   "ServiceDefaults",
			consulKind: capi.ServiceDefaults,
			configEntryResource: &v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "http",
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceDefaultsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				svcDefaults := resource.(*v1alpha1.ServiceDefaults)
				svcDefaults.Spec.Protocol = "tcp"
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				svcDefault, ok := consulEntry.(*capi.ServiceConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, "tcp", svcDefault.Protocol)
			},
		},
		{
			kubeKind:   "ServiceResolver",
			consulKind: capi.ServiceResolver,
			configEntryResource: &v1alpha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceResolverSpec{
					Redirect: &v1alpha1.ServiceResolverRedirect{
						Service: "redirect",
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceResolverController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				svcResolver := resource.(*v1alpha1.ServiceResolver)
				svcResolver.Spec.Redirect.Service = "different_redirect"
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				svcDefault, ok := consulEntry.(*capi.ServiceResolverConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, "different_redirect", svcDefault.Redirect.Service)
			},
		},
		{
			kubeKind:   "ProxyDefaults",
			consulKind: capi.ProxyDefaults,
			configEntryResource: &v1alpha1.ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.Global,
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ProxyDefaultsSpec{
					MeshGateway: v1alpha1.MeshGatewayConfig{
						Mode: "remote",
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ProxyDefaultsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				proxyDefault := resource.(*v1alpha1.ProxyDefaults)
				proxyDefault.Spec.MeshGateway.Mode = "local"
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				proxyDefault, ok := consulEntry.(*capi.ProxyConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, capi.MeshGatewayModeLocal, proxyDefault.MeshGateway.Mode)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResource)
			client := fake.NewFakeClientWithScheme(s, c.configEntryResource)

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			// We haven't run reconcile yet so we must create the config entry
			// in Consul ourselves.
			{
				written, _, err := consulClient.ConfigEntries().Set(c.configEntryResource.ToConsul(), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile which should update the entry in Consul.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      c.configEntryResource.Name(),
				}
				// First get it so we have the latest revision number.
				err = client.Get(ctx, namespacedName, c.configEntryResource)
				req.NoError(err)

				// Update the entry in Kube and run reconcile.
				c.updateF(c.configEntryResource)
				err := client.Update(ctx, c.configEntryResource)
				req.NoError(err)
				r := c.reconciler(client, consulClient, logrtest.TestLogger{T: t})
				resp, err := r.Reconcile(ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.NoError(err)
				req.False(resp.Requeue)

				// Now check that the object in Consul is as expected.
				cfg, _, err := consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResource.Name(), nil)
				req.NoError(err)
				req.Equal(c.configEntryResource.Name(), cfg.GetName())
				c.compare(t, cfg)
			}
		})
	}
}

func TestConfigEntryControllers_deletesConfigEntry(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		kubeKind                        string
		consulKind                      string
		configEntryResourceWithDeletion common.ConfigEntryResource
		reconciler                      func(client.Client, *capi.Client, logr.Logger) testReconciler
	}{
		{
			kubeKind:   "ServiceDefaults",
			consulKind: capi.ServiceDefaults,
			configEntryResourceWithDeletion: &v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "http",
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceDefaultsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
		},
		{
			kubeKind:   "ServiceResolver",
			consulKind: capi.ServiceResolver,
			configEntryResourceWithDeletion: &v1alpha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.ServiceResolverSpec{
					Redirect: &v1alpha1.ServiceResolverRedirect{
						Service: "redirect",
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceResolverController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
		},
		{
			kubeKind:   "ProxyDefaults",
			consulKind: capi.ProxyDefaults,
			configEntryResourceWithDeletion: &v1alpha1.ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:              common.Global,
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.ProxyDefaultsSpec{
					MeshGateway: v1alpha1.MeshGatewayConfig{
						Mode: "remote",
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ProxyDefaultsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResourceWithDeletion)
			client := fake.NewFakeClientWithScheme(s, c.configEntryResourceWithDeletion)

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			// We haven't run reconcile yet so we must create the config entry
			// in Consul ourselves.
			{
				written, _, err := consulClient.ConfigEntries().Set(c.configEntryResourceWithDeletion.ToConsul(), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile. It's marked for deletion so this should delete it.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      c.configEntryResourceWithDeletion.Name(),
				}
				r := c.reconciler(client, consulClient, logrtest.TestLogger{T: t})
				resp, err := r.Reconcile(ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.NoError(err)
				req.False(resp.Requeue)

				_, _, err = consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResourceWithDeletion.Name(), nil)
				req.EqualError(err,
					fmt.Sprintf("Unexpected response code: 404 (Config entry not found for %q / %q)",
						c.consulKind, c.configEntryResourceWithDeletion.Name()))
			}
		})
	}
}

func TestConfigEntryControllers_errorUpdatesSyncStatus(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		kubeKind            string
		consulKind          string
		configEntryResource common.ConfigEntryResource
		reconciler          func(client.Client, *capi.Client, logr.Logger) testReconciler
	}{
		{
			kubeKind:   "ServiceDefaults",
			consulKind: capi.ServiceDefaults,
			configEntryResource: &v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "http",
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceDefaultsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
		},
		{
			kubeKind:   "ServiceResolver",
			consulKind: capi.ServiceResolver,
			configEntryResource: &v1alpha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceResolverSpec{
					Redirect: &v1alpha1.ServiceResolverRedirect{
						Service: "redirect",
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceResolverController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
		},
		{
			kubeKind:   "ProxyDefaults",
			consulKind: capi.ProxyDefaults,
			configEntryResource: &v1alpha1.ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.Global,
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ProxyDefaultsSpec{
					MeshGateway: v1alpha1.MeshGatewayConfig{
						Mode: "remote",
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ProxyDefaultsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient: consulClient,
					},
				}
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResource)
			client := fake.NewFakeClientWithScheme(s, c.configEntryResource)

			// Construct a Consul client that will error by giving it
			// an unresolvable address.
			consulClient, err := capi.NewClient(&capi.Config{
				Address: "incorrect-address",
			})
			req.NoError(err)

			// ReconcileEntry should result in an error.
			r := c.reconciler(client, consulClient, logrtest.TestLogger{T: t})
			namespacedName := types.NamespacedName{
				Namespace: kubeNS,
				Name:      c.configEntryResource.Name(),
			}
			resp, err := r.Reconcile(ctrl.Request{
				NamespacedName: namespacedName,
			})
			req.Error(err)

			expErr := fmt.Sprintf("Get \"http://incorrect-address/v1/config/%s/%s\": dial tcp: lookup incorrect-address", c.consulKind, c.configEntryResource.Name())
			req.Contains(err.Error(), expErr)
			req.False(resp.Requeue)

			// Check that the status is "synced=false".
			err = client.Get(ctx, namespacedName, c.configEntryResource)
			req.NoError(err)
			status, reason, errMsg := c.configEntryResource.SyncedCondition()
			req.Equal(corev1.ConditionFalse, status)
			req.Equal("ConsulAgentError", reason)
			req.Contains(errMsg, expErr)
		})
	}
}
