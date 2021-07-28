package controller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-logr/logr"
	logrtest "github.com/go-logr/logr/testing"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
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

const datacenterName = "datacenter"

type testReconciler interface {
	Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error)
}

func TestConfigEntryControllers_createsConfigEntry(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		kubeKind            string
		consulKind          string
		consulPrereqs       []capi.ConfigEntry
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
					MeshGateway: v1alpha1.MeshGateway{
						Mode: "remote",
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ProxyDefaultsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				proxyDefault, ok := consulEntry.(*capi.ProxyConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, capi.MeshGatewayModeRemote, proxyDefault.MeshGateway.Mode)
			},
		},
		{
			kubeKind:   "Mesh",
			consulKind: capi.MeshConfig,
			configEntryResource: &v1alpha1.Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.Mesh,
					Namespace: kubeNS,
				},
				Spec: v1alpha1.MeshSpec{
					TransparentProxy: v1alpha1.TransparentProxyMeshConfig{
						MeshDestinationsOnly: true,
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &MeshController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				mesh, ok := consulEntry.(*capi.MeshConfigEntry)
				require.True(t, ok, "cast error")
				require.True(t, mesh.TransparentProxy.MeshDestinationsOnly)
			},
		},
		{
			kubeKind:   "ServiceRouter",
			consulKind: capi.ServiceRouter,
			consulPrereqs: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
			},
			configEntryResource: &v1alpha1.ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceRouterSpec{
					Routes: []v1alpha1.ServiceRoute{
						{
							Match: &v1alpha1.ServiceRouteMatch{
								HTTP: &v1alpha1.ServiceRouteHTTPMatch{
									PathPrefix: "/admin",
								},
							},
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceRouterController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				configEntry, ok := consulEntry.(*capi.ServiceRouterConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, "/admin", configEntry.Routes[0].Match.HTTP.PathPrefix)
			},
		},
		{
			kubeKind:   "ServiceSplitter",
			consulKind: capi.ServiceSplitter,
			consulPrereqs: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
			},
			configEntryResource: &v1alpha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceSplitterSpec{
					Splits: []v1alpha1.ServiceSplit{
						{
							Weight: 100,
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceSplitterController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				svcDefault, ok := consulEntry.(*capi.ServiceSplitterConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, float32(100), svcDefault.Splits[0].Weight)
			},
		},
		{
			kubeKind:   "ServiceIntentions",
			consulKind: capi.ServiceIntentions,
			consulPrereqs: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "bar",
					Protocol: "http",
				},
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "baz",
					Protocol: "http",
				},
			},
			configEntryResource: &v1alpha1.ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-name",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceIntentionsSpec{
					Destination: v1alpha1.Destination{
						Name: "foo",
					},
					Sources: v1alpha1.SourceIntentions{
						&v1alpha1.SourceIntention{
							Name:   "bar",
							Action: "allow",
						},
						&v1alpha1.SourceIntention{
							Name:   "baz",
							Action: "deny",
						},
						&v1alpha1.SourceIntention{
							Name: "bax",
							Permissions: v1alpha1.IntentionPermissions{
								&v1alpha1.IntentionPermission{
									Action: "allow",
									HTTP: &v1alpha1.IntentionHTTPPermission{
										PathExact: "/path",
										Header: v1alpha1.IntentionHTTPHeaderPermissions{
											v1alpha1.IntentionHTTPHeaderPermission{
												Name:    "auth",
												Present: true,
											},
										},
										Methods: []string{
											"PUT",
											"GET",
										},
									},
								},
							},
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceIntentionsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				svcIntentions, ok := consulEntry.(*capi.ServiceIntentionsConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, "foo", svcIntentions.Name)
				require.Equal(t, "bar", svcIntentions.Sources[0].Name)
				require.Equal(t, capi.IntentionActionAllow, svcIntentions.Sources[0].Action)
				require.Equal(t, "baz", svcIntentions.Sources[1].Name)
				require.Equal(t, capi.IntentionActionDeny, svcIntentions.Sources[1].Action)
				require.Equal(t, "bax", svcIntentions.Sources[2].Name)
				require.Equal(t, capi.IntentionActionAllow, svcIntentions.Sources[2].Permissions[0].Action)
				require.Equal(t, "/path", svcIntentions.Sources[2].Permissions[0].HTTP.PathExact)
			},
		},
		{
			kubeKind:   "IngressGateway",
			consulKind: capi.IngressGateway,
			configEntryResource: &v1alpha1.IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.IngressGatewaySpec{
					TLS: v1alpha1.GatewayTLSConfig{
						Enabled: true,
					},
					Listeners: []v1alpha1.IngressListener{
						{
							Port:     80,
							Protocol: "http",
							Services: []v1alpha1.IngressService{
								{
									Name: "*",
								},
							},
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &IngressGatewayController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				resource, ok := consulEntry.(*capi.IngressGatewayConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, true, resource.TLS.Enabled)
				require.Equal(t, 80, resource.Listeners[0].Port)
				require.Equal(t, "http", resource.Listeners[0].Protocol)
				require.Equal(t, "*", resource.Listeners[0].Services[0].Name)
			},
		},
		{
			kubeKind:   "TerminatingGateway",
			consulKind: capi.TerminatingGateway,
			configEntryResource: &v1alpha1.TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.TerminatingGatewaySpec{
					Services: []v1alpha1.LinkedService{
						{
							Name:     "name",
							CAFile:   "caFile",
							CertFile: "certFile",
							KeyFile:  "keyFile",
							SNI:      "sni",
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &TerminatingGatewayController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				resource, ok := consulEntry.(*capi.TerminatingGatewayConfigEntry)
				require.True(t, ok, "cast error")
				require.Len(t, resource.Services, 1)
				require.Equal(t, "name", resource.Services[0].Name)
				require.Equal(t, "caFile", resource.Services[0].CAFile)
				require.Equal(t, "certFile", resource.Services[0].CertFile)
				require.Equal(t, "keyFile", resource.Services[0].KeyFile)
				require.Equal(t, "sni", resource.Services[0].SNI)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResource)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.configEntryResource).Build()

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()

			consul.WaitForServiceIntentions(t)
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)
			for _, configEntry := range c.consulPrereqs {
				written, _, err := consulClient.ConfigEntries().Set(configEntry, nil)
				req.NoError(err)
				req.True(written)
			}

			r := c.reconciler(fakeClient, consulClient, logrtest.TestLogger{T: t})
			namespacedName := types.NamespacedName{
				Namespace: kubeNS,
				Name:      c.configEntryResource.KubernetesName(),
			}
			resp, err := r.Reconcile(ctx, ctrl.Request{
				NamespacedName: namespacedName,
			})
			req.NoError(err)
			req.False(resp.Requeue)

			cfg, _, err := consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResource.ConsulName(), nil)
			req.NoError(err)
			req.Equal(c.configEntryResource.ConsulName(), cfg.GetName())
			c.compare(t, cfg)

			// Check that the status is "synced".
			err = fakeClient.Get(ctx, namespacedName, c.configEntryResource)
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
		consulPrereqs       []capi.ConfigEntry
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
					MeshGateway: v1alpha1.MeshGateway{
						Mode: "remote",
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ProxyDefaultsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
		{
			kubeKind:   "Mesh",
			consulKind: capi.MeshConfig,
			configEntryResource: &v1alpha1.Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.Mesh,
					Namespace: kubeNS,
				},
				Spec: v1alpha1.MeshSpec{
					TransparentProxy: v1alpha1.TransparentProxyMeshConfig{
						MeshDestinationsOnly: true,
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &MeshController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				mesh := resource.(*v1alpha1.Mesh)
				mesh.Spec.TransparentProxy.MeshDestinationsOnly = false
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				meshConfigEntry, ok := consulEntry.(*capi.MeshConfigEntry)
				require.True(t, ok, "cast error")
				require.False(t, meshConfigEntry.TransparentProxy.MeshDestinationsOnly)
			},
		},
		{
			kubeKind:   "ServiceSplitter",
			consulKind: capi.ServiceSplitter,
			consulPrereqs: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "bar",
					Protocol: "http",
				},
			},
			configEntryResource: &v1alpha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceSplitterSpec{
					Splits: []v1alpha1.ServiceSplit{
						{
							Weight: 100,
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceSplitterController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				serviceSplitter := resource.(*v1alpha1.ServiceSplitter)
				serviceSplitter.Spec.Splits = []v1alpha1.ServiceSplit{
					{
						Weight: 80,
					},
					{
						Weight:  20,
						Service: "bar",
					},
				}
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				svcSplitter, ok := consulEntry.(*capi.ServiceSplitterConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, float32(80), svcSplitter.Splits[0].Weight)
				require.Equal(t, float32(20), svcSplitter.Splits[1].Weight)
				require.Equal(t, "bar", svcSplitter.Splits[1].Service)
			},
		},
		{
			kubeKind:   "ServiceRouter",
			consulKind: capi.ServiceRouter,
			consulPrereqs: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
			},
			configEntryResource: &v1alpha1.ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceRouterSpec{
					Routes: []v1alpha1.ServiceRoute{
						{
							Match: &v1alpha1.ServiceRouteMatch{
								HTTP: &v1alpha1.ServiceRouteHTTPMatch{
									PathPrefix: "/admin",
								},
							},
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceRouterController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				svcRouter := resource.(*v1alpha1.ServiceRouter)
				svcRouter.Spec.Routes[0].Match.HTTP.PathPrefix = "/different_path"
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				configEntry, ok := consulEntry.(*capi.ServiceRouterConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, "/different_path", configEntry.Routes[0].Match.HTTP.PathPrefix)
			},
		},
		{
			kubeKind:   "ServiceIntentions",
			consulKind: capi.ServiceIntentions,
			consulPrereqs: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "bar",
					Protocol: "http",
				},
			},
			configEntryResource: &v1alpha1.ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-name",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceIntentionsSpec{
					Destination: v1alpha1.Destination{
						Name: "foo",
					},
					Sources: v1alpha1.SourceIntentions{
						&v1alpha1.SourceIntention{
							Name:   "bar",
							Action: "allow",
						},
						&v1alpha1.SourceIntention{
							Name: "baz",
							Permissions: v1alpha1.IntentionPermissions{
								&v1alpha1.IntentionPermission{
									Action: "allow",
									HTTP: &v1alpha1.IntentionHTTPPermission{
										PathExact: "/path",
										Header: v1alpha1.IntentionHTTPHeaderPermissions{
											v1alpha1.IntentionHTTPHeaderPermission{
												Name:    "auth",
												Present: true,
											},
										},
										Methods: []string{
											"PUT",
											"GET",
										},
									},
								},
							},
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceIntentionsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				svcIntentions := resource.(*v1alpha1.ServiceIntentions)
				svcIntentions.Spec.Sources[0].Action = "deny"
				svcIntentions.Spec.Sources[1].Permissions[0].Action = "deny"
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				configEntry, ok := consulEntry.(*capi.ServiceIntentionsConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, capi.IntentionActionDeny, configEntry.Sources[0].Action)
				require.Equal(t, capi.IntentionActionDeny, configEntry.Sources[1].Permissions[0].Action)
			},
		},
		{
			kubeKind:   "IngressGateway",
			consulKind: capi.IngressGateway,
			configEntryResource: &v1alpha1.IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.IngressGatewaySpec{
					TLS: v1alpha1.GatewayTLSConfig{
						Enabled: true,
					},
					Listeners: []v1alpha1.IngressListener{
						{
							Port:     80,
							Protocol: "http",
							Services: []v1alpha1.IngressService{
								{
									Name: "*",
								},
							},
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &IngressGatewayController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				igw := resource.(*v1alpha1.IngressGateway)
				igw.Spec.TLS.Enabled = false
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				resource, ok := consulEntry.(*capi.IngressGatewayConfigEntry)
				require.True(t, ok, "cast error")
				require.Equal(t, false, resource.TLS.Enabled)
				require.Equal(t, 80, resource.Listeners[0].Port)
				require.Equal(t, "http", resource.Listeners[0].Protocol)
				require.Equal(t, "*", resource.Listeners[0].Services[0].Name)
			},
		},
		{
			kubeKind:   "TerminatingGateway",
			consulKind: capi.TerminatingGateway,
			configEntryResource: &v1alpha1.TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.TerminatingGatewaySpec{
					Services: []v1alpha1.LinkedService{
						{
							Name:     "name",
							CAFile:   "caFile",
							CertFile: "certFile",
							KeyFile:  "keyFile",
							SNI:      "sni",
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &TerminatingGatewayController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			updateF: func(resource common.ConfigEntryResource) {
				igw := resource.(*v1alpha1.TerminatingGateway)
				igw.Spec.Services[0].SNI = "new-sni"
			},
			compare: func(t *testing.T, consulEntry capi.ConfigEntry) {
				resource, ok := consulEntry.(*capi.TerminatingGatewayConfigEntry)
				require.True(t, ok, "cast error")
				require.Len(t, resource.Services, 1)
				require.Equal(t, "name", resource.Services[0].Name)
				require.Equal(t, "caFile", resource.Services[0].CAFile)
				require.Equal(t, "certFile", resource.Services[0].CertFile)
				require.Equal(t, "keyFile", resource.Services[0].KeyFile)
				require.Equal(t, "new-sni", resource.Services[0].SNI)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResource)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.configEntryResource).Build()

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()

			consul.WaitForServiceIntentions(t)
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			// Create any prereqs.
			for _, configEntry := range c.consulPrereqs {
				written, _, err := consulClient.ConfigEntries().Set(configEntry, nil)
				req.NoError(err)
				req.True(written)
			}

			// We haven't run reconcile yet so we must create the config entry
			// in Consul ourselves.
			{
				written, _, err := consulClient.ConfigEntries().Set(c.configEntryResource.ToConsul(datacenterName), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile which should update the entry in Consul.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      c.configEntryResource.KubernetesName(),
				}
				// First get it so we have the latest revision number.
				err = fakeClient.Get(ctx, namespacedName, c.configEntryResource)
				req.NoError(err)

				// Update the entry in Kube and run reconcile.
				c.updateF(c.configEntryResource)
				err := fakeClient.Update(ctx, c.configEntryResource)
				req.NoError(err)
				r := c.reconciler(fakeClient, consulClient, logrtest.TestLogger{T: t})
				resp, err := r.Reconcile(ctx, ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.NoError(err)
				req.False(resp.Requeue)

				// Now check that the object in Consul is as expected.
				cfg, _, err := consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResource.ConsulName(), nil)
				req.NoError(err)
				req.Equal(c.configEntryResource.ConsulName(), cfg.GetName())
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
		consulPrereq                    []capi.ConfigEntry
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
					MeshGateway: v1alpha1.MeshGateway{
						Mode: "remote",
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ProxyDefaultsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
		},
		{
			kubeKind:   "Mesh",
			consulKind: capi.MeshConfig,
			configEntryResourceWithDeletion: &v1alpha1.Mesh{
				ObjectMeta: metav1.ObjectMeta{
					Name:              common.Global,
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.MeshSpec{
					TransparentProxy: v1alpha1.TransparentProxyMeshConfig{
						MeshDestinationsOnly: true,
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &MeshController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
		},
		{
			kubeKind:   "ServiceRouter",
			consulKind: capi.ServiceRouter,
			consulPrereq: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
			},
			configEntryResourceWithDeletion: &v1alpha1.ServiceRouter{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.ServiceRouterSpec{
					Routes: []v1alpha1.ServiceRoute{
						{
							Match: &v1alpha1.ServiceRouteMatch{
								HTTP: &v1alpha1.ServiceRouteHTTPMatch{
									PathPrefix: "/admin",
								},
							},
						},
					},
				},
			},

			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceRouterController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
		},
		{
			kubeKind:   "ServiceSplitter",
			consulKind: capi.ServiceSplitter,
			consulPrereq: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
			},
			configEntryResourceWithDeletion: &v1alpha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.ServiceSplitterSpec{
					Splits: []v1alpha1.ServiceSplit{
						{
							Weight: 100,
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceSplitterController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
		},
		{
			kubeKind:   "ServiceIntentions",
			consulKind: capi.ServiceIntentions,
			consulPrereq: []capi.ConfigEntry{
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				},
				&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "bar",
					Protocol: "http",
				},
			},
			configEntryResourceWithDeletion: &v1alpha1.ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test-name",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.ServiceIntentionsSpec{
					Destination: v1alpha1.Destination{
						Name: "foo",
					},
					Sources: v1alpha1.SourceIntentions{
						&v1alpha1.SourceIntention{
							Name:   "bar",
							Action: "allow",
						},
						&v1alpha1.SourceIntention{
							Name: "baz",
							Permissions: v1alpha1.IntentionPermissions{
								&v1alpha1.IntentionPermission{
									Action: "allow",
									HTTP: &v1alpha1.IntentionHTTPPermission{
										PathExact: "/path",
										Header: v1alpha1.IntentionHTTPHeaderPermissions{
											v1alpha1.IntentionHTTPHeaderPermission{
												Name:    "auth",
												Present: true,
											},
										},
										Methods: []string{
											"PUT",
											"GET",
										},
									},
								},
							},
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &ServiceIntentionsController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
		},
		{
			kubeKind:   "IngressGateway",
			consulKind: capi.IngressGateway,
			configEntryResourceWithDeletion: &v1alpha1.IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.IngressGatewaySpec{
					TLS: v1alpha1.GatewayTLSConfig{
						Enabled: true,
					},
					Listeners: []v1alpha1.IngressListener{
						{
							Port:     80,
							Protocol: "http",
							Services: []v1alpha1.IngressService{
								{
									Name: "*",
								},
							},
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &IngressGatewayController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
		},
		{
			kubeKind:   "TerminatingGateway",
			consulKind: capi.TerminatingGateway,
			configEntryResourceWithDeletion: &v1alpha1.TerminatingGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.TerminatingGatewaySpec{
					Services: []v1alpha1.LinkedService{
						{
							Name:     "name",
							CAFile:   "caFile",
							CertFile: "certFile",
							KeyFile:  "keyFile",
							SNI:      "sni",
						},
					},
				},
			},
			reconciler: func(client client.Client, consulClient *capi.Client, logger logr.Logger) testReconciler {
				return &TerminatingGatewayController{
					Client: client,
					Log:    logger,
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(c.configEntryResourceWithDeletion).Build()

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()

			consul.WaitForServiceIntentions(t)
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			// Create any prereqs.
			for _, configEntry := range c.consulPrereq {
				written, _, err := consulClient.ConfigEntries().Set(configEntry, nil)
				req.NoError(err)
				req.True(written)
			}

			// We haven't run reconcile yet so we must create the config entry
			// in Consul ourselves.
			{
				written, _, err := consulClient.ConfigEntries().Set(c.configEntryResourceWithDeletion.ToConsul(datacenterName), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile. It's marked for deletion so this should delete it.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      c.configEntryResourceWithDeletion.KubernetesName(),
				}
				r := c.reconciler(fakeClient, consulClient, logrtest.TestLogger{T: t})
				resp, err := r.Reconcile(context.Background(), ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.NoError(err)
				req.False(resp.Requeue)

				_, _, err = consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResourceWithDeletion.ConsulName(), nil)
				req.EqualError(err,
					fmt.Sprintf("Unexpected response code: 404 (Config entry not found for %q / %q)",
						c.consulKind, c.configEntryResourceWithDeletion.ConsulName()))
			}
		})
	}
}

func TestConfigEntryControllers_errorUpdatesSyncStatus(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	req := require.New(t)
	ctx := context.Background()
	svcDefaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: kubeNS,
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}

	s := runtime.NewScheme()
	s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(svcDefaults).Build()

	// Construct a Consul client that will error by giving it
	// an unresolvable address.
	consulClient, err := capi.NewClient(&capi.Config{
		Address: "incorrect-address",
	})
	req.NoError(err)
	reconciler := &ServiceDefaultsController{
		Client: fakeClient,
		Log:    logrtest.TestLogger{T: t},
		ConfigEntryController: &ConfigEntryController{
			ConsulClient:   consulClient,
			DatacenterName: datacenterName,
		},
	}

	// ReconcileEntry should result in an error.
	namespacedName := types.NamespacedName{
		Namespace: kubeNS,
		Name:      svcDefaults.KubernetesName(),
	}
	resp, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: namespacedName,
	})
	req.Error(err)

	expErr := fmt.Sprintf("Get \"http://incorrect-address/v1/config/%s/%s\": dial tcp: lookup incorrect-address", capi.ServiceDefaults, svcDefaults.ConsulName())
	req.Contains(err.Error(), expErr)
	req.False(resp.Requeue)

	// Check that the status is "synced=false".
	err = fakeClient.Get(ctx, namespacedName, svcDefaults)
	req.NoError(err)
	status, reason, errMsg := svcDefaults.SyncedCondition()
	req.Equal(corev1.ConditionFalse, status)
	req.Equal("ConsulAgentError", reason)
	req.Contains(errMsg, expErr)
}

// Test that if the config entry hasn't changed in Consul but our resource
// synced status isn't set to true then we update its status.
func TestConfigEntryControllers_setsSyncedToTrue(t *testing.T) {
	t.Parallel()
	kubeNS := "default"
	req := require.New(t)
	ctx := context.Background()
	s := runtime.NewScheme()
	svcDefaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "foo",
			Namespace: kubeNS,
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
		Status: v1alpha1.Status{
			Conditions: v1alpha1.Conditions{
				{
					Type:   v1alpha1.ConditionSynced,
					Status: corev1.ConditionUnknown,
				},
			},
		},
	}
	s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)

	// The config entry exists in kube but its status will be nil.
	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(svcDefaults).Build()

	consul, err := testutil.NewTestServerConfigT(t, nil)
	req.NoError(err)
	defer consul.Stop()

	consul.WaitForServiceIntentions(t)
	consulClient, err := capi.NewClient(&capi.Config{
		Address: consul.HTTPAddr,
	})
	req.NoError(err)
	reconciler := &ServiceDefaultsController{
		Client: fakeClient,
		Log:    logrtest.TestLogger{T: t},
		ConfigEntryController: &ConfigEntryController{
			ConsulClient:   consulClient,
			DatacenterName: datacenterName,
		},
	}

	// Create the resource in Consul to mimic that it was created
	// successfully (but its status hasn't been updated).
	_, _, err = consulClient.ConfigEntries().Set(svcDefaults.ToConsul(datacenterName), nil)
	require.NoError(t, err)

	namespacedName := types.NamespacedName{
		Namespace: kubeNS,
		Name:      svcDefaults.KubernetesName(),
	}
	resp, err := reconciler.Reconcile(ctx, ctrl.Request{
		NamespacedName: namespacedName,
	})
	req.NoError(err)
	req.False(resp.Requeue)

	// Check that the status is now "synced".
	err = fakeClient.Get(ctx, namespacedName, svcDefaults)
	req.NoError(err)
	req.Equal(corev1.ConditionTrue, svcDefaults.SyncedConditionStatus())
}

// Test that if the config entry exists in Consul but is not managed by the
// controller, creating/updating the resource fails
func TestConfigEntryControllers_doesNotCreateUnownedConfigEntry(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		datacenterAnnotation string
		expErr               string
	}{
		{
			datacenterAnnotation: "",
			expErr:               "config entry already exists in Consul",
		},
		{
			datacenterAnnotation: "other-datacenter",
			expErr:               "config entry managed in different datacenter: \"other-datacenter\"",
		},
	}

	for _, c := range cases {
		t.Run(fmt.Sprintf("datacenter: %q", c.datacenterAnnotation), func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			svcDefaults := &v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "http",
				},
			}
			s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(svcDefaults).Build()

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()

			consul.WaitForServiceIntentions(t)
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			// We haven't run reconcile yet. We must create the config entry
			// in Consul ourselves in a different datacenter.
			{
				written, _, err := consulClient.ConfigEntries().Set(svcDefaults.ToConsul(c.datacenterAnnotation), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile which should **not** update the entry in Consul.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      svcDefaults.KubernetesName(),
				}
				// First get it so we have the latest revision number.
				err = fakeClient.Get(ctx, namespacedName, svcDefaults)
				req.NoError(err)

				// Attempt to create the entry in Kube and run reconcile.
				reconciler := ServiceDefaultsController{
					Client: fakeClient,
					Log:    logrtest.TestLogger{T: t},
					ConfigEntryController: &ConfigEntryController{
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
				resp, err := reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.EqualError(err, c.expErr)
				req.False(resp.Requeue)

				// Now check that the object in Consul is as expected.
				cfg, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, svcDefaults.ConsulName(), nil)
				req.NoError(err)
				req.Equal(cfg.GetMeta()[common.DatacenterKey], c.datacenterAnnotation)

				// Check that the status is "synced=false".
				err = fakeClient.Get(ctx, namespacedName, svcDefaults)
				req.NoError(err)
				status, reason, errMsg := svcDefaults.SyncedCondition()
				req.Equal(corev1.ConditionFalse, status)
				req.Equal("ExternallyManagedConfigError", reason)
				req.Equal(errMsg, c.expErr)
			}
		})
	}
}

// Test that if the config entry exists in Consul but is not managed by the
// controller, deleting the resource does not delete the Consul config entry
func TestConfigEntryControllers_doesNotDeleteUnownedConfig(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	// Test against the metadata being empty or set. Both should not trigger
	// deletion in Consul.
	cases := []string{"", "other-datacenter"}
	for _, datacenter := range cases {
		t.Run(datacenter, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			svcDefaultsWithDeletion := &v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					Namespace:         kubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{FinalizerName},
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "http",
				},
			}
			s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaultsWithDeletion)
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(svcDefaultsWithDeletion).Build()

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()

			consul.WaitForServiceIntentions(t)
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)
			reconciler := &ServiceDefaultsController{
				Client: fakeClient,
				Log:    logrtest.TestLogger{T: t},
				ConfigEntryController: &ConfigEntryController{
					ConsulClient:   consulClient,
					DatacenterName: datacenterName,
				},
			}

			// We haven't run reconcile yet so we must create the config entry
			// in Consul ourselves.
			{
				// Create the resource with different datacenter on metadata
				written, _, err := consulClient.ConfigEntries().Set(svcDefaultsWithDeletion.ToConsul(datacenter), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile. It's marked for deletion so this should delete the kubernetes resource
			// but not the consul config entry.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      svcDefaultsWithDeletion.KubernetesName(),
				}
				resp, err := reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.NoError(err)
				req.False(resp.Requeue)

				entry, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, svcDefaultsWithDeletion.ConsulName(), nil)
				req.NoError(err)
				req.Equal(entry.GetMeta()[common.DatacenterKey], datacenter)

				// Check that the resource is deleted from cluster.
				svcDefault := &v1alpha1.ServiceDefaults{}
				_ = fakeClient.Get(ctx, namespacedName, svcDefault)
				require.Empty(t, svcDefault.Finalizers())
			}
		})
	}
}

func TestConfigEntryControllers_updatesStatusWhenDeleteFails(t *testing.T) {
	ctx := context.Background()
	kubeNS := "default"

	s := runtime.NewScheme()
	s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.ServiceDefaults{}, &v1alpha1.ServiceSplitter{})

	defaults := &v1alpha1.ServiceDefaults{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: "default",
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}

	splitter := &v1alpha1.ServiceSplitter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "service",
			Namespace: "default",
		},
		Spec: v1alpha1.ServiceSplitterSpec{
			Splits: v1alpha1.ServiceSplits{
				{
					Weight:  100,
					Service: "service",
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(defaults, splitter).Build()

	consul, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(t, err)
	defer consul.Stop()

	consul.WaitForServiceIntentions(t)
	consulClient, err := capi.NewClient(&capi.Config{
		Address: consul.HTTPAddr,
	})
	require.NoError(t, err)

	logger := logrtest.TestLogger{T: t}

	svcDefaultsReconciler := ServiceDefaultsController{
		Client: fakeClient,
		Log:    logger,
		ConfigEntryController: &ConfigEntryController{
			ConsulClient:   consulClient,
			DatacenterName: datacenterName,
		},
	}
	svcSplitterReconciler := ServiceSplitterController{
		Client: fakeClient,
		Log:    logger,
		ConfigEntryController: &ConfigEntryController{
			ConsulClient:   consulClient,
			DatacenterName: datacenterName,
		},
	}

	defaultsNamespacedName := types.NamespacedName{
		Namespace: kubeNS,
		Name:      defaults.Name,
	}

	splitterNamespacedName := types.NamespacedName{
		Namespace: kubeNS,
		Name:      splitter.Name,
	}

	// Create config entries for service-defaults and service-splitter.
	resp, err := svcDefaultsReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: defaultsNamespacedName})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	resp, err = svcSplitterReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: splitterNamespacedName})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	err = fakeClient.Get(ctx, defaultsNamespacedName, defaults)
	require.NoError(t, err)

	// Update service-defaults with deletion timestamp so that it attempts deletion on reconcile.
	defaults.ObjectMeta.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	err = fakeClient.Update(ctx, defaults)
	require.NoError(t, err)

	// Reconcile should fail as the service-splitter still required the service-defaults causing the delete operation on Consul to fail.
	resp, err = svcDefaultsReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: defaultsNamespacedName})
	require.EqualError(t, err, "deleting config entry from consul: Unexpected response code: 500 (discovery chain \"service\" uses a protocol \"tcp\" that does not permit advanced routing or splitting behavior)")
	require.False(t, resp.Requeue)

	err = fakeClient.Get(ctx, defaultsNamespacedName, defaults)
	require.NoError(t, err)

	// Ensure the status of the resource is updated to display failure reason.
	syncCondition := defaults.GetCondition(v1alpha1.ConditionSynced)
	expectedCondition := &v1alpha1.Condition{
		Type:    v1alpha1.ConditionSynced,
		Status:  corev1.ConditionFalse,
		Reason:  ConsulAgentError,
		Message: "deleting config entry from consul: Unexpected response code: 500 (discovery chain \"service\" uses a protocol \"tcp\" that does not permit advanced routing or splitting behavior)",
	}
	require.True(t, cmp.Equal(syncCondition, expectedCondition, cmpopts.IgnoreFields(v1alpha1.Condition{}, "LastTransitionTime")))
}

