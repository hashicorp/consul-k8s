// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package controllers

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"sync"
	"testing"
	"time"

	mapset "github.com/deckarep/golang-set"
	logrtest "github.com/go-logr/logr/testr"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul/api"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/cache"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestControllerDoesNotInfinitelyReconcile(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, gwv1alpha2.Install(s))
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	testCases := map[string]struct {
		namespace        string
		certFn           func(*testing.T, context.Context, client.WithWatch, string) *corev1.Secret
		gwFn             func(*testing.T, context.Context, client.WithWatch, string) *gwv1beta1.Gateway
		httpRouteFn      func(*testing.T, context.Context, client.WithWatch, *gwv1beta1.Gateway, *v1alpha1.RouteAuthFilter) *gwv1beta1.HTTPRoute
		tcpRouteFn       func(*testing.T, context.Context, client.WithWatch, *gwv1beta1.Gateway) *v1alpha2.TCPRoute
		externalFilterFn func(*testing.T, context.Context, client.WithWatch, string) *v1alpha1.RouteAuthFilter
		policyFn         func(*testing.T, context.Context, client.WithWatch, *gwv1beta1.Gateway, string)
	}{
		"all fields set": {
			namespace:   "consul",
			certFn:      createCert,
			gwFn:        createAllFieldsSetAPIGW,
			httpRouteFn: createAllFieldsSetHTTPRoute,
			tcpRouteFn:  createAllFieldsSetTCPRoute,
			externalFilterFn: func(_ *testing.T, _ context.Context, _ client.WithWatch, _ string) *v1alpha1.RouteAuthFilter {
				return nil
			},
			policyFn: func(_ *testing.T, _ context.Context, _ client.WithWatch, _ *gwv1beta1.Gateway, _ string) {},
		},
		"minimal fields set": {
			namespace:   "",
			certFn:      createCert,
			gwFn:        minimalFieldsSetAPIGW,
			httpRouteFn: minimalFieldsSetHTTPRoute,
			tcpRouteFn:  minimalFieldsSetTCPRoute,
			externalFilterFn: func(_ *testing.T, _ context.Context, _ client.WithWatch, _ string) *v1alpha1.RouteAuthFilter {
				return nil
			},
			policyFn: func(_ *testing.T, _ context.Context, _ client.WithWatch, _ *gwv1beta1.Gateway, _ string) {},
		},
		"funky casing to test normalization doesnt cause infinite reconciliation": {
			namespace:   "",
			certFn:      createCert,
			gwFn:        createFunkyCasingFieldsAPIGW,
			httpRouteFn: createFunkyCasingFieldsHTTPRoute,
			tcpRouteFn:  createFunkyCasingFieldsTCPRoute,
			externalFilterFn: func(_ *testing.T, _ context.Context, _ client.WithWatch, _ string) *v1alpha1.RouteAuthFilter {
				return nil
			},
			policyFn: func(_ *testing.T, _ context.Context, _ client.WithWatch, _ *gwv1beta1.Gateway, _ string) {},
		},
		"http route with JWT auth": {
			namespace:        "",
			certFn:           createCert,
			gwFn:             createAllFieldsSetAPIGW,
			httpRouteFn:      createJWTAuthHTTPRoute,
			tcpRouteFn:       createFunkyCasingFieldsTCPRoute,
			externalFilterFn: createRouteAuthFilter,
			policyFn:         func(_ *testing.T, _ context.Context, _ client.WithWatch, _ *gwv1beta1.Gateway, _ string) {},
		},
		"policy attached to gateway": {
			namespace:   "",
			certFn:      createCert,
			gwFn:        createAllFieldsSetAPIGW,
			httpRouteFn: createAllFieldsSetHTTPRoute,
			tcpRouteFn:  createFunkyCasingFieldsTCPRoute,
			externalFilterFn: func(_ *testing.T, _ context.Context, _ client.WithWatch, _ string) *v1alpha1.RouteAuthFilter {
				return nil
			},
			policyFn: createGWPolicy,
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			fakeClient := fake.NewClientBuilder().WithScheme(s).
				WithStatusSubresource(&gwv1beta1.Gateway{}, &gwv1beta1.HTTPRoute{}, &gwv1alpha2.TCPRoute{}, &v1alpha1.RouteAuthFilter{})
			k8sClient := registerFieldIndexersForTest(fakeClient).Build()
			consulTestServerClient := test.TestServerWithMockConnMgrWatcher(t, nil)
			ctx, cancel := context.WithCancel(context.Background())

			t.Cleanup(func() {
				cancel()
			})
			logger := logrtest.New(t)

			cacheCfg := cache.Config{
				ConsulClientConfig:  consulTestServerClient.Cfg,
				ConsulServerConnMgr: consulTestServerClient.Watcher,
				Logger:              logger,
			}
			resourceCache := cache.New(cacheCfg)

			gwCache := cache.NewGatewayCache(ctx, cacheCfg)

			gwCtrl := GatewayController{
				HelmConfig:            common.HelmConfig{},
				Log:                   logger,
				Translator:            common.ResourceTranslator{},
				cache:                 resourceCache,
				gatewayCache:          gwCache,
				Client:                k8sClient,
				allowK8sNamespacesSet: mapset.NewSet(),
				denyK8sNamespacesSet:  mapset.NewSet(),
			}

			go func() {
				resourceCache.Run(ctx)
			}()

			resourceCache.WaitSynced(ctx)

			gwSub := resourceCache.Subscribe(ctx, api.APIGateway, gwCtrl.transformConsulGateway)
			httpRouteSub := resourceCache.Subscribe(ctx, api.HTTPRoute, gwCtrl.transformConsulHTTPRoute(ctx))
			tcpRouteSub := resourceCache.Subscribe(ctx, api.TCPRoute, gwCtrl.transformConsulTCPRoute(ctx))
			fileSystemCertSub := resourceCache.Subscribe(ctx, api.FileSystemCertificate, gwCtrl.transformConsulFileSystemCertificate(ctx))

			cert := tc.certFn(t, ctx, k8sClient, tc.namespace)
			k8sGWObj := tc.gwFn(t, ctx, k8sClient, tc.namespace)

			// reconcile so we add the finalizer
			_, err := gwCtrl.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: k8sGWObj.Namespace,
					Name:      k8sGWObj.Name,
				},
			})
			require.NoError(t, err)

			// reconcile again so that we get the creation with the finalizer
			_, err = gwCtrl.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: k8sGWObj.Namespace,
					Name:      k8sGWObj.Name,
				},
			})
			require.NoError(t, err)

			jwtProvider := createJWTProvider(t, ctx, k8sClient)
			authFilterObj := tc.externalFilterFn(t, ctx, k8sClient, jwtProvider.Name)
			httpRouteObj := tc.httpRouteFn(t, ctx, k8sClient, k8sGWObj, authFilterObj)
			tcpRouteObj := tc.tcpRouteFn(t, ctx, k8sClient, k8sGWObj)
			tc.policyFn(t, ctx, k8sClient, k8sGWObj, jwtProvider.Name)

			// reconcile again so that we get the route bound to the gateway
			_, err = gwCtrl.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: k8sGWObj.Namespace,
					Name:      k8sGWObj.Name,
				},
			})
			require.NoError(t, err)

			// reconcile again so that we get the route bound to the gateway
			_, err = gwCtrl.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: k8sGWObj.Namespace,
					Name:      k8sGWObj.Name,
				},
			})
			require.NoError(t, err)

			wg := &sync.WaitGroup{}
			// we never get the event from the cert because when it's created there are no gateways that reference it
			wg.Add(3)
			go func(w *sync.WaitGroup) {
				gwDone := false
				httpRouteDone := false
				tcpRouteDone := false
				for {
					// get the creation events from the upsert and then continually read from channel so we dont block other subs
					select {
					case <-ctx.Done():
						return
					case <-gwSub.Events():
						if !gwDone {
							gwDone = true
							w.Done()
						}
					case <-httpRouteSub.Events():
						if !httpRouteDone {
							httpRouteDone = true
							w.Done()
						}
					case <-tcpRouteSub.Events():
						if !tcpRouteDone {
							tcpRouteDone = true
							w.Done()
						}
					case <-fileSystemCertSub.Events():
					}
				}
			}(wg)

			wg.Wait()

			gwNamespaceName := types.NamespacedName{
				Name:      k8sGWObj.Name,
				Namespace: k8sGWObj.Namespace,
			}

			httpRouteNamespaceName := types.NamespacedName{
				Name:      httpRouteObj.Name,
				Namespace: httpRouteObj.Namespace,
			}

			tcpRouteNamespaceName := types.NamespacedName{
				Name:      tcpRouteObj.Name,
				Namespace: tcpRouteObj.Namespace,
			}

			certNamespaceName := types.NamespacedName{
				Name:      cert.Name,
				Namespace: cert.Namespace,
			}

			gwRef := gwCtrl.Translator.ConfigEntryReference(api.APIGateway, gwNamespaceName)
			httpRouteRef := gwCtrl.Translator.ConfigEntryReference(api.HTTPRoute, httpRouteNamespaceName)
			tcpRouteRef := gwCtrl.Translator.ConfigEntryReference(api.TCPRoute, tcpRouteNamespaceName)
			certRef := gwCtrl.Translator.ConfigEntryReference(api.FileSystemCertificate, certNamespaceName)

			curGWModifyIndex := resourceCache.Get(gwRef).GetModifyIndex()
			curHTTPRouteModifyIndex := resourceCache.Get(httpRouteRef).GetModifyIndex()
			curTCPRouteModifyIndex := resourceCache.Get(tcpRouteRef).GetModifyIndex()
			curCertModifyIndex := resourceCache.Get(certRef).GetModifyIndex()

			err = k8sClient.Get(ctx, gwNamespaceName, k8sGWObj)
			require.NoError(t, err)
			curGWResourceVersion := k8sGWObj.ResourceVersion

			err = k8sClient.Get(ctx, httpRouteNamespaceName, httpRouteObj)
			require.NoError(t, err)
			curHTTPRouteResourceVersion := httpRouteObj.ResourceVersion

			err = k8sClient.Get(ctx, tcpRouteNamespaceName, tcpRouteObj)
			require.NoError(t, err)
			curTCPRouteResourceVersion := tcpRouteObj.ResourceVersion

			err = k8sClient.Get(ctx, certNamespaceName, cert)
			require.NoError(t, err)
			curCertResourceVersion := cert.ResourceVersion

			go func() {
				// reconcile multiple times with no changes to be sure
				for i := 0; i < 5; i++ {
					_, err = gwCtrl.Reconcile(ctx, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Namespace: k8sGWObj.Namespace,
						},
					})
					require.NoError(t, err)
				}
			}()

			require.Never(t, func() bool {
				err = k8sClient.Get(ctx, gwNamespaceName, k8sGWObj)
				require.NoError(t, err)
				newGWResourceVersion := k8sGWObj.ResourceVersion

				err = k8sClient.Get(ctx, httpRouteNamespaceName, httpRouteObj)
				require.NoError(t, err)
				newHTTPRouteResourceVersion := httpRouteObj.ResourceVersion

				err = k8sClient.Get(ctx, tcpRouteNamespaceName, tcpRouteObj)
				require.NoError(t, err)
				newTCPRouteResourceVersion := tcpRouteObj.ResourceVersion

				err = k8sClient.Get(ctx, certNamespaceName, cert)
				require.NoError(t, err)
				newCertResourceVersion := cert.ResourceVersion

				return curGWModifyIndex == resourceCache.Get(gwRef).GetModifyIndex() &&
					curGWResourceVersion == newGWResourceVersion &&
					curHTTPRouteModifyIndex == resourceCache.Get(httpRouteRef).GetModifyIndex() &&
					curHTTPRouteResourceVersion == newHTTPRouteResourceVersion &&
					curTCPRouteModifyIndex == resourceCache.Get(tcpRouteRef).GetModifyIndex() &&
					curTCPRouteResourceVersion == newTCPRouteResourceVersion &&
					curCertModifyIndex == resourceCache.Get(certRef).GetModifyIndex() &&
					curCertResourceVersion == newCertResourceVersion
			}, time.Duration(2*time.Second), time.Duration(500*time.Millisecond), fmt.Sprintf("curGWModifyIndex: %d, newIndx: %d", curGWModifyIndex, resourceCache.Get(gwRef).GetModifyIndex()),
			)
		})
	}
}

