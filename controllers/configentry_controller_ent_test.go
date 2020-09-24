// +build enterprise

package controllers_test

import (
	"context"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testing"
	"github.com/hashicorp/consul-k8s/api/common"
	"github.com/hashicorp/consul-k8s/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/controllers"
	capi "github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// NOTE: We're not testing each controller type here because that's done in
// the OSS tests and it would result in too many permutations. Instead
// we're only testing with the ServiceDefaults controller which will exercise
// all the namespaces code.

func TestConfigEntryController_createsConfigEntry_consulNamespaces(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}

	for name, c := range cases {
		tt.Run(name, func(t *testing.T) {
			req := require.New(t)
			svcDefaults := &v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "foo",
					Namespace: c.SourceKubeNS,
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "http",
				},
			}
			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)
			ctx := context.Background()

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			client := fake.NewFakeClientWithScheme(s, svcDefaults)

			r := controllers.ServiceDefaultsController{
				Client: client,
				Log:    logrtest.TestLogger{T: t},
				Scheme: s,
				ConfigEntryController: &controllers.ConfigEntryController{
					ConsulClient:               consulClient,
					EnableConsulNamespaces:     true,
					EnableNSMirroring:          c.Mirror,
					NSMirroringPrefix:          c.MirrorPrefix,
					ConsulDestinationNamespace: c.DestConsulNS,
				},
			}

			resp, err := r.Reconcile(ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: svcDefaults.ObjectMeta.Namespace,
					Name:      svcDefaults.ObjectMeta.Name,
				},
			})
			req.NoError(err)
			req.False(resp.Requeue)

			cfg, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, "foo", &capi.QueryOptions{
				Namespace: c.ExpConsulNS,
			})
			req.NoError(err)
			svcDefault, ok := cfg.(*capi.ServiceConfigEntry)
			req.True(ok)
			req.Equal("http", svcDefault.Protocol)

			// Check that the status is "synced".
			err = client.Get(ctx, types.NamespacedName{
				Namespace: svcDefaults.Namespace,
				Name:      svcDefaults.Name(),
			}, svcDefaults)
			req.NoError(err)
			conditionSynced := svcDefaults.Status.GetCondition(v1alpha1.ConditionSynced)
			req.True(conditionSynced.IsTrue())

		})
	}
}

func TestConfigEntryController_updatesConfigEntry_consulNamespaces(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}

	for name, c := range cases {
		tt.Run(name, func(t *testing.T) {
			req := require.New(t)
			svcDefaults := &v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "foo",
					Namespace:  c.SourceKubeNS,
					Finalizers: []string{controllers.FinalizerName},
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "http",
				},
			}
			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)
			ctx := context.Background()

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			client := fake.NewFakeClientWithScheme(s, svcDefaults)

			r := controllers.ServiceDefaultsController{
				Client: client,
				Log:    logrtest.TestLogger{T: t},
				Scheme: s,
				ConfigEntryController: &controllers.ConfigEntryController{
					ConsulClient:               consulClient,
					EnableConsulNamespaces:     true,
					EnableNSMirroring:          c.Mirror,
					NSMirroringPrefix:          c.MirrorPrefix,
					ConsulDestinationNamespace: c.DestConsulNS,
				},
			}

			// We haven't run reconcile yet so ensure it's created in Consul.
			{
				if c.ExpConsulNS != "default" {
					_, _, err := consulClient.Namespaces().Create(&capi.Namespace{
						Name: c.ExpConsulNS,
					}, nil)
					req.NoError(err)
				}
				written, _, err := consulClient.ConfigEntries().Set(&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				}, &capi.WriteOptions{Namespace: c.ExpConsulNS})
				req.NoError(err)
				req.True(written)
			}

			// Now update it.
			{
				// First get it so we have the latest revision number.
				err = client.Get(ctx, types.NamespacedName{
					Namespace: svcDefaults.Namespace,
					Name:      svcDefaults.Name(),
				}, svcDefaults)
				req.NoError(err)

				// Update the protocol.
				svcDefaults.Spec.Protocol = "tcp"
				err := client.Update(ctx, svcDefaults)
				req.NoError(err)

				resp, err := r.Reconcile(ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: svcDefaults.ObjectMeta.Namespace,
						Name:      svcDefaults.ObjectMeta.Name,
					},
				})
				req.NoError(err)
				req.False(resp.Requeue)

				cfg, _, err := consulClient.ConfigEntries().Get(capi.ServiceDefaults, "foo", &capi.QueryOptions{Namespace: c.ExpConsulNS})
				req.NoError(err)
				svcDefault, ok := cfg.(*capi.ServiceConfigEntry)
				req.True(ok)
				req.Equal("tcp", svcDefault.Protocol)
			}
		})
	}
}

