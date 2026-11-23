// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/stretchr/testify/require"
)

func TestParseFrontendCABundleCanonicalizes(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	caA := testCACertificate(t, "a", now.Add(-time.Hour), now.Add(time.Hour), true)
	caB := testCACertificate(t, "b", now.Add(-time.Hour), now.Add(2*time.Hour), true)

	first, err := ParseFrontendCABundle(corev1.ConfigMap{
		Data: map[string]string{FrontendCACertificateKey: string(append(append(caB, caA...), caA...))},
	}, now)
	require.NoError(t, err)
	require.Equal(t, 2, first.ValidCount)

	second, err := ParseFrontendCABundle(corev1.ConfigMap{
		Data: map[string]string{FrontendCACertificateKey: "\n" + string(append(caA, caB...))},
	}, now)
	require.NoError(t, err)
	require.True(t, bytes.Equal(first.PEM, second.PEM))
	require.Equal(t, now.Add(time.Hour), first.NextTransition)
}

func TestParseFrontendCABundleValidity(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	t.Run("missing key", func(t *testing.T) {
		_, err := ParseFrontendCABundle(corev1.ConfigMap{}, now)
		require.ErrorIs(t, err, ErrFrontendCAMissingKey)
	})

	t.Run("malformed trailing data", func(t *testing.T) {
		ca := append(testCACertificate(t, "ca", now.Add(-time.Hour), now.Add(time.Hour), true), []byte("trailing")...)
		_, err := ParseFrontendCABundle(corev1.ConfigMap{Data: map[string]string{FrontendCACertificateKey: string(ca)}}, now)
		require.ErrorIs(t, err, ErrFrontendCAMalformed)
	})

	t.Run("invalid CA constraints", func(t *testing.T) {
		cert := testCACertificate(t, "leaf", now.Add(-time.Hour), now.Add(time.Hour), false)
		_, err := ParseFrontendCABundle(corev1.ConfigMap{Data: map[string]string{FrontendCACertificateKey: string(cert)}}, now)
		require.ErrorIs(t, err, ErrFrontendCAInvalidConstraint)
	})

	t.Run("expired and future without valid CA", func(t *testing.T) {
		expired := testCACertificate(t, "expired", now.Add(-2*time.Hour), now.Add(-time.Hour), true)
		future := testCACertificate(t, "future", now.Add(time.Hour), now.Add(2*time.Hour), true)
		bundle, err := ParseFrontendCABundle(corev1.ConfigMap{
			Data: map[string]string{FrontendCACertificateKey: string(append(expired, future...))},
		}, now)
		require.ErrorIs(t, err, ErrFrontendCANoValidCertificate)
		require.Equal(t, 1, bundle.ExpiredCount)
		require.Equal(t, 1, bundle.NotYetValidCount)
		require.Equal(t, now.Add(time.Hour), bundle.NextTransition)
	})
}

func TestFrontendCAReferencesIncludesDefaultAndPerPort(t *testing.T) {
	defaultRef := gwv1.ObjectReference{Group: "", Kind: "ConfigMap", Name: "default"}
	otherNamespace := gwv1.Namespace("security")
	overrideRef := gwv1.ObjectReference{Group: "", Kind: "ConfigMap", Name: "partner", Namespace: &otherNamespace}
	gateway := gwv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "gateway", Namespace: "apps"},
		Spec: gwv1.GatewaySpec{
			TLS: &gwv1.GatewayTLSConfig{Frontend: &gwv1.FrontendTLSConfig{
				Default: gwv1.TLSConfig{Validation: &gwv1.FrontendTLSValidation{CACertificateRefs: []gwv1.ObjectReference{defaultRef}}},
				PerPort: []gwv1.TLSPortConfig{{
					Port: 8443,
					TLS:  gwv1.TLSConfig{Validation: &gwv1.FrontendTLSValidation{CACertificateRefs: []gwv1.ObjectReference{overrideRef}}},
				}},
			}},
		},
	}

	require.Equal(t, []string{"apps/default", "security/partner"}, []string{
		FrontendCAReferences(gateway)[0].String(),
		FrontendCAReferences(gateway)[1].String(),
	})
	require.Equal(t, gwv1.AllowValidOnly, FrontendValidationMode(EffectiveFrontendTLSValidation(gateway, 443)))
	require.Equal(t, overrideRef, EffectiveFrontendTLSValidation(gateway, 8443).CACertificateRefs[0])
}

func TestFrontendTLSMissingCAKeepsPortBlocked(t *testing.T) {
	policy := &gwv1.FrontendTLSValidation{CACertificateRefs: []gwv1.ObjectReference{{Kind: "ConfigMap", Name: "missing"}}}
	gateway := gwv1.Gateway{Spec: gwv1.GatewaySpec{TLS: &gwv1.GatewayTLSConfig{
		Frontend: &gwv1.FrontendTLSConfig{PerPort: []gwv1.TLSPortConfig{
			{Port: 8443, TLS: gwv1.TLSConfig{Validation: policy}},
		}},
	}}}
	got := (ResourceTranslator{}).frontendTLS(gateway, &ResourceMap{}, nil)
	require.Nil(t, got.ValidationForPort(443))
	require.Equal(t, &api.APIGatewayFrontendTLSValidation{Mode: api.APIGatewayFrontendValidationDenyAll}, got.ValidationForPort(8443))
}

func testCACertificate(t *testing.T, commonName string, notBefore, notAfter time.Time, isCA bool) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(notBefore.UnixNano()),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		BasicConstraintsValid: true,
		IsCA:                  isCA,
		KeyUsage:              x509.KeyUsageDigitalSignature,
	}
	if isCA {
		template.KeyUsage |= x509.KeyUsageCertSign
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}