func createAllFieldsSetAPIGW(t *testing.T, ctx context.Context, k8sClient client.WithWatch, namespace string) *gwv1beta1.Gateway {
	// listener one configuration
	listenerOneName := "listener-one"
	listenerOneHostname := "*.consul.io"
	listenerOnePort := 3366
	listenerOneProtocol := "https"

	// listener two configuration
	listenerTwoName := "listener-two"
	listenerTwoHostname := "*.consul.io"
	listenerTwoPort := 5432
	listenerTwoProtocol := "http"

	// listener three configuration
	listenerThreeName := "listener-three"
	listenerThreePort := 8081
	listenerThreeProtocol := "tcp"

	// listener four configuration
	listenerFourName := "listener-four"
	listenerFourHostname := "*.consul.io"
	listenerFourPort := 5433
	listenerFourProtocol := "http"

	// Write gw to k8s
	gwClassCfg := &v1alpha1.GatewayClassConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GatewayClassConfig",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "gateway-class-config",
		},
		Spec: v1alpha1.GatewayClassConfigSpec{},
	}
	gwClass := &gwv1beta1.GatewayClass{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GatewayClass",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatewayclass",
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: "consul.hashicorp.com/gateway-controller",
			ParametersRef: &gwv1beta1.ParametersReference{
				Group: "consul.hashicorp.com",
				Kind:  "GatewayClassConfig",
				Name:  "gateway-class-config",
			},
			Description: new(string),
		},
	}
	gw := &gwv1beta1.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "gw",
			Namespace:   namespace,
			Annotations: make(map[string]string),
		},
		Spec: gwv1beta1.GatewaySpec{
			GatewayClassName: gwv1beta1.ObjectName(gwClass.Name),
			Listeners: []gwv1beta1.Listener{
				{
					Name:     gwv1beta1.SectionName(listenerOneName),
					Hostname: common.PointerTo(gwv1beta1.Hostname(listenerOneHostname)),
					Port:     gwv1beta1.PortNumber(listenerOnePort),
					Protocol: gwv1beta1.ProtocolType(listenerOneProtocol),
					TLS: &gwv1beta1.GatewayTLSConfig{
						CertificateRefs: []gwv1beta1.SecretObjectReference{
							{
								Kind:      common.PointerTo(gwv1beta1.Kind("Secret")),
								Name:      gwv1beta1.ObjectName("one-cert"),
								Namespace: common.PointerTo(gwv1beta1.Namespace(namespace)),
							},
						},
					},
					AllowedRoutes: &gwv1beta1.AllowedRoutes{
						Namespaces: &gwv1beta1.RouteNamespaces{
							From: common.PointerTo(gwv1beta1.FromNamespaces("All")),
						},
					},
				},
				{
					Name:     gwv1beta1.SectionName(listenerTwoName),
					Hostname: common.PointerTo(gwv1beta1.Hostname(listenerTwoHostname)),
					Port:     gwv1beta1.PortNumber(listenerTwoPort),
					Protocol: gwv1beta1.ProtocolType(listenerTwoProtocol),
					AllowedRoutes: &gwv1beta1.AllowedRoutes{
						Namespaces: &gwv1beta1.RouteNamespaces{
							From: common.PointerTo(gwv1beta1.FromNamespaces("Same")),
						},
					},
				},
				{
					Name:     gwv1beta1.SectionName(listenerThreeName),
					Port:     gwv1beta1.PortNumber(listenerThreePort),
					Protocol: gwv1beta1.ProtocolType(listenerThreeProtocol),
					AllowedRoutes: &gwv1beta1.AllowedRoutes{
						Namespaces: &gwv1beta1.RouteNamespaces{
							From: common.PointerTo(gwv1beta1.FromNamespaces("All")),
						},
					},
				},
				{
					Name:     gwv1beta1.SectionName(listenerFourName),
					Hostname: common.PointerTo(gwv1beta1.Hostname(listenerFourHostname)),
					Port:     gwv1beta1.PortNumber(listenerFourPort),
					Protocol: gwv1beta1.ProtocolType(listenerFourProtocol),
					AllowedRoutes: &gwv1beta1.AllowedRoutes{
						Namespaces: &gwv1beta1.RouteNamespaces{
							From: common.PointerTo(gwv1beta1.FromNamespaces("Selector")),
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									common.NamespaceNameLabel: "consul",
								},
								MatchExpressions: []metav1.LabelSelectorRequirement{},
							},
						},
					},
				},
			},
		},
	}

	err := k8sClient.Create(ctx, gwClassCfg)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, gwClass)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, gw)
	require.NoError(t, err)

	return gw
}

