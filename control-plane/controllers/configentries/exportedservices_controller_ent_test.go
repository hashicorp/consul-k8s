// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package configentries_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	logrtest "github.com/go-logr/logr/testing"
	capi "github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/hashicorp/consul-k8s/control-plane/api/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/controllers/configentries"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

// This tests explicitly tests ExportedServicesController instead of using the existing
// pattern of adding tests for the controller to configentry_controller test. That is because
// unlike the other CRDs, ExportedServices are only supported in Consul Enterprise. But the
// test pattern of the enterprise tests already covers a config-entry similar to partition-exports
// ie a "global" configentry. Hence a separate file has been created to test this controller.

func TestExportedServicesController_createsExportedServices(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
		},
	}

	for name, c := range cases {
		tt.Run(name, func(t *testing.T) {
			req := require.New(t)
			s := runtime.NewScheme()
			exportedServices := &v1alpha1.ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "default",
					Namespace: c.SourceKubeNS,
				},
				Spec: v1alpha1.ExportedServicesSpec{
					Services: []v1alpha1.ExportedService{
						{
							Name:      "frontend",
							Namespace: "front",
							Consumers: []v1alpha1.ServiceConsumer{
								{Partition: "foo"},
								{Partition: "bar"},
							},
						},
					},
				},
			}
			s.AddKnownTypes(v1alpha1.GroupVersion, exportedServices)
			ctx := context.Background()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			testClient.TestServer.WaitForServiceIntentions(t)
			consulClient := testClient.APIClient

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(exportedServices).WithStatusSubresource(exportedServices).Build()

			controller := &configentries.ExportedServicesController{
				Client: fakeClient,
				Log:    logrtest.NewTestLogger(t),
				Scheme: s,
				ConfigEntryController: &configentries.ConfigEntryController{
					ConsulClientConfig:         testClient.Cfg,
					ConsulServerConnMgr:        testClient.Watcher,
					EnableConsulNamespaces:     true,
					EnableNSMirroring:          c.Mirror,
					NSMirroringPrefix:          c.MirrorPrefix,
					ConsulDestinationNamespace: c.DestConsulNS,
				},
			}

			resp, err := controller.Reconcile(ctx, ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: c.SourceKubeNS,
					Name:      exportedServices.KubernetesName(),
				},
			})
			req.NoError(err)
			req.False(resp.Requeue)

			cfg, _, err := consulClient.ConfigEntries().Get(capi.ExportedServices, exportedServices.ConsulName(), &capi.QueryOptions{
				Namespace: common.DefaultConsulNamespace,
			})
			req.NoError(err)

			configEntry, ok := cfg.(*capi.ExportedServicesConfigEntry)
			req.True(ok)
			req.Equal(configEntry.Services[0].Name, "frontend")

			// Check that the status is "synced".
			err = fakeClient.Get(ctx, types.NamespacedName{
				Namespace: c.SourceKubeNS,
				Name:      exportedServices.KubernetesName(),
			}, exportedServices)
			req.NoError(err)
			conditionSynced := exportedServices.SyncedConditionStatus()
			req.Equal(conditionSynced, corev1.ConditionTrue)
		})
	}
}

func TestExportedServicesController_updatesExportedServices(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
		},
	}

	for name, c := range cases {
		tt.Run(name, func(t *testing.T) {
			req := require.New(t)
			s := runtime.NewScheme()
			exportedServices := &v1alpha1.ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "default",
					Namespace:  c.SourceKubeNS,
					Finalizers: []string{configentries.FinalizerName},
				},
				Spec: v1alpha1.ExportedServicesSpec{
					Services: []v1alpha1.ExportedService{
						{
							Name:      "frontend",
							Namespace: "front",
							Consumers: []v1alpha1.ServiceConsumer{
								{Partition: "foo"},
								{Partition: "bar"},
							},
						},
					},
				},
			}
			s.AddKnownTypes(v1alpha1.GroupVersion, exportedServices)
			ctx := context.Background()

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			testClient.TestServer.WaitForServiceIntentions(t)
			consulClient := testClient.APIClient
			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(exportedServices).WithStatusSubresource(exportedServices).Build()

			controller := &configentries.ExportedServicesController{
				Client: fakeClient,
				Log:    logrtest.NewTestLogger(t),
				Scheme: s,
				ConfigEntryController: &configentries.ConfigEntryController{
					ConsulClientConfig:         testClient.Cfg,
					ConsulServerConnMgr:        testClient.Watcher,
					EnableConsulNamespaces:     true,
					EnableNSMirroring:          c.Mirror,
					NSMirroringPrefix:          c.MirrorPrefix,
					ConsulDestinationNamespace: c.DestConsulNS,
				},
			}

			// We haven't run reconcile yet so ensure it's created in Consul.
			{
				_, _, err := consulClient.ConfigEntries().Set(&capi.ExportedServicesConfigEntry{
					Name: "default",
					Services: []capi.ExportedService{
						{
							Name:      "frontend",
							Namespace: "front",
							Consumers: []capi.ServiceConsumer{
								{Partition: "foo"},
								{Partition: "bar"},
							},
						},
					},
				}, &capi.WriteOptions{Namespace: common.DefaultConsulNamespace})
				req.NoError(err)
			}

			// Now update it.
			{
				// First get it so we have the latest revision number.
				err := fakeClient.Get(ctx, types.NamespacedName{
					Namespace: c.SourceKubeNS,
					Name:      exportedServices.KubernetesName(),
				}, exportedServices)
				req.NoError(err)

				// Update the resource.
				exportedServices.Spec.Services[0].Name = "backend"
				err = fakeClient.Update(ctx, exportedServices)
				req.NoError(err)

				resp, err := controller.Reconcile(ctx, ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: c.SourceKubeNS,
						Name:      exportedServices.KubernetesName(),
					},
				})
				req.NoError(err)
				req.False(resp.Requeue)

				cfg, _, err := consulClient.ConfigEntries().Get(capi.ExportedServices, exportedServices.ConsulName(), &capi.QueryOptions{
					Namespace: common.DefaultConsulNamespace,
				})
				req.NoError(err)
				entry := cfg.(*capi.ExportedServicesConfigEntry)
				req.Equal("backend", entry.Services[0].Name)
			}
		})
	}
}

