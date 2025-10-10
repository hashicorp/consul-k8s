// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"math/big"
	"path"
	"strconv"
	"testing"
	"time"

	terratestk8s "github.com/gruntwork-io/terratest/modules/k8s"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil/retry"
)

var (
	gatewayGroup    = gwv1beta1.Group(gwv1beta1.GroupVersion.Group)
	consulGroup     = gwv1beta1.Group(v1alpha1.GroupVersion.Group)
	gatewayKind     = gwv1beta1.Kind("Gateway")
	serviceKind     = gwv1beta1.Kind("Service")
	secretKind      = gwv1beta1.Kind("Secret")
	meshServiceKind = gwv1beta1.Kind("MeshService")
	httpRouteKind   = gwv1beta1.Kind("HTTPRoute")
	tcpRouteKind    = gwv1beta1.Kind("TCPRoute")
)

func TestAPIGateway_Tenancy(t *testing.T) {
	cases := []struct {
		secure             bool
		namespaceMirroring bool
	}{
		{
			secure:             false,
			namespaceMirroring: false,
		},
		{
			secure:             true,
			namespaceMirroring: false,
		},
		{
			secure:             false,
			namespaceMirroring: true,
		},
		{
			secure:             true,
			namespaceMirroring: true,
		},
	}
	for _, c := range cases {
		name := fmt.Sprintf("secure: %t, namespaces: %t", c.secure, c.namespaceMirroring)
		t.Run(name, func(t *testing.T) {
			cfg := suite.Config()

			if !cfg.EnableEnterprise && c.namespaceMirroring {
				t.Skipf("skipping this test because -enable-enterprise is not set")
			}

			ctx := suite.Environment().DefaultContext(t)

			helmValues := map[string]string{
				"global.enableConsulNamespaces":               strconv.FormatBool(c.namespaceMirroring),
				"global.acls.manageSystemACLs":                strconv.FormatBool(c.secure),
				"global.tls.enabled":                          strconv.FormatBool(c.secure),
				"global.logLevel":                             "trace",
				"connectInject.enabled":                       "true",
				"connectInject.consulNamespaces.mirroringK8S": strconv.FormatBool(c.namespaceMirroring),
			}

			releaseName := helpers.RandomName()
			consulCluster := consul.NewHelmCluster(t, helmValues, ctx, cfg, releaseName)

			consulCluster.Create(t)

			serviceNamespace, serviceK8SOptions := createNamespace(t, ctx, cfg)
			certificateNamespace, certificateK8SOptions := createNamespace(t, ctx, cfg)
			gatewayNamespace, gatewayK8SOptions := createNamespace(t, ctx, cfg)
			routeNamespace, routeK8SOptions := createNamespace(t, ctx, cfg)

			logger.Logf(t, "creating target server in %s namespace", serviceNamespace)
			k8s.DeployKustomize(t, serviceK8SOptions, cfg.NoCleanupOnFailure, cfg.NoCleanup, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			logger.Logf(t, "creating certificate resources in %s namespace", certificateNamespace)
			applyFixture(t, cfg, certificateK8SOptions, "cases/api-gateways/certificate")

			logger.Logf(t, "creating gateway in %s namespace", gatewayNamespace)
			applyFixture(t, cfg, gatewayK8SOptions, "cases/api-gateways/gateway")

			logger.Logf(t, "creating route resources in %s namespace", routeNamespace)
			applyFixture(t, cfg, routeK8SOptions, "cases/api-gateways/httproute")

			// patch certificate with data
			logger.Log(t, "patching certificate with generated data")
			certificate := generateCertificate(t, nil, "gateway.test.local")
			k8s.RunKubectl(t, certificateK8SOptions, "patch", "secret", "certificate", "-p", fmt.Sprintf(`{"data":{"tls.crt":"%s","tls.key":"%s"}}`, base64.StdEncoding.EncodeToString(certificate.CertPEM), base64.StdEncoding.EncodeToString(certificate.PrivateKeyPEM)), "--type=merge")

			// patch the resources to reference each other
			logger.Log(t, "patching gateway to certificate")
			k8s.RunKubectl(t, gatewayK8SOptions, "patch", "gateway", "gateway", "-p", fmt.Sprintf(`{"spec":{"listeners":[{"protocol":"HTTPS","port":8082,"name":"https","tls":{"certificateRefs":[{"name":"certificate","namespace":"%s"}]},"allowedRoutes":{"namespaces":{"from":"All"}}}]}}`, certificateNamespace), "--type=merge")

			logger.Log(t, "patching route to target server")
			k8s.RunKubectl(t, routeK8SOptions, "patch", "httproute", "route", "-p", fmt.Sprintf(`{"spec":{"rules":[{"backendRefs":[{"name":"static-server","namespace":"%s","port":80}]}]}}`, serviceNamespace), "--type=merge")

			logger.Log(t, "patching route to gateway")
			k8s.RunKubectl(t, routeK8SOptions, "patch", "httproute", "route", "-p", fmt.Sprintf(`{"spec":{"parentRefs":[{"name":"gateway","namespace":"%s"}]}}`, gatewayNamespace), "--type=merge")

			// Grab a kubernetes and consul client so that we can verify binding
			// behavior prior to issuing requests through the gateway.
			k8sClient := ctx.ControllerRuntimeClient(t)
			consulClient, _ := consulCluster.SetupConsulClient(t, c.secure)

			retryCheck(t, 200, func(r *retry.R) {
				var gateway gwv1beta1.Gateway
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: gatewayNamespace}, &gateway)
				require.NoError(r, err)

				// check our statuses
				checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Conditions, falseCondition("Programmed", "Pending"))
				// we expect a sync error here since dropping the listener means the gateway is now invalid
				checkStatusCondition(r, gateway.Status.Conditions, falseCondition("Synced", "SyncError"))

				require.Len(r, gateway.Status.Listeners, 1)
				require.EqualValues(r, 1, gateway.Status.Listeners[0].AttachedRoutes)
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, falseCondition("Conflicted", "NoConflicts"))
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, falseCondition("ResolvedRefs", "RefNotPermitted"))
			})

			// since the sync operation should fail above, check that we don't have the entry in Consul.
			checkConsulNotExists(t, consulClient, api.APIGateway, "gateway", namespaceForConsul(c.namespaceMirroring, gatewayNamespace))

			// route failure
			retryCheck(t, 60, func(r *retry.R) {
				var httproute gwv1beta1.HTTPRoute
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "route", Namespace: routeNamespace}, &httproute)
				require.NoError(r, err)

				require.Len(r, httproute.Status.Parents, 1)
				require.EqualValues(r, gatewayClassControllerName, httproute.Status.Parents[0].ControllerName)
				require.EqualValues(r, "gateway", httproute.Status.Parents[0].ParentRef.Name)
				require.NotNil(r, httproute.Status.Parents[0].ParentRef.Namespace)
				require.EqualValues(r, gatewayNamespace, *httproute.Status.Parents[0].ParentRef.Namespace)
				checkStatusCondition(r, httproute.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, httproute.Status.Parents[0].Conditions, falseCondition("ResolvedRefs", "RefNotPermitted"))
			})

			// we only sync validly referenced certificates over, so check to make sure it is not created.
			checkConsulNotExists(t, consulClient, api.FileSystemCertificate, "certificate", namespaceForConsul(c.namespaceMirroring, certificateNamespace))

			// now create reference grants
			createReferenceGrant(t, k8sClient, "gateway-certificate", gatewayNamespace, certificateNamespace)
			createReferenceGrant(t, k8sClient, "route-service", routeNamespace, serviceNamespace)

			// gateway updated with references allowed
			retryCheck(t, 60, func(r *retry.R) {
				var gateway gwv1beta1.Gateway
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "gateway", Namespace: gatewayNamespace}, &gateway)
				require.NoError(r, err)

				// check our statuses
				checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Programmed", "Programmed"))
				checkStatusCondition(r, gateway.Status.Conditions, trueCondition("Synced", "Synced"))
				require.Len(r, gateway.Status.Listeners, 1)
				require.EqualValues(r, 1, gateway.Status.Listeners[0].AttachedRoutes)
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, falseCondition("Conflicted", "NoConflicts"))
				checkStatusCondition(r, gateway.Status.Listeners[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
			})

			// check the Consul gateway is updated, with the listener.
			retryCheck(t, 30, func(r *retry.R) {
				entry, _, err := consulClient.ConfigEntries().Get(api.APIGateway, "gateway", &api.QueryOptions{
					Namespace: namespaceForConsul(c.namespaceMirroring, gatewayNamespace),
				})
				require.NoError(r, err)
				gateway := entry.(*api.APIGatewayConfigEntry)

				require.EqualValues(r, "gateway", gateway.Meta["k8s-name"])
				require.EqualValues(r, gatewayNamespace, gateway.Meta["k8s-namespace"])
				require.Len(r, gateway.Listeners, 1)
				checkConsulStatusCondition(t, gateway.Status.Conditions, trueConsulCondition("Accepted", "Accepted"))
				checkConsulStatusCondition(t, gateway.Status.Conditions, trueConsulCondition("ResolvedRefs", "ResolvedRefs"))
			})

			// route updated with gateway and services allowed
			retryCheck(t, 30, func(r *retry.R) {
				var httproute gwv1beta1.HTTPRoute
				err := k8sClient.Get(context.Background(), types.NamespacedName{Name: "route", Namespace: routeNamespace}, &httproute)
				require.NoError(r, err)

				require.Len(r, httproute.Status.Parents, 1)
				require.EqualValues(r, gatewayClassControllerName, httproute.Status.Parents[0].ControllerName)
				require.EqualValues(r, "gateway", httproute.Status.Parents[0].ParentRef.Name)
				require.NotNil(r, httproute.Status.Parents[0].ParentRef.Namespace)
				require.EqualValues(r, gatewayNamespace, *httproute.Status.Parents[0].ParentRef.Namespace)
				checkStatusCondition(r, httproute.Status.Parents[0].Conditions, trueCondition("Accepted", "Accepted"))
				checkStatusCondition(r, httproute.Status.Parents[0].Conditions, trueCondition("ResolvedRefs", "ResolvedRefs"))
			})

			// now check to make sure that the route is updated and valid
			retryCheck(t, 30, func(r *retry.R) {
				// since we're not bound, check to make sure that the route doesn't target the gateway in Consul.
				entry, _, err := consulClient.ConfigEntries().Get(api.HTTPRoute, "route", &api.QueryOptions{
					Namespace: namespaceForConsul(c.namespaceMirroring, routeNamespace),
				})
				require.NoError(r, err)
				route := entry.(*api.HTTPRouteConfigEntry)

				require.EqualValues(r, "route", route.Meta["k8s-name"])
				require.EqualValues(r, routeNamespace, route.Meta["k8s-namespace"])
				require.Len(r, route.Parents, 1)
			})

			// and check to make sure that the certificate exists
			retryCheck(t, 30, func(r *retry.R) {
				entry, _, err := consulClient.ConfigEntries().Get(api.FileSystemCertificate, "certificate", &api.QueryOptions{
					Namespace: namespaceForConsul(c.namespaceMirroring, certificateNamespace),
				})
				require.NoError(r, err)
				certificate := entry.(*api.FileSystemCertificateConfigEntry)

				require.EqualValues(r, "certificate", certificate.Meta["k8s-name"])
				require.EqualValues(r, certificateNamespace, certificate.Meta["k8s-namespace"])
			})
		})
	}
}