func createJWTAuthHTTPRoute(t *testing.T, ctx context.Context, k8sClient client.WithWatch, gw *gwv1beta1.Gateway, authFilter *v1alpha1.RouteAuthFilter) *gwv1beta1.HTTPRoute {
	svcDefault := &v1alpha1.ServiceDefaults{
		TypeMeta: metav1.TypeMeta{
			Kind: "ServiceDefaults",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Service",
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind: "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "Service",
			Labels: map[string]string{"app": "Service"},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "high",
					Protocol: "TCP",
					Port:     8080,
				},
			},
			Selector: map[string]string{"app": "Service"},
		},
	}

	serviceAccount := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind: "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Service",
		},
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind: "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "Service",
			Labels: map[string]string{"app": "Service"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: common.PointerTo(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "Service"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       corev1.PodSpec{},
			},
		},
	}

	err := k8sClient.Create(ctx, svcDefault)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, svc)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, serviceAccount)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	route := &gwv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "http-route",
		},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{
						Kind:        (*gwv1beta1.Kind)(&gw.Kind),
						Namespace:   (*gwv1beta1.Namespace)(&gw.Namespace),
						Name:        gwv1beta1.ObjectName(gw.Name),
						SectionName: &gw.Spec.Listeners[0].Name,
						Port:        &gw.Spec.Listeners[0].Port,
					},
				},
			},
			Hostnames: []gwv1beta1.Hostname{"route.consul.io"},
			Rules: []gwv1beta1.HTTPRouteRule{
				{
					Matches: []gwv1beta1.HTTPRouteMatch{
						{
							Path: &gwv1beta1.HTTPPathMatch{
								Type:  common.PointerTo(gwv1beta1.PathMatchType("PathPrefix")),
								Value: common.PointerTo("/v1"),
							},
							Headers: []gwv1beta1.HTTPHeaderMatch{
								{
									Type:  common.PointerTo(gwv1beta1.HeaderMatchExact),
									Name:  "version",
									Value: "version",
								},
							},
							QueryParams: []gwv1beta1.HTTPQueryParamMatch{
								{
									Type:  common.PointerTo(gwv1beta1.QueryParamMatchExact),
									Name:  "search",
									Value: "q",
								},
							},
							Method: common.PointerTo(gwv1beta1.HTTPMethod("GET")),
						},
					},
					Filters: []gwv1beta1.HTTPRouteFilter{
						{
							Type: gwv1beta1.HTTPRouteFilterRequestHeaderModifier,
							RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
								Set: []gwv1beta1.HTTPHeader{
									{
										Name:  "foo",
										Value: "bax",
									},
								},
								Add: []gwv1beta1.HTTPHeader{
									{
										Name:  "arc",
										Value: "reactor",
									},
								},
								Remove: []string{"remove"},
							},
						},
						{
							Type: gwv1beta1.HTTPRouteFilterURLRewrite,
							URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
								Hostname: common.PointerTo(gwv1beta1.PreciseHostname("host.com")),
								Path: &gwv1beta1.HTTPPathModifier{
									Type:            gwv1beta1.FullPathHTTPPathModifier,
									ReplaceFullPath: common.PointerTo("/foobar"),
								},
							},
						},

						{
							Type: gwv1beta1.HTTPRouteFilterURLRewrite,
							URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
								Hostname: common.PointerTo(gwv1beta1.PreciseHostname("host.com")),
								Path: &gwv1beta1.HTTPPathModifier{
									Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
									ReplacePrefixMatch: common.PointerTo("/foo"),
								},
							},
						},
						{
							Type: gwv1beta1.HTTPRouteFilterExtensionRef,
							ExtensionRef: &gwv1beta1.LocalObjectReference{
								Group: gwv1beta1.Group(v1alpha1.ConsulHashicorpGroup),
								Kind:  v1alpha1.RouteAuthFilterKind,
								Name:  gwv1beta1.ObjectName(authFilter.Name),
							},
						},
					},
					BackendRefs: []gwv1beta1.HTTPBackendRef{
						{
							BackendRef: gwv1beta1.BackendRef{
								BackendObjectReference: gwv1beta1.BackendObjectReference{
									Name: "Service",
									Port: common.PointerTo(gwv1beta1.PortNumber(8080)),
								},
								Weight: common.PointerTo(int32(50)),
							},
						},
					},
				},
			},
		},
	}

	err = k8sClient.Create(ctx, route)
	require.NoError(t, err)

	return route
}

