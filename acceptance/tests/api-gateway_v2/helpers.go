// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package apigatewayv2

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	meshv2beta1 "github.com/hashicorp/consul-k8s/control-plane/api/mesh/v2beta1"
	corev1 "k8s.io/api/core/v1"
	"math/big"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	StaticClientName           = "static-client"
	gatewayClassControllerName = "mesh.consul.hashicorp.com/gateway-controller"
	//TODO these values will likely need to be update to their V2 values for the test to pass.
	gatewayClassFinalizer = "gateway-exists-finalizer.consul.hashicorp.com"
	gatewayFinalizer      = "gateway-finalizer.consul.hashicorp.com"
)

type certificateInfo struct {
	Cert          *x509.Certificate
	PrivateKey    *rsa.PrivateKey
	CertPEM       []byte
	PrivateKeyPEM []byte
}

func checkV2StatusCondition(t require.TestingT, conditions []meshv2beta1.Condition, toCheck meshv2beta1.Condition) {
	for _, c := range conditions {
		if c.Type == toCheck.Type {
			require.EqualValues(t, toCheck.Reason, c.Reason)
			require.EqualValues(t, toCheck.Status, c.Status)
			return
		}
	}

	t.Errorf("expected condition not found: %s", toCheck.Type)
}

func trueV2Condition(conditionType, reason string) meshv2beta1.Condition {
	return meshv2beta1.Condition{
		Type:   meshv2beta1.ConditionType(conditionType),
		Reason: reason,
		Status: corev1.ConditionTrue,
	}
}

func falseV2Condition(conditionType, reason string) meshv2beta1.Condition {
	return meshv2beta1.Condition{
		Type:   meshv2beta1.ConditionType(conditionType),
		Reason: reason,
		Status: corev1.ConditionFalse,
	}
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