func applyFixture(t *testing.T, cfg *config.TestConfig, k8sOptions *terratestk8s.KubectlOptions, fixture string) {
	t.Helper()

	out, err := k8s.RunKubectlAndGetOutputE(t, k8sOptions, "apply", "-k", path.Join("../fixtures", fixture))
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectlAndGetOutputE(t, k8sOptions, "delete", "-k", path.Join("../fixtures", fixture))
	})
}

func createNamespace(t *testing.T, ctx environment.TestContext, cfg *config.TestConfig) (string, *terratestk8s.KubectlOptions) {
	t.Helper()

	namespace := helpers.RandomName()

	logger.Logf(t, "creating Kubernetes namespace %s", namespace)
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", namespace)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, cfg.NoCleanup, func() {
		k8s.RunKubectl(t, ctx.KubectlOptions(t), "delete", "ns", namespace)
	})

	return namespace, &terratestk8s.KubectlOptions{
		ContextName: ctx.KubectlOptions(t).ContextName,
		ConfigPath:  ctx.KubectlOptions(t).ConfigPath,
		Namespace:   namespace,
	}
}

type certificateInfo struct {
	Cert          *x509.Certificate
	PrivateKey    *rsa.PrivateKey
	CertPEM       []byte
	PrivateKeyPEM []byte
}