func createAllFieldsSetTCPRoute(t *testing.T, ctx context.Context, k8sClient client.WithWatch, gw *gwv1beta1.Gateway) *v1alpha2.TCPRoute {
	route := &v1alpha2.TCPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "TCPRoute",
			APIVersion: "gateway.networking.k8s.io/v1alpha2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "tcp-route",
		},
		Spec: gwv1alpha2.TCPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{
						Kind:        (*gwv1beta1.Kind)(&gw.Kind),
						Namespace:   (*gwv1beta1.Namespace)(&gw.Namespace),
						Name:        gwv1beta1.ObjectName(gw.Name),
						SectionName: &gw.Spec.Listeners[2].Name,
						Port:        &gw.Spec.Listeners[2].Port,
					},
				},
			},
			Rules: []gwv1alpha2.TCPRouteRule{
				{
					BackendRefs: []gwv1beta1.BackendRef{
						{
							BackendObjectReference: gwv1beta1.BackendObjectReference{
								Name: "Service",
								Port: common.PointerTo(gwv1beta1.PortNumber(25000)),
							},
							Weight: common.PointerTo(int32(50)),
						},
					},
				},
			},
		},
	}

	err := k8sClient.Create(ctx, route)
	require.NoError(t, err)

	return route
}

func createCert(t *testing.T, ctx context.Context, k8sClient client.WithWatch, certNS string) *corev1.Secret {
	// listener one tls config
	certName := "one-cert"

	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	usage := x509.KeyUsageCertSign
	expiration := time.Now().AddDate(10, 0, 0)

	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "consul.test",
		},
		IsCA:                  true,
		NotBefore:             time.Now().Add(-10 * time.Minute),
		NotAfter:              expiration,
		SubjectKeyId:          []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              usage,
		BasicConstraintsValid: true,
	}
	caCert := cert
	caPrivateKey := privateKey

	data, err := x509.CreateCertificate(rand.Reader, cert, caCert, &privateKey.PublicKey, caPrivateKey)
	require.NoError(t, err)

	certBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: data,
	})

	privateKeyBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: certNS,
			Name:      certName,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       certBytes,
			corev1.TLSPrivateKeyKey: privateKeyBytes,
		},
	}

	err = k8sClient.Create(ctx, secret)
	require.NoError(t, err)

	return secret
}

