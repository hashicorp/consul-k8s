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

const datacenterName = "datacenter"

type testReconciler interface {
	Reconcile(req ctrl.Request) (ctrl.Result, error)
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
			client := fake.NewFakeClientWithScheme(s, c.configEntryResource)

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

			r := c.reconciler(client, consulClient, logrtest.TestLogger{T: t})
			namespacedName := types.NamespacedName{
				Namespace: kubeNS,
				Name:      c.configEntryResource.KubernetesName(),
			}
			resp, err := r.Reconcile(ctrl.Request{
				NamespacedName: namespacedName,
			})
			req.NoError(err)
			req.False(resp.Requeue)

			cfg, _, err := consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResource.ConsulName(), nil)
			req.NoError(err)
			req.Equal(c.configEntryResource.ConsulName(), cfg.GetName())
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
			client := fake.NewFakeClientWithScheme(s, c.configEntryResource)

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
			client := fake.NewFakeClientWithScheme(s, c.configEntryResourceWithDeletion)

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
				r := c.reconciler(client, consulClient, logrtest.TestLogger{T: t})
				resp, err := r.Reconcile(ctrl.Request{
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
		},
		{
			kubeKind:   "ServiceRouter",
			consulKind: capi.ServiceRouter,
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
		},
		{
			kubeKind:   "ServiceIntentions",
			consulKind: capi.ServiceIntentions,
			configEntryResource: &v1alpha1.ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceIntentionsSpec{
					Destination: v1alpha1.Destination{
						Name: "foo",
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
			configEntryResource: &v1alpha1.IngressGateway{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.IngressGatewaySpec{
					TLS: v1alpha1.GatewayTLSConfig{
						Enabled: true,
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
				Name:      c.configEntryResource.KubernetesName(),
			}
			resp, err := r.Reconcile(ctrl.Request{
				NamespacedName: namespacedName,
			})
			req.Error(err)

			expErr := fmt.Sprintf("Get \"http://incorrect-address/v1/config/%s/%s\": dial tcp: lookup incorrect-address", c.consulKind, c.configEntryResource.ConsulName())
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

// Test that if the config entry hasn't changed in Consul but our resource
// synced status isn't set to true then we update its status.
func TestConfigEntryControllers_setsSyncedToTrue(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		kubeKind            string
		consulKind          string
		consulPrereq        capi.ConfigEntry
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
				Status: v1alpha1.Status{
					Conditions: v1alpha1.Conditions{
						{
							Type:   v1alpha1.ConditionSynced,
							Status: corev1.ConditionUnknown,
						},
					},
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
				Status: v1alpha1.Status{
					Conditions: v1alpha1.Conditions{
						{
							Type:   v1alpha1.ConditionSynced,
							Status: corev1.ConditionUnknown,
						},
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
				Status: v1alpha1.Status{
					Conditions: v1alpha1.Conditions{
						{
							Type:   v1alpha1.ConditionSynced,
							Status: corev1.ConditionUnknown,
						},
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
			kubeKind:   "ServiceRouter",
			consulKind: capi.ServiceRouter,
			consulPrereq: &capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Name:     "foo",
				Protocol: "http",
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
				Status: v1alpha1.Status{
					Conditions: v1alpha1.Conditions{
						{
							Type:   v1alpha1.ConditionSynced,
							Status: corev1.ConditionUnknown,
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
			kubeKind:   "ServiceIntentions",
			consulKind: capi.ServiceIntentions,
			consulPrereq: &capi.ServiceConfigEntry{
				Kind:     capi.ServiceDefaults,
				Name:     "foo",
				Protocol: "http",
			},
			configEntryResource: &v1alpha1.ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceIntentionsSpec{
					Destination: v1alpha1.Destination{
						Name: "foo",
					},
					Sources: v1alpha1.SourceIntentions{
						&v1alpha1.SourceIntention{
							Name:   "bar",
							Action: "deny",
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
				Status: v1alpha1.Status{
					Conditions: v1alpha1.Conditions{
						{
							Type:   v1alpha1.ConditionSynced,
							Status: corev1.ConditionUnknown,
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
				Status: v1alpha1.Status{
					Conditions: v1alpha1.Conditions{
						{
							Type:   v1alpha1.ConditionSynced,
							Status: corev1.ConditionUnknown,
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
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResource)

			// The config entry exists in kube but its status will be nil.
			client := fake.NewFakeClientWithScheme(s, c.configEntryResource)

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()

			consul.WaitForServiceIntentions(t)
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			// Create any prereqs.
			if c.consulPrereq != nil {
				written, _, err := consulClient.ConfigEntries().Set(c.consulPrereq, nil)
				req.NoError(err)
				req.True(written)
			}

			// Create the resource in Consul to mimic that it was created
			// successfully (but its status hasn't been updated).
			_, _, err = consulClient.ConfigEntries().Set(c.configEntryResource.ToConsul(datacenterName), nil)
			require.NoError(t, err)

			r := c.reconciler(client, consulClient, logrtest.TestLogger{T: t})
			namespacedName := types.NamespacedName{
				Namespace: kubeNS,
				Name:      c.configEntryResource.KubernetesName(),
			}
			resp, err := r.Reconcile(ctrl.Request{
				NamespacedName: namespacedName,
			})
			req.NoError(err)
			req.False(resp.Requeue)

			// Check that the status is now "synced".
			err = client.Get(ctx, namespacedName, c.configEntryResource)
			req.NoError(err)
			req.Equal(corev1.ConditionTrue, c.configEntryResource.SyncedConditionStatus())
		})
	}
}

// Test that if the config entry exists in Consul but is not managed by the
// controller, creating/updating the resource fails
func TestConfigEntryControllers_doesNotCreateUnownedConfigEntry(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		kubeKind            string
		consulKind          string
		consulPrereqs       []capi.ConfigEntry
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
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
					Name:      "test",
					Namespace: kubeNS,
				},
				Spec: v1alpha1.ServiceIntentionsSpec{
					Destination: v1alpha1.Destination{
						Name: "foo",
					},
					Sources: v1alpha1.SourceIntentions{
						&v1alpha1.SourceIntention{
							Name:   "bar",
							Action: "deny",
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

			// We haven't run reconcile yet. We must create the config entry
			// in Consul ourselves in a different datacenter.
			{
				written, _, err := consulClient.ConfigEntries().Set(c.configEntryResource.ToConsul("different-datacenter"), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile which should **not** update the entry in Consul.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      c.configEntryResource.KubernetesName(),
				}
				// First get it so we have the latest revision number.
				err = client.Get(ctx, namespacedName, c.configEntryResource)
				req.NoError(err)

				// Attempt to create the entry in Kube and run reconcile.
				r := c.reconciler(client, consulClient, logrtest.TestLogger{T: t})
				resp, err := r.Reconcile(ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.EqualError(err, "config entry managed in different datacenter: \"different-datacenter\"")
				req.False(resp.Requeue)

				// Now check that the object in Consul is as expected.
				cfg, _, err := consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResource.ConsulName(), nil)
				req.NoError(err)
				req.Equal(cfg.GetMeta()[common.DatacenterKey], "different-datacenter")

				// Check that the status is "synced=false".
				err = client.Get(ctx, namespacedName, c.configEntryResource)
				req.NoError(err)
				status, reason, errMsg := c.configEntryResource.SyncedCondition()
				req.Equal(corev1.ConditionFalse, status)
				req.Equal("ExternallyManagedConfigError", reason)
				req.Equal(errMsg, "config entry managed in different datacenter: \"different-datacenter\"")
			}
		})
	}
}

// Test that if the config entry exists in Consul but is not managed by the
// controller, deleting the resource does not delete the Consul config entry
func TestConfigEntryControllers_doesNotDeleteUnownedConfig(t *testing.T) {
	t.Parallel()
	kubeNS := "default"

	cases := []struct {
		kubeKind                        string
		consulKind                      string
		consulPrereq                    []capi.ConfigEntry
		configEntryResourceWithDeletion common.ConfigEntryResource
		reconciler                      func(client.Client, *capi.Client, logr.Logger) testReconciler
		confirmDelete                   func(*testing.T, client.Client, context.Context, types.NamespacedName)
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
			confirmDelete: func(t *testing.T, cli client.Client, ctx context.Context, name types.NamespacedName) {
				svcDefault := &v1alpha1.ServiceDefaults{}
				_ = cli.Get(ctx, name, svcDefault)
				require.Empty(t, svcDefault.Finalizers())
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
			confirmDelete: func(t *testing.T, cli client.Client, ctx context.Context, name types.NamespacedName) {
				svcResolver := &v1alpha1.ServiceResolver{}
				_ = cli.Get(ctx, name, svcResolver)
				require.Empty(t, svcResolver.Finalizers())
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
						ConsulClient:   consulClient,
						DatacenterName: datacenterName,
					},
				}
			},
			confirmDelete: func(t *testing.T, cli client.Client, ctx context.Context, name types.NamespacedName) {
				proxyDefault := &v1alpha1.ProxyDefaults{}
				_ = cli.Get(ctx, name, proxyDefault)
				require.Empty(t, proxyDefault.Finalizers())
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
			confirmDelete: func(t *testing.T, cli client.Client, ctx context.Context, name types.NamespacedName) {
				svcRouter := &v1alpha1.ServiceRouter{}
				_ = cli.Get(ctx, name, svcRouter)
				require.Empty(t, svcRouter.Finalizers())
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
			confirmDelete: func(t *testing.T, cli client.Client, ctx context.Context, name types.NamespacedName) {
				svcSplitter := &v1alpha1.ServiceSplitter{}
				_ = cli.Get(ctx, name, svcSplitter)
				require.Empty(t, svcSplitter.Finalizers())
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
			},
			configEntryResourceWithDeletion: &v1alpha1.ServiceIntentions{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "test",
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
			confirmDelete: func(t *testing.T, cli client.Client, ctx context.Context, name types.NamespacedName) {
				svcIntentions := &v1alpha1.ServiceIntentions{}
				_ = cli.Get(ctx, name, svcIntentions)
				require.Empty(t, svcIntentions.Finalizers())
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
			confirmDelete: func(t *testing.T, cli client.Client, ctx context.Context, name types.NamespacedName) {
				resource := &v1alpha1.IngressGateway{}
				_ = cli.Get(ctx, name, resource)
				require.Empty(t, resource.Finalizers())
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
			confirmDelete: func(t *testing.T, cli client.Client, ctx context.Context, name types.NamespacedName) {
				resource := &v1alpha1.TerminatingGateway{}
				_ = cli.Get(ctx, name, resource)
				require.Empty(t, resource.Finalizers())
			},
		},
	}

	for _, c := range cases {
		t.Run(c.kubeKind, func(t *testing.T) {
			req := require.New(t)
			ctx := context.Background()

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, c.configEntryResourceWithDeletion)
			client := fake.NewFakeClientWithScheme(s, c.configEntryResourceWithDeletion)

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
				// Create the resource with different datacenter on metadata
				written, _, err := consulClient.ConfigEntries().Set(c.configEntryResourceWithDeletion.ToConsul("different-datacenter"), nil)
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile. It's marked for deletion so this should delete the kubernetes resource
			// but not the consul config entry.
			{
				namespacedName := types.NamespacedName{
					Namespace: kubeNS,
					Name:      c.configEntryResourceWithDeletion.KubernetesName(),
				}
				r := c.reconciler(client, consulClient, logrtest.TestLogger{T: t})
				resp, err := r.Reconcile(ctrl.Request{
					NamespacedName: namespacedName,
				})
				req.NoError(err)
				req.False(resp.Requeue)

				entry, _, err := consulClient.ConfigEntries().Get(c.consulKind, c.configEntryResourceWithDeletion.ConsulName(), nil)
				req.NoError(err)
				req.Equal(entry.GetMeta()[common.DatacenterKey], "different-datacenter")

				// Check that the resource is deleted from cluster.
				c.confirmDelete(t, client, ctx, namespacedName)
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

	client := fake.NewFakeClientWithScheme(s, defaults, splitter)

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
		Client: client,
		Log:    logger,
		ConfigEntryController: &ConfigEntryController{
			ConsulClient:   consulClient,
			DatacenterName: datacenterName,
		},
	}
	svcSplitterReconciler := ServiceSplitterController{
		Client: client,
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
	resp, err := svcDefaultsReconciler.Reconcile(ctrl.Request{NamespacedName: defaultsNamespacedName})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	resp, err = svcSplitterReconciler.Reconcile(ctrl.Request{NamespacedName: splitterNamespacedName})
	require.NoError(t, err)
	require.False(t, resp.Requeue)

	err = client.Get(ctx, defaultsNamespacedName, defaults)
	require.NoError(t, err)

	// Update service-defaults with deletion timestamp so that it attempts deletion on reconcile.
	defaults.ObjectMeta.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	err = client.Update(ctx, defaults)
	require.NoError(t, err)

	// Reconcile should fail as the service-splitter still required the service-defaults causing the delete operation on Consul to fail.
	resp, err = svcDefaultsReconciler.Reconcile(ctrl.Request{NamespacedName: defaultsNamespacedName})
	require.EqualError(t, err, "deleting config entry from consul: Unexpected response code: 500 (discovery chain \"service\" uses a protocol \"tcp\" that does not permit advanced routing or splitting behavior)")
	require.False(t, resp.Requeue)

	err = client.Get(ctx, defaultsNamespacedName, defaults)
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