func generateCertificate(t *testing.T, ca *certificateInfo, commonName string) *certificateInfo {
	t.Helper()

	bits := 2048
	privateKey, err := rsa.GenerateKey(rand.Reader, bits)
	require.NoError(t, err)

	usage := x509.KeyUsageDigitalSignature
	if ca == nil {
		usage = x509.KeyUsageCertSign
	}

	expiration := time.Now().AddDate(10, 0, 0)
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Testing, INC."},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{"Fake Street"},
			PostalCode:    []string{"11111"},
			CommonName:    commonName,
		},
		IsCA:                  ca == nil,
		NotBefore:             time.Now().Add(-10 * time.Minute),
		NotAfter:              expiration,
		SubjectKeyId:          []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:              usage,
		BasicConstraintsValid: true,
	}
	caCert := cert
	if ca != nil {
		caCert = ca.Cert
	}
	caPrivateKey := privateKey
	if ca != nil {
		caPrivateKey = ca.PrivateKey
	}
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

	return &certificateInfo{
		Cert:          cert,
		CertPEM:       certBytes,
		PrivateKey:    privateKey,
		PrivateKeyPEM: privateKeyBytes,
	}
}

func retryCheck(t *testing.T, count int, fn func(r *retry.R)) {
	retryCheckWithWait(t, count, 2*time.Second, fn)
}

