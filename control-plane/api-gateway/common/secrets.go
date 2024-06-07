// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/miekg/dns"
	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/version"
)

var (
	errFailedToParsePrivateKeyPem = errors.New("failed to parse private key PEM")
	errKeyLengthTooShort          = errors.New("RSA key length must be at least 2048-bit")
	errKeyLengthTooShortFIPS      = errors.New("RSA key length must be at either 2048-bit, 3072-bit, or 4096-bit in FIPS mode")
)

func ParseCertificateData(secret corev1.Secret) (cert string, privateKey string, err error) {
	decodedPrivateKey := secret.Data[corev1.TLSPrivateKeyKey]
	decodedCertificate := secret.Data[corev1.TLSCertKey]

	privateKeyBlock, _ := pem.Decode(decodedPrivateKey)
	if privateKeyBlock == nil {
		return "", "", errFailedToParsePrivateKeyPem
	}

	certificateBlock, _ := pem.Decode(decodedCertificate)
	if certificateBlock == nil {
		return "", "", errors.New("failed to parse certificate PEM")
	}

	// make sure we have a valid x509 certificate
	certificate, err := x509.ParseCertificate(certificateBlock.Bytes)
	if err != nil {
		return "", "", err
	}

	// validate that the cert was generated with the given private key
	_, err = tls.X509KeyPair(decodedCertificate, decodedPrivateKey)
	if err != nil {
		return "", "", err
	}

	// validate that each host referenced in the CN, DNSSans, and IPSans
	// are valid hostnames
	if err := validateCertificateHosts(certificate); err != nil {
		return "", "", err
	}

	return string(decodedCertificate), string(decodedPrivateKey), nil
}

func validateCertificateHosts(certificate *x509.Certificate) error {
	hosts := []string{certificate.Subject.CommonName}

	hosts = append(hosts, certificate.DNSNames...)

	for _, ip := range certificate.IPAddresses {
		hosts = append(hosts, ip.String())
	}

	for _, host := range hosts {
		if _, ok := dns.IsDomainName(host); !ok {
			return fmt.Errorf("host %q must be a valid DNS hostname", host)
		}
	}

	return nil
}

// Envoy will silently reject any keys that are less than 2048 bytes long
// https://github.com/envoyproxy/envoy/blob/main/source/extensions/transport_sockets/tls/context_impl.cc#L238
const MinKeyLength = 2048

// ValidateKeyLength ensures that the key length for a certificate is of a valid length
// for envoy dependent on if consul is running in FIPS mode or not.
func ValidateKeyLength(privateKey string) error {
	privateKeyBlock, _ := pem.Decode([]byte(privateKey))

	if privateKeyBlock == nil {
		return errFailedToParsePrivateKeyPem
	}

	if privateKeyBlock.Type != "RSA PRIVATE KEY" {
		return nil
	}

	key, err := x509.ParsePKCS1PrivateKey(privateKeyBlock.Bytes)
	if err != nil {
		return err
	}

	keyBitLen := key.N.BitLen()

	if version.IsFIPS() {
		return fipsLenCheck(keyBitLen)
	}

	return nonFipsLenCheck(keyBitLen)
}

func nonFipsLenCheck(keyLen int) error {
	// ensure private key is of the correct length
	if keyLen < MinKeyLength {
		return errKeyLengthTooShort
	}

	return nil
}

func fipsLenCheck(keyLen int) error {
	if keyLen != 2048 && keyLen != 3072 && keyLen != 4096 {
		return errKeyLengthTooShortFIPS
	}
	return nil
}
