// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigateway

import (
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
	"github.com/hashicorp/consul-k8s/acceptance/framework/config"
	"github.com/hashicorp/consul-k8s/acceptance/framework/consul"
	"github.com/hashicorp/consul-k8s/acceptance/framework/environment"
	"github.com/hashicorp/consul-k8s/acceptance/framework/helpers"
	"github.com/hashicorp/consul-k8s/acceptance/framework/k8s"
	"github.com/hashicorp/consul-k8s/acceptance/framework/logger"
	"github.com/stretchr/testify/require"
)

func TestAPIGateway_Tenancy(t *testing.T) {
	cases := []struct {
		secure             bool
		namespaceMirroring bool
	}{
		// {
		// 	secure:             false,
		// 	namespaceMirroring: false,
		// },
		// {
		// 	secure:             true,
		// 	namespaceMirroring: false,
		// },
		{
			secure:             false,
			namespaceMirroring: true,
		},
		// {
		// 	secure:             true,
		// 	namespaceMirroring: true,
		// },
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
			k8s.DeployKustomize(t, serviceK8SOptions, cfg.NoCleanupOnFailure, cfg.DebugDirectory, "../fixtures/cases/static-server-inject")

			logger.Logf(t, "creating certificate resources in %s namespace", certificateNamespace)
			applyFixture(t, cfg, certificateK8SOptions, "cases/api-gateways/certificate")

			logger.Logf(t, "creating gateway in %s namespace", gatewayNamespace)
			applyFixture(t, cfg, gatewayK8SOptions, "bases/api-gateway")

			logger.Logf(t, "creating route resources in %s namespace", routeNamespace)
			applyFixture(t, cfg, routeK8SOptions, "cases/api-gateways/httproute")

			// patch certificate with data
			logger.Log(t, "patching certificate with generated data")
			certificate := generateCertificate(t, nil, "gateway.test.local")
			k8s.RunKubectl(t, certificateK8SOptions, "patch", "secret", "certificate", "-p", fmt.Sprintf(`{"data":{"tls.crt":"%s","tls.key":"%s"}}`, base64.StdEncoding.EncodeToString(certificate.CertPEM), base64.StdEncoding.EncodeToString(certificate.PrivateKeyPEM)), "--type=merge")

			// patch the resources to reference each other
			logger.Log(t, "patching gateway to certificate")
			k8s.RunKubectl(t, gatewayK8SOptions, "patch", "gateway", "gateway", "-p", fmt.Sprintf(`{"spec":{"gatewayClassName":"gateway-class","listeners":[{"protocol":"HTTP","port":8082,"name":"https","tls":{"certificateRefs":[{"name":"certificate","namespace":"%s"}]}}]}}`, certificateNamespace), "--type=merge")

			logger.Log(t, "patching route to target server")
			k8s.RunKubectl(t, routeK8SOptions, "patch", "httproute", "route", "-p", fmt.Sprintf(`{"spec":{"rules":[{"backendRefs":[{"name":"static-server","namespace":"%s","port":80}]}]}}`, serviceNamespace), "--type=merge")

			logger.Log(t, "patching route to gateway")
			k8s.RunKubectl(t, routeK8SOptions, "patch", "httproute", "route", "-p", fmt.Sprintf(`{"spec":{"parentRefs":[{"name":"gateway","namespace":"%s"}]}}`, gatewayNamespace), "--type=merge")

			// check that we have resolution errors because of missing reference grants
			time.Sleep(10 * time.Minute)
			// ensure that the consul resources are not created

			// create the reference grants

			// check that we have no resolution errors

			// ensure that the consul resources are created with the proper status
		})
	}
}

func applyFixture(t *testing.T, cfg *config.TestConfig, k8sOptions *terratestk8s.KubectlOptions, fixture string) {
	t.Helper()

	out, err := k8s.RunKubectlAndGetOutputE(t, k8sOptions, "apply", "-k", path.Join("../fixtures", fixture))
	require.NoError(t, err, out)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
		k8s.RunKubectlAndGetOutputE(t, k8sOptions, "delete", "-k", path.Join("../fixtures", fixture))
	})
}

func createNamespace(t *testing.T, ctx environment.TestContext, cfg *config.TestConfig) (string, *terratestk8s.KubectlOptions) {
	t.Helper()

	namespace := helpers.RandomName()

	logger.Logf(t, "creating Kubernetes namespace %s", namespace)
	k8s.RunKubectl(t, ctx.KubectlOptions(t), "create", "ns", namespace)
	helpers.Cleanup(t, cfg.NoCleanupOnFailure, func() {
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

	bits := 1024
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