func minimalFieldsSetAPIGW(t *testing.T, ctx context.Context, k8sClient client.WithWatch, namespace string) *gwv1beta1.Gateway {
	// listener one configuration
	listenerOneName := "listener-one"
	listenerOneHostname := "*.consul.io"
	listenerOnePort := 3366
	listenerOneProtocol := "https"

	// listener three configuration
	listenerThreeName := "listener-three"
	listenerThreePort := 8081
	listenerThreeProtocol := "tcp"

	// Write gw to k8s
	gwClassCfg := &v1alpha1.GatewayClassConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GatewayClassConfig",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "gateway-class-config",
		},
		Spec: v1alpha1.GatewayClassConfigSpec{},
	}
	gwClass := &gwv1beta1.GatewayClass{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GatewayClass",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatewayclass",
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: "consul.hashicorp.com/gateway-controller",
			ParametersRef: &gwv1beta1.ParametersReference{
				Group: "consul.hashicorp.com",
				Kind:  "GatewayClassConfig",
				Name:  "gateway-class-config",
			},
			Description: new(string),
		},
	}
	gw := &gwv1beta1.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "gw",
			Annotations: make(map[string]string),
		},
		Spec: gwv1beta1.GatewaySpec{
			GatewayClassName: gwv1beta1.ObjectName(gwClass.Name),
			Listeners: []gwv1beta1.Listener{
				{
					Name:     gwv1beta1.SectionName(listenerOneName),
					Hostname: common.PointerTo(gwv1beta1.Hostname(listenerOneHostname)),
					Port:     gwv1beta1.PortNumber(listenerOnePort),
					Protocol: gwv1beta1.ProtocolType(listenerOneProtocol),
					TLS: &gwv1beta1.GatewayTLSConfig{
						CertificateRefs: []gwv1beta1.SecretObjectReference{
							{
								Kind:      common.PointerTo(gwv1beta1.Kind("Secret")),
								Name:      gwv1beta1.ObjectName("one-cert"),
								Namespace: common.PointerTo(gwv1beta1.Namespace(namespace)),
							},
						},
					},
				},
				{
					Name:     gwv1beta1.SectionName(listenerThreeName),
					Port:     gwv1beta1.PortNumber(listenerThreePort),
					Protocol: gwv1beta1.ProtocolType(listenerThreeProtocol),
					AllowedRoutes: &gwv1beta1.AllowedRoutes{
						Namespaces: &gwv1beta1.RouteNamespaces{
							From: common.PointerTo(gwv1beta1.FromNamespaces("All")),
						},
					},
				},
			},
		},
	}

	err := k8sClient.Create(ctx, gwClassCfg)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, gwClass)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, gw)
	require.NoError(t, err)

	return gw
}

func minimalFieldsSetHTTPRoute(t *testing.T, ctx context.Context, k8sClient client.WithWatch, gw *gwv1beta1.Gateway, _ *v1alpha1.RouteAuthFilter) *gwv1beta1.HTTPRoute {
	svcDefault := &v1alpha1.ServiceDefaults{
		TypeMeta: metav1.TypeMeta{
			Kind: "ServiceDefaults",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Service",
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind: "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "Service",
			Labels: map[string]string{"app": "Service"},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "high",
					Protocol: "TCP",
					Port:     8080,
				},
			},
			Selector: map[string]string{"app": "Service"},
		},
	}

	serviceAccount := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind: "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Service",
		},
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind: "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "Service",
			Labels: map[string]string{"app": "Service"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: common.PointerTo(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "Service"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       corev1.PodSpec{},
			},
		},
	}

	err := k8sClient.Create(ctx, svcDefault)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, svc)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, serviceAccount)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	route := &gwv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "http-route",
		},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{
						Kind:        (*gwv1beta1.Kind)(&gw.Kind),
						Namespace:   (*gwv1beta1.Namespace)(&gw.Namespace),
						Name:        gwv1beta1.ObjectName(gw.Name),
						SectionName: &gw.Spec.Listeners[0].Name,
						Port:        &gw.Spec.Listeners[0].Port,
					},
				},
			},
			Hostnames: []gwv1beta1.Hostname{"route.consul.io"},
			Rules: []gwv1beta1.HTTPRouteRule{
				{
					BackendRefs: []gwv1beta1.HTTPBackendRef{
						{
							BackendRef: gwv1beta1.BackendRef{
								BackendObjectReference: gwv1beta1.BackendObjectReference{
									Name: "Service",
									Port: common.PointerTo(gwv1beta1.PortNumber(8080)),
								},
							},
						},
					},
				},
			},
		},
	}

	err = k8sClient.Create(ctx, route)
	require.NoError(t, err)

	return route
}

func minimalFieldsSetTCPRoute(t *testing.T, ctx context.Context, k8sClient client.WithWatch, gw *gwv1beta1.Gateway) *v1alpha2.TCPRoute {
	route := &v1alpha2.TCPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "TCPRoute",
			APIVersion: "gateway.networking.k8s.io/v1alpha2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "tcp-route",
		},
		Spec: gwv1alpha2.TCPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{
						Kind:        (*gwv1beta1.Kind)(&gw.Kind),
						Namespace:   (*gwv1beta1.Namespace)(&gw.Namespace),
						Name:        gwv1beta1.ObjectName(gw.Name),
						SectionName: &gw.Spec.Listeners[1].Name,
						Port:        &gw.Spec.Listeners[1].Port,
					},
				},
			},
			Rules: []gwv1alpha2.TCPRouteRule{
				{
					BackendRefs: []gwv1beta1.BackendRef{
						{
							BackendObjectReference: gwv1beta1.BackendObjectReference{
								Name: "Service",
								Port: common.PointerTo(gwv1beta1.PortNumber(25000)),
							},
						},
					},
				},
			},
		},
	}

	err := k8sClient.Create(ctx, route)
	require.NoError(t, err)

	return route
}