func retryCheckWithWait(t *testing.T, count int, wait time.Duration, fn func(r *retry.R)) {
	t.Helper()

	counter := &retry.Counter{Count: count, Wait: wait}
	retry.RunWith(counter, t, fn)
}

func createReferenceGrant(t *testing.T, client client.Client, name, from, to string) {
	t.Helper()

	// we just create a reference grant for all combinations in the given namespaces

	require.NoError(t, client.Create(context.Background(), &gwv1beta1.ReferenceGrant{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: to,
		},
		Spec: gwv1beta1.ReferenceGrantSpec{
			From: []gwv1beta1.ReferenceGrantFrom{{
				Group:     gatewayGroup,
				Kind:      gatewayKind,
				Namespace: gwv1beta1.Namespace(from),
			}, {
				Group:     gatewayGroup,
				Kind:      httpRouteKind,
				Namespace: gwv1beta1.Namespace(from),
			}, {
				Group:     gatewayGroup,
				Kind:      tcpRouteKind,
				Namespace: gwv1beta1.Namespace(from),
			}},
			To: []gwv1beta1.ReferenceGrantTo{{
				Group: gatewayGroup,
				Kind:  gatewayKind,
			}, {
				Kind: serviceKind,
			}, {
				Group: consulGroup,
				Kind:  meshServiceKind,
			}, {
				Kind: secretKind,
			}},
		},
	}))
}

func namespaceForConsul(namespaceMirroringEnabled bool, namespace string) string {
	if namespaceMirroringEnabled {
		return namespace
	}
	return ""
}