func TestExportedServicesController_deletesExportedServices(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		Mirror       bool
		MirrorPrefix string
		SourceKubeNS string
		DestConsulNS string
	}{
		"SourceKubeNS=default, DestConsulNS=default": {
			SourceKubeNS: "default",
			DestConsulNS: "default",
		},
		"SourceKubeNS=kube, DestConsulNS=default": {
			SourceKubeNS: "kube",
			DestConsulNS: "default",
		},
		"SourceKubeNS=default, DestConsulNS=other": {
			SourceKubeNS: "default",
			DestConsulNS: "other",
		},
		"SourceKubeNS=kube, DestConsulNS=other": {
			SourceKubeNS: "kube",
			DestConsulNS: "other",
		},
		"SourceKubeNS=default, Mirror=true": {
			SourceKubeNS: "default",
			Mirror:       true,
		},
		"SourceKubeNS=kube, Mirror=true": {
			SourceKubeNS: "kube",
			Mirror:       true,
		},
		"SourceKubeNS=default, Mirror=true, Prefix=prefix": {
			SourceKubeNS: "default",
			Mirror:       true,
			MirrorPrefix: "prefix-",
		},
	}

	for name, c := range cases {
		tt.Run(name, func(t *testing.T) {
			req := require.New(t)
			s := runtime.NewScheme()
			exportedServices := &v1alpha1.ExportedServices{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "default",
					Namespace:         c.SourceKubeNS,
					Finalizers:        []string{configentries.FinalizerName},
					DeletionTimestamp: &metav1.Time{Time: time.Now()},
				},
				Spec: v1alpha1.ExportedServicesSpec{
					Services: []v1alpha1.ExportedService{
						{
							Name:      "frontend",
							Namespace: "front",
							Consumers: []v1alpha1.ServiceConsumer{
								{Partition: "foo"},
								{Partition: "bar"},
							},
						},
					},
				},
			}
			s.AddKnownTypes(v1alpha1.GroupVersion, exportedServices)

			testClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			testClient.TestServer.WaitForServiceIntentions(t)
			consulClient := testClient.APIClient

			fakeClient := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(exportedServices).WithStatusSubresource(exportedServices).Build()

			controller := &configentries.ExportedServicesController{
				Client: fakeClient,
				Log:    logrtest.NewTestLogger(t),
				Scheme: s,
				ConfigEntryController: &configentries.ConfigEntryController{
					ConsulClientConfig:         testClient.Cfg,
					ConsulServerConnMgr:        testClient.Watcher,
					EnableConsulNamespaces:     true,
					EnableNSMirroring:          c.Mirror,
					NSMirroringPrefix:          c.MirrorPrefix,
					ConsulDestinationNamespace: c.DestConsulNS,
				},
			}

			// We haven't run reconcile yet so ensure it's created in Consul.
			{
				_, _, err := consulClient.ConfigEntries().Set(&capi.ExportedServicesConfigEntry{
					Name: "default",
					Services: []capi.ExportedService{
						{
							Name:      "frontend",
							Namespace: "front",
							Consumers: []capi.ServiceConsumer{
								{Partition: "foo"},
								{Partition: "bar"},
							},
						},
					},
				},
					&capi.WriteOptions{Namespace: common.DefaultConsulNamespace})
				req.NoError(err)
			}

			// Now run reconcile. It's marked for deletion so this should delete it.
			{
				resp, err := controller.Reconcile(context.Background(), ctrl.Request{
					NamespacedName: types.NamespacedName{
						Namespace: c.SourceKubeNS,
						Name:      exportedServices.KubernetesName(),
					},
				})
				req.NoError(err)
				req.False(resp.Requeue)

				_, _, err = consulClient.ConfigEntries().Get(capi.ExportedServices, exportedServices.ConsulName(), &capi.QueryOptions{
					Namespace: common.DefaultConsulNamespace,
				})
				req.EqualError(err, fmt.Sprintf(`Unexpected response code: 404 (Config entry not found for "%s" / "%s")`, capi.ExportedServices, exportedServices.ConsulName()))
			}
		})
	}
}