func createFunkyCasingFieldsAPIGW(t *testing.T, ctx context.Context, k8sClient client.WithWatch, namespace string) *gwv1beta1.Gateway {
	// listener one configuration
	listenerOneName := "listener-one"
	listenerOneHostname := "*.consul.io"
	listenerOnePort := 3366
	listenerOneProtocol := "hTtPs"

	// listener two configuration
	listenerTwoName := "listener-two"
	listenerTwoHostname := "*.consul.io"
	listenerTwoPort := 5432
	listenerTwoProtocol := "HTTP"

	// listener three configuration
	listenerThreeName := "listener-three"
	listenerThreePort := 8081
	listenerThreeProtocol := "tCp"

	// listener four configuration
	listenerFourName := "listener-four"
	listenerFourHostname := "*.consul.io"
	listenerFourPort := 5433
	listenerFourProtocol := "hTTp"

	// Write gw to k8s
	gwClassCfg := &v1alpha1.GatewayClassConfig{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GatewayClassConfig",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "gateway-class-config",
		},
		Spec: v1alpha1.GatewayClassConfigSpec{},
	}
	gwClass := &gwv1beta1.GatewayClass{
		TypeMeta: metav1.TypeMeta{
			Kind:       "GatewayClass",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "gatewayclass",
		},
		Spec: gwv1beta1.GatewayClassSpec{
			ControllerName: "consul.hashicorp.com/gateway-controller",
			ParametersRef: &gwv1beta1.ParametersReference{
				Group: "consul.hashicorp.com",
				Kind:  "GatewayClassConfig",
				Name:  "gateway-class-config",
			},
			Description: new(string),
		},
	}
	gw := &gwv1beta1.Gateway{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Gateway",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "gw",
			Namespace:   namespace,
			Annotations: make(map[string]string),
		},
		Spec: gwv1beta1.GatewaySpec{
			GatewayClassName: gwv1beta1.ObjectName(gwClass.Name),
			Listeners: []gwv1beta1.Listener{
				{
					Name:     gwv1beta1.SectionName(listenerOneName),
					Hostname: common.PointerTo(gwv1beta1.Hostname(listenerOneHostname)),
					Port:     gwv1beta1.PortNumber(listenerOnePort),
					Protocol: gwv1beta1.ProtocolType(listenerOneProtocol),
					TLS: &gwv1beta1.GatewayTLSConfig{
						CertificateRefs: []gwv1beta1.SecretObjectReference{
							{
								Kind:      common.PointerTo(gwv1beta1.Kind("Secret")),
								Name:      gwv1beta1.ObjectName("one-cert"),
								Namespace: common.PointerTo(gwv1beta1.Namespace(namespace)),
							},
						},
					},
					AllowedRoutes: &gwv1beta1.AllowedRoutes{
						Namespaces: &gwv1beta1.RouteNamespaces{
							From: common.PointerTo(gwv1beta1.FromNamespaces("All")),
						},
					},
				},
				{
					Name:     gwv1beta1.SectionName(listenerTwoName),
					Hostname: common.PointerTo(gwv1beta1.Hostname(listenerTwoHostname)),
					Port:     gwv1beta1.PortNumber(listenerTwoPort),
					Protocol: gwv1beta1.ProtocolType(listenerTwoProtocol),
					AllowedRoutes: &gwv1beta1.AllowedRoutes{
						Namespaces: &gwv1beta1.RouteNamespaces{
							From: common.PointerTo(gwv1beta1.FromNamespaces("Same")),
						},
					},
				},
				{
					Name:     gwv1beta1.SectionName(listenerThreeName),
					Port:     gwv1beta1.PortNumber(listenerThreePort),
					Protocol: gwv1beta1.ProtocolType(listenerThreeProtocol),
					AllowedRoutes: &gwv1beta1.AllowedRoutes{
						Namespaces: &gwv1beta1.RouteNamespaces{
							From: common.PointerTo(gwv1beta1.FromNamespaces("All")),
						},
					},
				},
				{
					Name:     gwv1beta1.SectionName(listenerFourName),
					Hostname: common.PointerTo(gwv1beta1.Hostname(listenerFourHostname)),
					Port:     gwv1beta1.PortNumber(listenerFourPort),
					Protocol: gwv1beta1.ProtocolType(listenerFourProtocol),
					AllowedRoutes: &gwv1beta1.AllowedRoutes{
						Namespaces: &gwv1beta1.RouteNamespaces{
							From: common.PointerTo(gwv1beta1.FromNamespaces("Selector")),
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									common.NamespaceNameLabel: "consul",
								},
								MatchExpressions: []metav1.LabelSelectorRequirement{},
							},
						},
					},
				},
			},
		},
	}

	err := k8sClient.Create(ctx, gwClassCfg)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, gwClass)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, gw)
	require.NoError(t, err)

	return gw
}