func TestConfigEntryController_deletesConfigEntry_consulNamespaces(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}

	for name, c := range cases {
		tt.Run(name, func(t *testing.T) {
			req := require.New(t)
			// Create it with the deletion timestamp set to mimic that it's already
			// been marked for deletion.
			svcDefaults := &v1alpha1.ServiceDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "foo",
					Namespace:         c.SourceKubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{controllers.FinalizerName},
				},
				Spec: v1alpha1.ServiceDefaultsSpec{
					Protocol: "http",
				},
			}
			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, svcDefaults)

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			client := fake.NewFakeClientWithScheme(s, svcDefaults)

			r := controllers.ServiceDefaultsController{
				Client: client,
				Log:    logrtest.TestLogger{T: t},
				Scheme: s,
				ConfigEntryController: &controllers.ConfigEntryController{
					ConsulClient:               consulClient,
					EnableConsulNamespaces:     true,
					EnableNSMirroring:          c.Mirror,
					NSMirroringPrefix:          c.MirrorPrefix,
					ConsulDestinationNamespace: c.DestConsulNS,
				},
			}

			// We haven't run reconcile yet so ensure it's created in Consul.
			{
				if c.ExpConsulNS != "default" {
					_, _, err := consulClient.Namespaces().Create(&capi.Namespace{
						Name: c.ExpConsulNS,
					}, nil)
					req.NoError(err)
				}

				written, _, err := consulClient.ConfigEntries().Set(&capi.ServiceConfigEntry{
					Kind:     capi.ServiceDefaults,
					Name:     "foo",
					Protocol: "http",
				}, &capi.WriteOptions{Namespace: c.ExpConsulNS})
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile. It's marked for deletion so this should delete it.
			{
				resp, err := r.Reconcile(ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: svcDefaults.ObjectMeta.Namespace,
						Name:      svcDefaults.ObjectMeta.Name,
					},
				})
				req.NoError(err)
				req.False(resp.Requeue)

				_, _, err = consulClient.ConfigEntries().Get(capi.ServiceDefaults, "foo", &capi.QueryOptions{Namespace: c.ExpConsulNS})
				req.EqualError(err, "Unexpected response code: 404 (Config entry not found for \"service-defaults\" / \"foo\")")
			}
		})
	}
}

func TestConfigEntryController_createsGlobalConfigEntry_consulNamespaces(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}

	for name, c := range cases {
		tt.Run(name, func(t *testing.T) {
			req := require.New(t)
			proxyDefaults := &v1alpha1.ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:      common.Global,
					Namespace: c.SourceKubeNS,
				},
				Spec: v1alpha1.ProxyDefaultsSpec{
					MeshGateway: v1alpha1.MeshGatewayConfig{
						Mode: "remote",
					},
				},
			}

			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, proxyDefaults)
			ctx := context.Background()

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			client := fake.NewFakeClientWithScheme(s, proxyDefaults)

			proxyDefaultsController := controllers.ProxyDefaultsController{
				Client: client,
				Log:    logrtest.TestLogger{T: t},
				Scheme: s,
				ConfigEntryController: &controllers.ConfigEntryController{
					ConsulClient:               consulClient,
					EnableConsulNamespaces:     true,
					EnableNSMirroring:          c.Mirror,
					NSMirroringPrefix:          c.MirrorPrefix,
					ConsulDestinationNamespace: c.DestConsulNS,
				},
			}

			resp, err := proxyDefaultsController.Reconcile(ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: proxyDefaults.ObjectMeta.Namespace,
					Name:      proxyDefaults.ObjectMeta.Name,
				},
			})
			req.NoError(err)
			req.False(resp.Requeue)

			cfg, _, err := consulClient.ConfigEntries().Get(capi.ProxyDefaults, common.Global, &capi.QueryOptions{
				Namespace: common.DefaultConsulNamespace,
			})
			req.NoError(err)
			proxyDefault, ok := cfg.(*capi.ProxyConfigEntry)
			req.True(ok)
			req.Equal(capi.MeshGatewayModeRemote, proxyDefault.MeshGateway.Mode)

			// Check that the status is "synced".
			err = client.Get(ctx, types.NamespacedName{
				Namespace: proxyDefaults.Namespace,
				Name:      proxyDefaults.Name(),
			}, proxyDefaults)
			req.NoError(err)
			conditionSynced := proxyDefaults.Status.GetCondition(v1alpha1.ConditionSynced)
			req.True(conditionSynced.IsTrue())
		})
	}
}

