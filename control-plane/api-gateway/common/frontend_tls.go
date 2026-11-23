// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"bytes"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	gwv1 "sigs.k8s.io/gateway-api/apis/v1"
)

const FrontendCACertificateKey = "ca.crt"

var (
	ErrFrontendCAMissingKey         = errors.New("ConfigMap does not contain ca.crt")
	ErrFrontendCAMalformed          = errors.New("CA certificate bundle is malformed")
	ErrFrontendCAInvalidConstraint  = errors.New("certificate is not a valid CA")
	ErrFrontendCANoValidCertificate = errors.New("CA bundle has no currently valid certificate")
)

type FrontendCABundle struct {
	PEM              []byte
	ValidCount       int
	ExpiredCount     int
	NotYetValidCount int
	NextTransition   time.Time
}

func EffectiveFrontendTLSValidation(gateway gwv1.Gateway, port gwv1.PortNumber) *gwv1.FrontendTLSValidation {
	if gateway.Spec.TLS == nil || gateway.Spec.TLS.Frontend == nil {
		return nil
	}
	for _, override := range gateway.Spec.TLS.Frontend.PerPort {
		if override.Port == port {
			return override.TLS.Validation
		}
	}
	return gateway.Spec.TLS.Frontend.Default.Validation
}

func FrontendValidationMode(validation *gwv1.FrontendTLSValidation) gwv1.FrontendValidationModeType {
	if validation == nil || validation.Mode == "" {
		return gwv1.AllowValidOnly
	}
	return validation.Mode
}

func FrontendCAReferences(gateway gwv1.Gateway) []types.NamespacedName {
	unique := make(map[types.NamespacedName]struct{})
	add := func(validation *gwv1.FrontendTLSValidation) {
		if validation == nil {
			return
		}
		for _, ref := range validation.CACertificateRefs {
			if ref.Group != "" || ref.Kind != "ConfigMap" {
				continue
			}
			unique[IndexedNamespacedNameWithDefault(ref.Name, ref.Namespace, gateway.Namespace)] = struct{}{}
		}
	}
	if gateway.Spec.TLS != nil && gateway.Spec.TLS.Frontend != nil {
		add(gateway.Spec.TLS.Frontend.Default.Validation)
		for _, override := range gateway.Spec.TLS.Frontend.PerPort {
			add(override.TLS.Validation)
		}
	}

	refs := make([]types.NamespacedName, 0, len(unique))
	for ref := range unique {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Namespace == refs[j].Namespace {
			return refs[i].Name < refs[j].Name
		}
		return refs[i].Namespace < refs[j].Namespace
	})
	return refs
}

func FrontendCAResourceName(gateway types.NamespacedName, ref types.NamespacedName) string {
	sum := sha256.Sum256([]byte(gateway.String() + "\x00" + ref.String()))
	return gateway.Name + "-client-ca-" + hex.EncodeToString(sum[:6])
}

func FrontendCAFilename(gateway types.NamespacedName, ref types.NamespacedName) string {
	return FrontendCAResourceName(gateway, ref) + ".crt"
}

func ParseFrontendCABundle(configMap corev1.ConfigMap, now time.Time) (FrontendCABundle, error) {
	raw, ok := configMap.Data[FrontendCACertificateKey]
	if !ok {
		return FrontendCABundle{}, ErrFrontendCAMissingKey
	}

	remaining := []byte(raw)
	unique := make(map[string][]byte)
	result := FrontendCABundle{}
	for len(bytes.TrimSpace(remaining)) > 0 {
		block, rest := pem.Decode(remaining)
		if block == nil {
			return FrontendCABundle{}, ErrFrontendCAMalformed
		}
		remaining = rest
		if block.Type != "CERTIFICATE" || len(block.Headers) != 0 {
			return FrontendCABundle{}, fmt.Errorf("%w: unexpected PEM block %q", ErrFrontendCAMalformed, block.Type)
		}
		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return FrontendCABundle{}, fmt.Errorf("%w: %v", ErrFrontendCAMalformed, err)
		}
		if !certificate.BasicConstraintsValid || !certificate.IsCA ||
			certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
			return FrontendCABundle{}, fmt.Errorf("%w: subject %q", ErrFrontendCAInvalidConstraint, certificate.Subject.String())
		}

		switch {
		case now.Before(certificate.NotBefore):
			result.NotYetValidCount++
			result.NextTransition = earliestTime(result.NextTransition, certificate.NotBefore)
			continue
		case !now.Before(certificate.NotAfter):
			result.ExpiredCount++
			continue
		default:
			result.NextTransition = earliestTime(result.NextTransition, certificate.NotAfter)
		}
		unique[string(certificate.Raw)] = certificate.Raw
	}

	if len(unique) == 0 {
		return result, ErrFrontendCANoValidCertificate
	}

	der := make([][]byte, 0, len(unique))
	for _, certificate := range unique {
		der = append(der, certificate)
	}
	sort.Slice(der, func(i, j int) bool { return bytes.Compare(der[i], der[j]) < 0 })

	var canonical bytes.Buffer
	for _, certificate := range der {
		if err := pem.Encode(&canonical, &pem.Block{Type: "CERTIFICATE", Bytes: certificate}); err != nil {
			return FrontendCABundle{}, err
		}
	}
	result.PEM = canonical.Bytes()
	result.ValidCount = len(der)
	return result, nil
}

func ValidateFrontendCAReference(ref gwv1.ObjectReference) error {
	if ref.Group != "" || ref.Kind != "ConfigMap" {
		return fmt.Errorf("unsupported CA certificate reference %s/%s", ref.Group, ref.Kind)
	}
	if strings.TrimSpace(string(ref.Name)) == "" {
		return errors.New("CA certificate reference name must not be empty")
	}
	return nil
}

func earliestTime(current, candidate time.Time) time.Time {
	if current.IsZero() || candidate.Before(current) {
		return candidate
	}
	return current
}