func createFunkyCasingFieldsHTTPRoute(t *testing.T, ctx context.Context, k8sClient client.WithWatch, gw *gwv1beta1.Gateway, _ *v1alpha1.RouteAuthFilter) *gwv1beta1.HTTPRoute {
	svcDefault := &v1alpha1.ServiceDefaults{
		TypeMeta: metav1.TypeMeta{
			Kind: "ServiceDefaults",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Service",
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "hTtp",
		},
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind: "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "Service",
			Labels: map[string]string{"app": "Service"},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "high",
					Protocol: "TCP",
					Port:     8080,
				},
			},
			Selector: map[string]string{"app": "Service"},
		},
	}

	serviceAccount := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind: "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Service",
		},
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind: "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "Service",
			Labels: map[string]string{"app": "Service"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: common.PointerTo(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "Service"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       corev1.PodSpec{},
			},
		},
	}

	err := k8sClient.Create(ctx, svcDefault)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, svc)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, serviceAccount)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	route := &gwv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "http-route",
		},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{
						Namespace:   (*gwv1beta1.Namespace)(&gw.Namespace),
						Name:        gwv1beta1.ObjectName(gw.Name),
						SectionName: &gw.Spec.Listeners[0].Name,
						Port:        &gw.Spec.Listeners[0].Port,
					},
				},
			},
			Hostnames: []gwv1beta1.Hostname{"route.consul.io"},
			Rules: []gwv1beta1.HTTPRouteRule{
				{
					Matches: []gwv1beta1.HTTPRouteMatch{
						{
							Path: &gwv1beta1.HTTPPathMatch{
								Type: common.PointerTo(gwv1beta1.PathMatchPathPrefix),
							},
							Headers: []gwv1beta1.HTTPHeaderMatch{
								{
									Type:  common.PointerTo(gwv1beta1.HeaderMatchExact),
									Name:  "version",
									Value: "version",
								},
							},
							QueryParams: []gwv1beta1.HTTPQueryParamMatch{
								{
									Type:  common.PointerTo(gwv1beta1.QueryParamMatchExact),
									Name:  "search",
									Value: "q",
								},
							},
							Method: common.PointerTo(gwv1beta1.HTTPMethod("geT")),
						},
					},
					Filters: []gwv1beta1.HTTPRouteFilter{
						{
							Type: gwv1beta1.HTTPRouteFilterRequestHeaderModifier,
							RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
								Set: []gwv1beta1.HTTPHeader{
									{
										Name:  "foo",
										Value: "bax",
									},
								},
								Add: []gwv1beta1.HTTPHeader{
									{
										Name:  "arc",
										Value: "reactor",
									},
								},
								Remove: []string{"remove"},
							},
						},
						{
							Type: gwv1beta1.HTTPRouteFilterURLRewrite,
							URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
								Hostname: common.PointerTo(gwv1beta1.PreciseHostname("host.com")),
								Path: &gwv1beta1.HTTPPathModifier{
									Type:            gwv1beta1.FullPathHTTPPathModifier,
									ReplaceFullPath: common.PointerTo("/foobar"),
								},
							},
						},

						{
							Type: gwv1beta1.HTTPRouteFilterURLRewrite,
							URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
								Hostname: common.PointerTo(gwv1beta1.PreciseHostname("host.com")),
								Path: &gwv1beta1.HTTPPathModifier{
									Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
									ReplacePrefixMatch: common.PointerTo("/foo"),
								},
							},
						},
					},
					BackendRefs: []gwv1beta1.HTTPBackendRef{
						{
							BackendRef: gwv1beta1.BackendRef{
								BackendObjectReference: gwv1beta1.BackendObjectReference{
									Name: "Service",
									Port: common.PointerTo(gwv1beta1.PortNumber(8080)),
								},
								Weight: common.PointerTo(int32(-50)),
							},
						},
					},
				},
			},
		},
	}

	err = k8sClient.Create(ctx, route)
	require.NoError(t, err)

	return route
}

func createFunkyCasingFieldsTCPRoute(t *testing.T, ctx context.Context, k8sClient client.WithWatch, gw *gwv1beta1.Gateway) *v1alpha2.TCPRoute {
	route := &v1alpha2.TCPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "TCPRoute",
			APIVersion: "gateway.networking.k8s.io/v1alpha2",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "tcp-route",
		},
		Spec: gwv1alpha2.TCPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{
						Namespace:   (*gwv1beta1.Namespace)(&gw.Namespace),
						Name:        gwv1beta1.ObjectName(gw.Name),
						SectionName: &gw.Spec.Listeners[2].Name,
						Port:        &gw.Spec.Listeners[2].Port,
					},
				},
			},
			Rules: []gwv1alpha2.TCPRouteRule{
				{
					BackendRefs: []gwv1beta1.BackendRef{
						{
							BackendObjectReference: gwv1beta1.BackendObjectReference{
								Name: "Service",
								Port: common.PointerTo(gwv1beta1.PortNumber(25000)),
							},
							Weight: common.PointerTo(int32(-50)),
						},
					},
				},
			},
		},
	}

	err := k8sClient.Create(ctx, route)
	require.NoError(t, err)

	return route
}