func TestConfigEntryController_updatesGlobalConfigEntry_consulNamespaces(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}

	for name, c := range cases {
		tt.Run(name, func(t *testing.T) {
			req := require.New(t)
			proxyDefaults := &v1alpha1.ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:       common.Global,
					Namespace:  c.SourceKubeNS,
					Finalizers: []string{controllers.FinalizerName},
				},
				Spec: v1alpha1.ProxyDefaultsSpec{
					MeshGateway: v1alpha1.MeshGatewayConfig{
						Mode: "remote",
					},
				},
			}
			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, proxyDefaults)
			ctx := context.Background()

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			client := fake.NewFakeClientWithScheme(s, proxyDefaults)

			r := controllers.ProxyDefaultsController{
				Client: client,
				Log:    logrtest.TestLogger{T: t},
				Scheme: s,
				ConfigEntryController: &controllers.ConfigEntryController{
					ConsulClient:               consulClient,
					EnableConsulNamespaces:     true,
					EnableNSMirroring:          c.Mirror,
					NSMirroringPrefix:          c.MirrorPrefix,
					ConsulDestinationNamespace: c.DestConsulNS,
				},
			}

			// We haven't run reconcile yet so ensure it's created in Consul.
			{
				if c.ExpConsulNS != "default" {
					_, _, err := consulClient.Namespaces().Create(&capi.Namespace{
						Name: c.ExpConsulNS,
					}, nil)
					req.NoError(err)
				}
				written, _, err := consulClient.ConfigEntries().Set(&capi.ProxyConfigEntry{
					Kind: capi.ProxyDefaults,
					Name: common.Global,
					MeshGateway: capi.MeshGatewayConfig{
						Mode: capi.MeshGatewayModeLocal,
					},
				}, &capi.WriteOptions{Namespace: common.DefaultConsulNamespace})
				req.NoError(err)
				req.True(written)
			}

			// Now update it.
			{
				// First get it so we have the latest revision number.
				err = client.Get(ctx, types.NamespacedName{
					Namespace: proxyDefaults.Namespace,
					Name:      proxyDefaults.Name(),
				}, proxyDefaults)
				req.NoError(err)

				// Update the protocol.
				proxyDefaults.Spec.MeshGateway.Mode = "remote"
				err := client.Update(ctx, proxyDefaults)
				req.NoError(err)

				resp, err := r.Reconcile(ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: proxyDefaults.ObjectMeta.Namespace,
						Name:      proxyDefaults.ObjectMeta.Name,
					},
				})
				req.NoError(err)
				req.False(resp.Requeue)

				cfg, _, err := consulClient.ConfigEntries().Get(capi.ProxyDefaults, common.Global, &capi.QueryOptions{Namespace: common.DefaultConsulNamespace})
				req.NoError(err)
				svcDefault, ok := cfg.(*capi.ProxyConfigEntry)
				req.True(ok)
				req.Equal(capi.MeshGatewayModeRemote, svcDefault.MeshGateway.Mode)
			}
		})
	}
}

func TestConfigEntryController_deletesGlobalConfigEntry_consulNamespaces(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
		ExpConsulNS  string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
			ExpConsulNS:  "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
			ExpConsulNS:  "default",
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
			ExpConsulNS:  "kube",
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
			ExpConsulNS:  "prefix-default",
		},
	}

	for name, c := range cases {
		tt.Run(name, func(t *testing.T) {
			req := require.New(t)
			// Create it with the deletion timestamp set to mimic that it's already
			// been marked for deletion.
			proxyDefaults := &v1alpha1.ProxyDefaults{
				ObjectMeta: metav1.ObjectMeta{
					Name:              common.Global,
					Namespace:         c.SourceKubeNS,
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
					Finalizers:        []string{controllers.FinalizerName},
				},
				Spec: v1alpha1.ProxyDefaultsSpec{
					MeshGateway: v1alpha1.MeshGatewayConfig{
						Mode: "remote",
					},
				},
			}
			s := runtime.NewScheme()
			s.AddKnownTypes(v1alpha1.GroupVersion, proxyDefaults)

			consul, err := testutil.NewTestServerConfigT(t, nil)
			req.NoError(err)
			defer consul.Stop()
			consulClient, err := capi.NewClient(&capi.Config{
				Address: consul.HTTPAddr,
			})
			req.NoError(err)

			client := fake.NewFakeClientWithScheme(s, proxyDefaults)

			r := controllers.ProxyDefaultsController{
				Client: client,
				Log:    logrtest.TestLogger{T: t},
				Scheme: s,
				ConfigEntryController: &controllers.ConfigEntryController{
					ConsulClient:               consulClient,
					EnableConsulNamespaces:     true,
					EnableNSMirroring:          c.Mirror,
					NSMirroringPrefix:          c.MirrorPrefix,
					ConsulDestinationNamespace: c.DestConsulNS,
				},
			}

			// We haven't run reconcile yet so ensure it's created in Consul.
			{
				if c.ExpConsulNS != "default" {
					_, _, err := consulClient.Namespaces().Create(&capi.Namespace{
						Name: c.ExpConsulNS,
					}, nil)
					req.NoError(err)
				}

				written, _, err := consulClient.ConfigEntries().Set(&capi.ProxyConfigEntry{
					Kind: capi.ProxyDefaults,
					Name: common.Global,
					MeshGateway: capi.MeshGatewayConfig{
						Mode: "local",
					},
				}, &capi.WriteOptions{Namespace: c.ExpConsulNS})
				req.NoError(err)
				req.True(written)
			}

			// Now run reconcile. It's marked for deletion so this should delete it.
			{
				resp, err := r.Reconcile(ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: proxyDefaults.ObjectMeta.Namespace,
						Name:      proxyDefaults.ObjectMeta.Name,
					},
				})
				req.NoError(err)
				req.False(resp.Requeue)

				_, _, err = consulClient.ConfigEntries().Get(capi.ProxyDefaults, common.Global, &capi.QueryOptions{Namespace: common.DefaultConsulNamespace})
				req.EqualError(err, "Unexpected response code: 404 (Config entry not found for \"proxy-defaults\" / \"global\")")
			}
		})
	}
}
