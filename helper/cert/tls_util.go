package cert

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// GenerateCA generates a CA with the provided
// common name valid for 10 years. It returns the private key as
// a crypto.Signer and a PEM string and certificate
// as a *x509.Certificate and a PEM string or an error.
func GenerateCA(commonName string) (
	signer crypto.Signer,
	keyPem string,
	caCertPem string,
	caCertTemplate *x509.Certificate,
	err error) {
	// Create the private key we'll use for this CA cert.
	signer, keyPem, err = privateKey()
	if err != nil {
		return
	}

	// The serial number for the cert
	sn, err := serialNumber()
	if err != nil {
		return
	}

	signerKeyId, err := keyId(signer.Public())
	if err != nil {
		return
	}

	// Create the CA cert
	caCertTemplate = &x509.Certificate{
		SerialNumber: sn,
		Subject: pkix.Name{
			CommonName:    commonName,
			Country:       []string{"US"},
			PostalCode:    []string{"94105"},
			Province:      []string{"CA"},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{"101 Second Street"},
			Organization:  []string{"HashiCorp Inc."},
		},
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IsCA:                  true,
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		NotBefore:             time.Now().Add(-1 * time.Minute),
		AuthorityKeyId:        signerKeyId,
		SubjectKeyId:          signerKeyId,
	}

	bs, err := x509.CreateCertificate(
		rand.Reader, caCertTemplate, caCertTemplate, signer.Public(), signer)
	if err != nil {
		return
	}

	var buf bytes.Buffer
	err = pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: bs})
	if err != nil {
		return
	}
	caCertPem = buf.String()

	return
}

// GenerateCert generates a leaf certificate
// with the given common name, expiry, hosts as SANs,
// and CA. It returns a PEM encoded certificate
// and private key of the generated certificate or an error.
func GenerateCert(
	commonName string,
	expiry time.Duration,
	caCert *x509.Certificate,
	caCertSigner crypto.Signer,
	hosts []string) (string, string, error) {
	// Create the private key we'll use for this leaf cert.
	signer, keyPEM, err := privateKey()
	if err != nil {
		return "", "", err
	}

	// The serial number for the cert
	sn, err := serialNumber()
	if err != nil {
		return "", "", err
	}

	// Create the leaf cert
	template := x509.Certificate{
		SerialNumber:          sn,
		Subject:               pkix.Name{CommonName: commonName},
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		NotAfter:              time.Now().Add(expiry),
		NotBefore:             time.Now().Add(-1 * time.Minute),
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
		}
	}
	bs, err := x509.CreateCertificate(
		rand.Reader, &template, caCert, signer.Public(), caCertSigner)
	if err != nil {
		return "", "", err
	}
	var buf bytes.Buffer
	err = pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: bs})
	if err != nil {
		return "", "", err
	}

	return buf.String(), keyPEM, nil
}

// ParseCert parses the x509 certificate from a PEM-encoded value.
func ParseCert(pemValue []byte) (*x509.Certificate, error) {
	// The _ result below is not an error but the remaining PEM bytes.
	block, _ := pem.Decode(pemValue)
	if block == nil {
		return nil, fmt.Errorf("no PEM-encoded data found")
	}

	if block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("first PEM-block should be CERTIFICATE type")
	}

	return x509.ParseCertificate(block.Bytes)
}

// ParseSigner parses a crypto.Signer from a PEM-encoded key. The private key
// is expected to be the first block in the PEM value.
func ParseSigner(pemValue string) (crypto.Signer, error) {
	// The _ result below is not an error but the remaining PEM bytes.
	block, _ := pem.Decode([]byte(pemValue))
	if block == nil {
		return nil, fmt.Errorf("no PEM-encoded data found")
	}

	switch block.Type {
	case "EC PRIVATE KEY":
		return x509.ParseECPrivateKey(block.Bytes)
	default:
		return nil, fmt.Errorf("unknown PEM block type for signing key: %s", block.Type)
	}
}

// privateKey returns a new ECDSA-based private key. Both a crypto.Signer
// and the key in PEM format are returned.
func privateKey() (crypto.Signer, string, error) {
	pk, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, "", err
	}

	bs, err := x509.MarshalECPrivateKey(pk)
	if err != nil {
		return nil, "", err
	}

	var buf bytes.Buffer
	err = pem.Encode(&buf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: bs})
	if err != nil {
		return nil, "", err
	}

	return pk, buf.String(), nil
}

// serialNumber generates a new random serial number.
func serialNumber() (*big.Int, error) {
	return rand.Int(rand.Reader, (&big.Int{}).Exp(big.NewInt(2), big.NewInt(159), nil))
}

// keyId returns a x509 keyId from the given signing key. The key must be
// an *ecdsa.PublicKey currently, but may support more types in the future.
func keyId(raw interface{}) ([]byte, error) {
	switch raw.(type) {
	case *ecdsa.PublicKey:
	default:
		return nil, fmt.Errorf("invalid key type: %T", raw)
	}

	// This is not standard; RFC allows any unique identifier as long as they
	// match in subject/authority chains but suggests specific hashing of DER
	// bytes of public key including DER tags.
	bs, err := x509.MarshalPKIXPublicKey(raw)
	if err != nil {
		return nil, err
	}

	// String formatted
	kID := sha256.Sum256(bs)
	return []byte(strings.Replace(fmt.Sprintf("% x", kID), " ", ":", -1)), nil
}