func createAllFieldsSetHTTPRoute(t *testing.T, ctx context.Context, k8sClient client.WithWatch, gw *gwv1beta1.Gateway, filter *v1alpha1.RouteAuthFilter) *gwv1beta1.HTTPRoute {
	svcDefault := &v1alpha1.ServiceDefaults{
		TypeMeta: metav1.TypeMeta{
			Kind: "ServiceDefaults",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Service",
		},
		Spec: v1alpha1.ServiceDefaultsSpec{
			Protocol: "http",
		},
	}

	svc := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind: "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "Service",
			Labels: map[string]string{"app": "Service"},
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name:     "high",
					Protocol: "TCP",
					Port:     8080,
				},
			},
			Selector: map[string]string{"app": "Service"},
		},
	}

	serviceAccount := &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind: "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "Service",
		},
	}

	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind: "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   "Service",
			Labels: map[string]string{"app": "Service"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: common.PointerTo(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "Service"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec:       corev1.PodSpec{},
			},
		},
	}

	err := k8sClient.Create(ctx, svcDefault)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, svc)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, serviceAccount)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, deployment)
	require.NoError(t, err)

	route := &gwv1beta1.HTTPRoute{
		TypeMeta: metav1.TypeMeta{
			Kind:       "HTTPRoute",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "http-route",
		},
		Spec: gwv1beta1.HTTPRouteSpec{
			CommonRouteSpec: gwv1beta1.CommonRouteSpec{
				ParentRefs: []gwv1beta1.ParentReference{
					{
						Kind:        (*gwv1beta1.Kind)(&gw.Kind),
						Namespace:   (*gwv1beta1.Namespace)(&gw.Namespace),
						Name:        gwv1beta1.ObjectName(gw.Name),
						SectionName: &gw.Spec.Listeners[0].Name,
						Port:        &gw.Spec.Listeners[0].Port,
					},
				},
			},
			Hostnames: []gwv1beta1.Hostname{"route.consul.io"},
			Rules: []gwv1beta1.HTTPRouteRule{
				{
					Matches: []gwv1beta1.HTTPRouteMatch{
						{
							Path: &gwv1beta1.HTTPPathMatch{
								Type:  common.PointerTo(gwv1beta1.PathMatchType("PathPrefix")),
								Value: common.PointerTo("/v1"),
							},
							Headers: []gwv1beta1.HTTPHeaderMatch{
								{
									Type:  common.PointerTo(gwv1beta1.HeaderMatchExact),
									Name:  "version",
									Value: "version",
								},
							},
							QueryParams: []gwv1beta1.HTTPQueryParamMatch{
								{
									Type:  common.PointerTo(gwv1beta1.QueryParamMatchExact),
									Name:  "search",
									Value: "q",
								},
							},
							Method: common.PointerTo(gwv1beta1.HTTPMethod("GET")),
						},
					},
					Filters: []gwv1beta1.HTTPRouteFilter{
						{
							Type: gwv1beta1.HTTPRouteFilterRequestHeaderModifier,
							RequestHeaderModifier: &gwv1beta1.HTTPHeaderFilter{
								Set: []gwv1beta1.HTTPHeader{
									{
										Name:  "foo",
										Value: "bax",
									},
								},
								Add: []gwv1beta1.HTTPHeader{
									{
										Name:  "arc",
										Value: "reactor",
									},
								},
								Remove: []string{"remove"},
							},
						},
						{
							Type: gwv1beta1.HTTPRouteFilterURLRewrite,
							URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
								Hostname: common.PointerTo(gwv1beta1.PreciseHostname("host.com")),
								Path: &gwv1beta1.HTTPPathModifier{
									Type:            gwv1beta1.FullPathHTTPPathModifier,
									ReplaceFullPath: common.PointerTo("/foobar"),
								},
							},
						},

						{
							Type: gwv1beta1.HTTPRouteFilterURLRewrite,
							URLRewrite: &gwv1beta1.HTTPURLRewriteFilter{
								Hostname: common.PointerTo(gwv1beta1.PreciseHostname("host.com")),
								Path: &gwv1beta1.HTTPPathModifier{
									Type:               gwv1beta1.PrefixMatchHTTPPathModifier,
									ReplacePrefixMatch: common.PointerTo("/foo"),
								},
							},
						},
					},
					BackendRefs: []gwv1beta1.HTTPBackendRef{
						{
							BackendRef: gwv1beta1.BackendRef{
								BackendObjectReference: gwv1beta1.BackendObjectReference{
									Name: "Service",
									Port: common.PointerTo(gwv1beta1.PortNumber(8080)),
								},
								Weight: common.PointerTo(int32(50)),
							},
						},
					},
				},
			},
		},
	}

	err = k8sClient.Create(ctx, route)
	require.NoError(t, err)

	return route
}

func createRouteAuthFilter(t *testing.T, ctx context.Context, k8sClient client.WithWatch, providerName string) *v1alpha1.RouteAuthFilter {
	filter := &v1alpha1.RouteAuthFilter{
		TypeMeta: metav1.TypeMeta{
			Kind: v1alpha1.RouteAuthFilterKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "auth-filter",
		},
		Spec: v1alpha1.RouteAuthFilterSpec{
			JWT: &v1alpha1.GatewayJWTRequirement{
				Providers: []*v1alpha1.GatewayJWTProvider{
					{
						Name: providerName,
					},
				},
			},
		},
	}
	err := k8sClient.Create(ctx, filter)
	require.NoError(t, err)

	return filter
}

func createJWTProvider(t *testing.T, ctx context.Context, k8sClient client.WithWatch) *v1alpha1.JWTProvider {
	provider := &v1alpha1.JWTProvider{
		TypeMeta: metav1.TypeMeta{
			Kind: v1alpha1.JWTProviderKubeKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "provider",
		},
		Spec: v1alpha1.JWTProviderSpec{
			JSONWebKeySet: &v1alpha1.JSONWebKeySet{},
			Issuer:        "local",
		},
	}

	err := k8sClient.Create(ctx, provider)
	require.NoError(t, err)

	return provider
}

func createGWPolicy(t *testing.T, ctx context.Context, k8sClient client.WithWatch, gw *gwv1beta1.Gateway, providerName string) {
	policy := &v1alpha1.GatewayPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind: "GatewayPolicy",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "gw-policy",
		},
		Spec: v1alpha1.GatewayPolicySpec{
			TargetRef: v1alpha1.PolicyTargetReference{
				Group:       gw.GroupVersionKind().Group,
				Kind:        gw.GroupVersionKind().Kind,
				Name:        gw.Name,
				Namespace:   gw.Namespace,
				SectionName: &gw.Spec.Listeners[0].Name,
			},
			Override: &v1alpha1.GatewayPolicyConfig{
				JWT: &v1alpha1.GatewayJWTRequirement{
					Providers: []*v1alpha1.GatewayJWTProvider{
						{
							Name: providerName,
						},
					},
				},
			},
			Default: &v1alpha1.GatewayPolicyConfig{
				JWT: &v1alpha1.GatewayJWTRequirement{
					Providers: []*v1alpha1.GatewayJWTProvider{
						{
							Name: providerName,
						},
					},
				},
			},
		},
	}

	err := k8sClient.Create(ctx, policy)
	require.NoError(t, err)
}