// Test that if the resource already exists in Consul but the Kube resource
// has the "migrate-entry" annotation then we let the Kube resource sync to Consul.
func TestConfigEntryController_Migration(t *testing.T) {
	kubeNS := "default"
	protocol := "http"
	cfgEntryName := "service"

	cases := map[string]struct {
		KubeResource   v1alpha1.ServiceDefaults
		ConsulResource capi.ServiceConfigEntry
		ExpErr         string
	}{
		"identical resources should be migrated successfully": {
			KubeResource: v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cfgEntryName,
					Namespace: kubeNS,
					Annotations: map[string]string{
						common.MigrateEntryKey: "true",
					},
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: protocol,
				},
			},
			ConsulResource: capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Name:     cfgEntryName,
				Protocol: protocol,
			},
		},
		"different resources (protocol) should not be migrated": {
			KubeResource: v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cfgEntryName,
					Namespace: kubeNS,
					Annotations: map[string]string{
						common.MigrateEntryKey: "true",
					},
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "tcp",
				},
			},
			ConsulResource: capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Name:     cfgEntryName,
				Protocol: protocol,
			},
			ExpErr: "migration failed: Kubernetes resource does not match existing Consul config entry",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, &v1alpha1.ServiceDefaults{})

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(&c.KubeResource).Build()
			consul, err := testutil.NewTestServerConfigT(t, nil)
			require.NoError(t, err)
			defer consul.Stop()

			consul.WaitForServiceIntentions(t)
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			require.NoError(t, err)

			// Create the service-defaults in Consul.
			success, _, err := consulClient.ConfigEntries().Set(&c.ConsulResource, nil)
			require.NoError(t, err)
			require.True(t, success, "config entry was not created")

			// Set up the reconciler.
			logger := logrtest.TestLogger{T: t}
			svcDefaultsReconciler := ServiceDefaultsController{
				Client: fakeClient,
				Log:    logger,
				ConfigEntryController: &ConfigEntryController{
					ConsulClient:   consulClient,
					DatacenterName: datacenterName,
				},
			}

			defaultsNamespacedName := types.NamespacedName{
				Namespace: kubeNS,
				Name:      cfgEntryName,
			}

			// Trigger the reconciler.
			resp, err := svcDefaultsReconciler.Reconcile(ctx, ctrl.Request{NamespacedName: defaultsNamespacedName})
			if c.ExpErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), c.ExpErr)
			} else {
				require.NoError(t, err)
				require.False(t, resp.Requeue)
			}

			entryAfterReconcile := &v1alpha1.ServiceDefaults{}
			err = fakeClient.Get(ctx, defaultsNamespacedName, entryAfterReconcile)
			require.NoError(t, err)

			syncCondition := entryAfterReconcile.GetCondition(v1alpha1.ConditionSynced)
			if c.ExpErr != "" {
				// Ensure the status of the resource is migration failed.
				require.Equal(t, corev1.ConditionFalse, syncCondition.Status)
				require.Equal(t, MigrationFailedError, syncCondition.Reason)
				require.Contains(t, syncCondition.Message, c.ExpErr)

				// Check that the Consul resource hasn't changed.
				entry, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, cfgEntryName, nil)
				require.NoError(t, err)
				require.NotContains(t, entry.GetMeta(), common.DatacenterKey)
				require.Equal(t, protocol, entry.(*capi.ServiceConfigEntry).Protocol)
			} else {
				// Ensure the status of the resource is synced.
				expectedCondition := &v1alpha1.Condition{
					Type:   v1alpha1.ConditionSynced,
					Status: corev1.ConditionTrue,
				}
				require.True(t, cmp.Equal(syncCondition, expectedCondition, cmpopts.IgnoreFields(v1alpha1.Condition{}, "LastTransitionTime")))

				// Ensure the Consul resource has the expected metadata.
				entry, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, cfgEntryName, nil)
				require.NoError(t, err)
				require.Contains(t, entry.GetMeta(), common.DatacenterKey)
				require.Equal(t, "datacenter", entry.GetMeta()[common.DatacenterKey])
			}
		})
	}
}
