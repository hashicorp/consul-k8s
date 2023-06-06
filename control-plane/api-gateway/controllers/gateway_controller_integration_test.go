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

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/cache"
	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul/api"
)

func TestControllerDoesNotInfinitelyReconcile(t *testing.T) {
	s := runtime.NewScheme()
	require.NoError(t, clientgoscheme.AddToScheme(s))
	require.NoError(t, gwv1alpha2.Install(s))
	require.NoError(t, gwv1beta1.Install(s))
	require.NoError(t, v1alpha1.AddToScheme(s))

	k8sClient := registerFieldIndexersForTest(fake.NewClientBuilder().WithScheme(s)).Build()
	consulTestServerClient := test.TestServerWithMockConnMgrWatcher(t, nil)

	ctx := context.Background()
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
	_ = resourceCache.Subscribe(ctx, api.TCPRoute, gwCtrl.transformConsulTCPRoute(ctx))
	_ = resourceCache.Subscribe(ctx, api.InlineCertificate, gwCtrl.transformConsulInlineCertificate(ctx))

	k8sGWObj := createAllFieldsSetAPIGW(t, ctx, k8sClient)
	httpRouteObj := createAllFieldsSetHTTPRoute(t, ctx, k8sClient, k8sGWObj)

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

	// reconcile again so that we get the route bound to the gateway
	_, err = gwCtrl.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: k8sGWObj.Namespace,
			Name:      k8sGWObj.Name,
		},
	})
	require.NoError(t, err)

	// get the creation events from the upsert
	<-gwSub.Events()
	<-httpRouteSub.Events()

	gwNamespaceName := types.NamespacedName{
		Name:      k8sGWObj.Name,
		Namespace: k8sGWObj.Namespace,
	}

	httpRouteNamespaceName := types.NamespacedName{
		Name:      httpRouteObj.Name,
		Namespace: httpRouteObj.Namespace,
	}

	gwRef := gwCtrl.Translator.ConfigEntryReference(api.APIGateway, gwNamespaceName)
	httpRouteRef := gwCtrl.Translator.ConfigEntryReference(api.HTTPRoute, httpRouteNamespaceName)
	curGWModifyIndex := resourceCache.Get(gwRef).GetModifyIndex()
	curHTTPRouteModifyIndex := resourceCache.Get(httpRouteRef).GetModifyIndex()

	err = k8sClient.Get(ctx, gwNamespaceName, k8sGWObj)
	require.NoError(t, err)
	curGWResourceVersion := k8sGWObj.ResourceVersion

	err = k8sClient.Get(ctx, httpRouteNamespaceName, httpRouteObj)
	require.NoError(t, err)
	curHTTPRouteResourceVersion := httpRouteObj.ResourceVersion

	go func() {
		// reconcile gateway again without any changes
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

		return curGWModifyIndex == resourceCache.Get(gwRef).GetModifyIndex() &&
			curGWResourceVersion == newGWResourceVersion &&
			curHTTPRouteModifyIndex == resourceCache.Get(httpRouteRef).GetModifyIndex() &&
			curHTTPRouteResourceVersion == newHTTPRouteResourceVersion
	}, time.Duration(2*time.Second), time.Duration(500*time.Millisecond), fmt.Sprintf("curGWModifyIndex: %d, newIndx: %d", curGWModifyIndex, resourceCache.Get(gwRef).GetModifyIndex()),
	)
}

func createAllFieldsSetAPIGW(t *testing.T, ctx context.Context, k8sClient client.WithWatch) *gwv1beta1.Gateway {
	// listener one configuration
	listenerOneName := "listener-one"
	listenerOneHostname := "*.consul.io"
	listenerOnePort := 3366
	listenerOneProtocol := "https"

	// listener one tls config
	listenerOneCertName := "one-cert"
	listenerOneCertK8sNamespace := ""
	cert := generateTestCertificate(t, listenerOneCertK8sNamespace, listenerOneCertName)

	err := k8sClient.Create(ctx, &cert)
	require.NoError(t, err)

	// listener two configuration
	listenerTwoName := "listener-two"
	listenerTwoHostname := "*.consul.io"
	listenerTwoPort := 5432
	listenerTwoProtocol := "http"

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
			ControllerName: "hashicorp.com/consul-api-gateway-controller",
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
			Namespace:   "consul",
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
								Name:      gwv1beta1.ObjectName(listenerOneCertName),
								Namespace: common.PointerTo(gwv1beta1.Namespace(listenerOneCertK8sNamespace)),
							},
						},
					},
					AllowedRoutes: &gwv1beta1.AllowedRoutes{
						Namespaces: &gwv1beta1.RouteNamespaces{
							// TODO: do we support "selector" here?
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
							// TODO: do we support "selector" here?
							From: common.PointerTo(gwv1beta1.FromNamespaces("Same")),
						},
					},
				},
			},
		},
	}

	err = k8sClient.Create(ctx, gwClassCfg)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, gwClass)
	require.NoError(t, err)

	err = k8sClient.Create(ctx, gw)
	require.NoError(t, err)

	return gw
}

func createAllFieldsSetHTTPRoute(t *testing.T, ctx context.Context, k8sClient client.WithWatch, gw *gwv1beta1.Gateway) *gwv1beta1.HTTPRoute {
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

	refGrant := gwv1beta1.ReferenceGrant{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ReferenceGrant",
			APIVersion: "gateway.networking.k8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "grant",
			Namespace: "consul",
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{
				{
					Group: "gateway.networking.k8s.io",
					Kind:  "HTTPRoute",
				},
			},
			To: []gwv1beta1.ReferenceGrantTo{
				{
					Group: "gateway.networking.k8s.io",
					Kind:  "Gateway",
				},
			},
		},
	}

	err = k8sClient.Create(ctx, &refGrant)
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

func createAllFieldsSetTCPRoute(t *testing.T, ctx context.Context, k8sClient client.WithWatch, gw *gwv1beta1.Gateway) *v1alpha2.TCPRoute {
	return &v1alpha2.TCPRoute{}
}

func generateTestCertificate(t *testing.T, namespace, name string) corev1.Secret {
	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
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

	return corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       certBytes,
			corev1.TLSPrivateKeyKey: privateKeyBytes,
		},
	}
}
