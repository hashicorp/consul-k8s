// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package tls_init

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"testing"
	"time"

	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  nil,
			expErr: "-name-prefix must be set",
		},
		{
			flags:  []string{"-name-prefix", "consul", "-ca", "foo"},
			expErr: "either both -ca and -key or neither must be set",
		},
		{
			flags:  []string{"-name-prefix", "consul", "-key", "/foo"},
			expErr: "either both -ca and -key or neither must be set",
		},
		{
			flags:  []string{"-name-prefix", "consul", "-log-level", "invalid"},
			expErr: "unknown log level: invalid",
		},
		{
			flags:  []string{"-name-prefix", "consul", "-days", "-3"},
			expErr: "-days must be a positive integer",
		},
	}

	for _, c := range cases {
		t.Run(c.expErr, func(tt *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{UI: ui}
			exitCode := cmd.Run(c.flags)
			require.Equal(tt, 1, exitCode, ui.ErrorWriter.String())
			require.Contains(tt, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

func TestRun_CreatesServerCertificatesWithExistingCAAsFiles(t *testing.T) {

	cases := []struct {
		caCert    string
		caKey     string
		algorithm string
	}{
		{
			caCert:    caCertEC,
			caKey:     caKeyEC,
			algorithm: "ec",
		},
		{
			caCert:    caCertRSA,
			caKey:     caKeyRSA,
			algorithm: "rsa",
		},
		{
			// caCertRSA is used because the key is just caKeyRSA encrypted.
			caCert:    caCertRSA,
			caKey:     caKeyPKCS8,
			algorithm: "pkcs8",
		},
	}

	for _, c := range cases {
		t.Run(c.algorithm, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{UI: ui}
			k8s := fake.NewSimpleClientset()
			cmd.clientset = k8s

			ca, err := os.CreateTemp("", "")
			require.NoError(t, err)
			defer os.RemoveAll(ca.Name())
			err = os.WriteFile(ca.Name(), []byte(c.caCert), 0644)
			require.NoError(t, err)

			key, err := os.CreateTemp("", "")
			require.NoError(t, err)
			defer os.RemoveAll(key.Name())
			err = os.WriteFile(key.Name(), []byte(c.caKey), 0644)
			require.NoError(t, err)

			flags := []string{"-name-prefix", "consul", "-ca", ca.Name(), "-key", key.Name()}

			exitCode := cmd.Run(flags)
			require.Equal(t, 0, exitCode)

			caCertBlock, _ := pem.Decode([]byte(c.caCert))
			caCertificate, err := x509.ParseCertificate(caCertBlock.Bytes)
			require.NoError(t, err)

			serverCertSecret, err := k8s.CoreV1().Secrets("default").Get(context.Background(), "consul-server-cert", metav1.GetOptions{})
			require.NoError(t, err)
			serverCert := serverCertSecret.Data[corev1.TLSCertKey]
			serverKey := serverCertSecret.Data[corev1.TLSPrivateKeyKey]

			certBlock, _ := pem.Decode(serverCert)
			certificate, err := x509.ParseCertificate(certBlock.Bytes)
			require.NoError(t, err)
			require.False(t, certificate.IsCA)
			require.Equal(t, []string{"server.dc1.consul", "localhost"}, certificate.DNSNames)
			require.Equal(t, []net.IP{net.ParseIP("127.0.0.1").To4(), net.ParseIP("::1").To16()}, certificate.IPAddresses)

			keyBlock, _ := pem.Decode(serverKey)
			privateKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
			require.NoError(t, err)
			require.Equal(t, &privateKey.PublicKey, certificate.PublicKey)

			require.NoError(t, certificate.CheckSignatureFrom(caCertificate))

		})
	}
}

func TestRun_UpdatesServerCertificatesWithExistingCertsAsFiles(t *testing.T) {
	ui := cli.NewMockUi()
	cmd := Command{UI: ui}
	k8s := fake.NewSimpleClientset()
	cmd.clientset = k8s

	_, err := k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-server-cert",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       []byte(serverCert),
			corev1.TLSPrivateKeyKey: []byte(serverKey),
		},
		Type: corev1.SecretTypeTLS,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	ca, err := os.CreateTemp("", "")
	require.NoError(t, err)
	defer os.RemoveAll(ca.Name())
	err = os.WriteFile(ca.Name(), []byte(caCertEC), 0644)
	require.NoError(t, err)

	key, err := os.CreateTemp("", "")
	require.NoError(t, err)
	defer os.RemoveAll(key.Name())
	err = os.WriteFile(key.Name(), []byte(caKeyEC), 0644)
	require.NoError(t, err)

	flags := []string{"-name-prefix", "consul", "-ca", ca.Name(), "-key", key.Name(), "-additional-dnsname", "test.dns.name"}

	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode)

	caCertBlock, _ := pem.Decode([]byte(caCertEC))
	caCertificate, err := x509.ParseCertificate(caCertBlock.Bytes)
	require.NoError(t, err)

	serverCertSecret, err := k8s.CoreV1().Secrets("default").Get(context.Background(), "consul-server-cert", metav1.GetOptions{})
	require.NoError(t, err)
	newServerCert := serverCertSecret.Data[corev1.TLSCertKey]
	newServerKey := serverCertSecret.Data[corev1.TLSPrivateKeyKey]

	require.NotEqual(t, []byte(serverCert), newServerCert)
	certBlock, _ := pem.Decode(newServerCert)
	certificate, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	require.False(t, certificate.IsCA)
	require.Equal(t, []string{"test.dns.name", "server.dc1.consul", "localhost"}, certificate.DNSNames)

	require.NotEqual(t, []byte(serverKey), newServerKey)
	keyBlock, _ := pem.Decode(newServerKey)
	privateKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	require.NoError(t, err)
	require.Equal(t, &privateKey.PublicKey, certificate.PublicKey)

	require.NoError(t, certificate.CheckSignatureFrom(caCertificate))
}

func TestRun_CreatesServerCertificatesWithExistingCertsAsSecrets(t *testing.T) {
	ui := cli.NewMockUi()
	cmd := Command{UI: ui}
	k8s := fake.NewSimpleClientset()

	_, err := k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-cert",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSCertKey: []byte(caCertEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-key",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey: []byte(caKeyEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	cmd.clientset = k8s

	flags := []string{"-name-prefix", "consul"}

	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode)

	caCertBlock, _ := pem.Decode([]byte(caCertEC))
	caCertificate, err := x509.ParseCertificate(caCertBlock.Bytes)
	require.NoError(t, err)

	serverCertSecret, err := k8s.CoreV1().Secrets("default").Get(context.Background(), "consul-server-cert", metav1.GetOptions{})
	require.NoError(t, err)
	serverCert := serverCertSecret.Data[corev1.TLSCertKey]
	serverKey := serverCertSecret.Data[corev1.TLSPrivateKeyKey]

	certBlock, _ := pem.Decode(serverCert)
	certificate, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	require.False(t, certificate.IsCA)
	require.Equal(t, []string{"server.dc1.consul", "localhost"}, certificate.DNSNames)
	require.Equal(t, []net.IP{net.ParseIP("127.0.0.1").To4(), net.ParseIP("::1").To16()}, certificate.IPAddresses)

	keyBlock, _ := pem.Decode(serverKey)
	privateKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	require.NoError(t, err)
	require.Equal(t, &privateKey.PublicKey, certificate.PublicKey)

	require.NoError(t, certificate.CheckSignatureFrom(caCertificate))
}

func TestRun_CreatesServerCertificatesWithoutExistingCerts(t *testing.T) {
	ui := cli.NewMockUi()
	cmd := Command{UI: ui}
	k8s := fake.NewSimpleClientset()
	cmd.clientset = k8s

	flags := []string{"-name-prefix", "consul"}
	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode)

	serverCertSecret, err := k8s.CoreV1().Secrets("default").Get(context.Background(), "consul-server-cert", metav1.GetOptions{})
	require.NoError(t, err)
	newServerCert := serverCertSecret.Data[corev1.TLSCertKey]
	newServerKey := serverCertSecret.Data[corev1.TLSPrivateKeyKey]

	certBlock, _ := pem.Decode(newServerCert)
	certificate, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	require.False(t, certificate.IsCA)
	require.Equal(t, []string{"server.dc1.consul", "localhost"}, certificate.DNSNames)
	require.Equal(t, []net.IP{net.ParseIP("127.0.0.1").To4(), net.ParseIP("::1").To16()}, certificate.IPAddresses)

	keyBlock, _ := pem.Decode(newServerKey)
	privateKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	require.NoError(t, err)
	require.Equal(t, &privateKey.PublicKey, certificate.PublicKey)
}

func TestRun_UpdatesServerCertificatesWithExistingCertsAsSecrets(t *testing.T) {
	ui := cli.NewMockUi()
	cmd := Command{UI: ui}
	k8s := fake.NewSimpleClientset()
	cmd.clientset = k8s

	_, err := k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-cert",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSCertKey: []byte(caCertEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-key",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey: []byte(caKeyEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-server-cert",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSCertKey:       []byte(serverCert),
			corev1.TLSPrivateKeyKey: []byte(serverKey),
		},
		Type: corev1.SecretTypeTLS,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	flags := []string{"-name-prefix", "consul", "-additional-dnsname", "test.dns.name"}
	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode)

	caCertBlock, _ := pem.Decode([]byte(caCertEC))
	caCertificate, err := x509.ParseCertificate(caCertBlock.Bytes)
	require.NoError(t, err)

	serverCertSecret, err := k8s.CoreV1().Secrets("default").Get(context.Background(), "consul-server-cert", metav1.GetOptions{})
	require.NoError(t, err)
	newServerCert := serverCertSecret.Data[corev1.TLSCertKey]
	newServerKey := serverCertSecret.Data[corev1.TLSPrivateKeyKey]

	require.NotEqual(t, []byte(serverCert), newServerCert)
	certBlock, _ := pem.Decode(newServerCert)
	certificate, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	require.False(t, certificate.IsCA)
	require.Equal(t, []string{"test.dns.name", "server.dc1.consul", "localhost"}, certificate.DNSNames)

	require.NotEqual(t, []byte(serverKey), newServerKey)
	keyBlock, _ := pem.Decode(newServerKey)
	privateKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	require.NoError(t, err)
	require.Equal(t, &privateKey.PublicKey, certificate.PublicKey)

	require.NoError(t, certificate.CheckSignatureFrom(caCertificate))
}

func TestRun_CreatesServerCertificatesWithExpiryWithinSpecifiedDays(t *testing.T) {
	ui := cli.NewMockUi()
	cmd := Command{UI: ui}
	k8s := fake.NewSimpleClientset()
	cmd.clientset = k8s

	_, err := k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-cert",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSCertKey: []byte(caCertEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-key",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey: []byte(caKeyEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	flags := []string{"-name-prefix", "consul", "-days", "365"}
	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode)

	serverCertSecret, err := k8s.CoreV1().Secrets("default").Get(context.Background(), "consul-server-cert", metav1.GetOptions{})
	require.NoError(t, err)
	newServerCert := serverCertSecret.Data[corev1.TLSCertKey]

	require.NotEqual(t, []byte(serverCert), newServerCert)
	certBlock, _ := pem.Decode(newServerCert)
	certificate, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)

	// Add 365 days instead of 1 year to account for leap years
	require.Equal(t, time.Now().AddDate(0, 0, 365).Unix(), certificate.NotAfter.Unix())
}

func TestRun_CreatesServerCertificatesWithProvidedHosts(t *testing.T) {
	ui := cli.NewMockUi()
	cmd := Command{UI: ui}
	k8s := fake.NewSimpleClientset()
	cmd.clientset = k8s

	_, err := k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-cert",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSCertKey: []byte(caCertEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-key",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey: []byte(caKeyEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	flags := []string{"-name-prefix", "consul", "-additional-dnsname", "test.name.one", "-additional-dnsname", "test.name.two", "-additional-ipaddress", "10.0.0.1"}
	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode)

	serverCertSecret, err := k8s.CoreV1().Secrets("default").Get(context.Background(), "consul-server-cert", metav1.GetOptions{})
	require.NoError(t, err)
	newServerCert := serverCertSecret.Data[corev1.TLSCertKey]

	require.NotEqual(t, []byte(serverCert), newServerCert)
	certBlock, _ := pem.Decode(newServerCert)
	certificate, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	require.Equal(t, []string{"test.name.one", "test.name.two", "server.dc1.consul", "localhost"}, certificate.DNSNames)
	require.Equal(t, []net.IP{net.ParseIP(`10.0.0.1`).To4(), net.ParseIP(`127.0.0.1`).To4(), net.ParseIP(`::1`).To16()}, certificate.IPAddresses)
}

func TestRun_CreatesServerCertificatesWithSpecifiedDomainAndDC(t *testing.T) {
	ui := cli.NewMockUi()
	cmd := Command{UI: ui}
	k8s := fake.NewSimpleClientset()
	cmd.clientset = k8s

	_, err := k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-cert",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSCertKey: []byte(caCertEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = k8s.CoreV1().Secrets("default").Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-key",
			Namespace: "default",
		},
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey: []byte(caKeyEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	flags := []string{"-name-prefix", "consul", "-domain", "foobar", "-dc", "testdc"}
	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode)

	serverCertSecret, err := k8s.CoreV1().Secrets("default").Get(context.Background(), "consul-server-cert", metav1.GetOptions{})
	require.NoError(t, err)
	newServerCert := serverCertSecret.Data[corev1.TLSCertKey]

	require.NotEqual(t, []byte(serverCert), newServerCert)
	certBlock, _ := pem.Decode(newServerCert)
	certificate, err := x509.ParseCertificate(certBlock.Bytes)
	require.NoError(t, err)
	require.Equal(t, []string{"server.testdc.foobar", "localhost"}, certificate.DNSNames)
}

func TestRun_CreatesServerCertificatesInSpecifiedNamespace(t *testing.T) {
	ui := cli.NewMockUi()
	cmd := Command{UI: ui}
	k8s := fake.NewSimpleClientset()
	cmd.clientset = k8s
	namespace := "foo"

	_, err := k8s.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = k8s.CoreV1().Secrets(namespace).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-cert",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			corev1.TLSCertKey: []byte(caCertEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	_, err = k8s.CoreV1().Secrets(namespace).Create(context.Background(), &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consul-ca-key",
			Namespace: namespace,
		},
		Data: map[string][]byte{
			corev1.TLSPrivateKeyKey: []byte(caKeyEC),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	flags := []string{"-name-prefix", "consul", "-k8s-namespace", namespace}
	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode)

	_, err = k8s.CoreV1().Secrets(namespace).Get(context.Background(), "consul-server-cert", metav1.GetOptions{})
	require.NoError(t, err)
}

const (
	caCertEC string = `-----BEGIN CERTIFICATE-----
MIIDPjCCAuWgAwIBAgIRAOjdIMIYBXgeoXBDydhFImcwCgYIKoZIzj0EAwIwgZEx
CzAJBgNVBAYTAlVTMQswCQYDVQQIEwJDQTEWMBQGA1UEBxMNU2FuIEZyYW5jaXNj
bzEaMBgGA1UECRMRMTAxIFNlY29uZCBTdHJlZXQxDjAMBgNVBBETBTk0MTA1MRcw
FQYDVQQKEw5IYXNoaUNvcnAgSW5jLjEYMBYGA1UEAxMPQ29uc3VsIEFnZW50IENB
MB4XDTIwMTIxNzIyMDUyMFoXDTMwMTIxNTIyMDYyMFowgZExCzAJBgNVBAYTAlVT
MQswCQYDVQQIEwJDQTEWMBQGA1UEBxMNU2FuIEZyYW5jaXNjbzEaMBgGA1UECRMR
MTAxIFNlY29uZCBTdHJlZXQxDjAMBgNVBBETBTk0MTA1MRcwFQYDVQQKEw5IYXNo
aUNvcnAgSW5jLjEYMBYGA1UEAxMPQ29uc3VsIEFnZW50IENBMFkwEwYHKoZIzj0C
AQYIKoZIzj0DAQcDQgAEvETlXGiuMdIH3nOTf/1RGYmBoZA9RaaDp1T9kcABGuzo
xEA+P7VOrd4cnIiTnYqkslAdqcXWmoFEubPFTuKgh6OCARowggEWMA4GA1UdDwEB
/wQEAwIBhjAdBgNVHSUEFjAUBggrBgEFBQcDAQYIKwYBBQUHAwIwDwYDVR0TAQH/
BAUwAwEB/zBoBgNVHQ4EYQRfMmI6OWI6MDY6NmI6ZjQ6NTc6M2M6Zjc6MjA6OGQ6
ZTA6NjU6NjQ6MjY6MjI6NDA6YTk6NmE6NmI6MjM6MmU6MGY6OGI6YzI6MTk6MDI6
YzA6OTk6NTE6MDE6ZWM6ZTQwagYDVR0jBGMwYYBfMmI6OWI6MDY6NmI6ZjQ6NTc6
M2M6Zjc6MjA6OGQ6ZTA6NjU6NjQ6MjY6MjI6NDA6YTk6NmE6NmI6MjM6MmU6MGY6
OGI6YzI6MTk6MDI6YzA6OTk6NTE6MDE6ZWM6ZTQwCgYIKoZIzj0EAwIDRwAwRAIg
WBJ2jlEV/kttcHlcHpvyO3GHCp3AE+G4f27NWqYdYeACIDJkx6OjZBU7i4K3HSrO
qlxZIl+NFZSHr8XS6BFNB8vc
-----END CERTIFICATE-----`

	caKeyEC string = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIO+ASjxFB5gYZju94Ujx81ykp54K53b1TvQNQW/zgbFqoAoGCCqGSM49
AwEHoUQDQgAEvETlXGiuMdIH3nOTf/1RGYmBoZA9RaaDp1T9kcABGuzoxEA+P7VO
rd4cnIiTnYqkslAdqcXWmoFEubPFTuKghw==
-----END EC PRIVATE KEY-----`

	serverCert string = `-----BEGIN CERTIFICATE-----
MIIDJTCCAsqgAwIBAgIRAKC1c+hYXwrGBWqsUpkfG8owCgYIKoZIzj0EAwIwgZEx
CzAJBgNVBAYTAlVTMQswCQYDVQQIEwJDQTEWMBQGA1UEBxMNU2FuIEZyYW5jaXNj
bzEaMBgGA1UECRMRMTAxIFNlY29uZCBTdHJlZXQxDjAMBgNVBBETBTk0MTA1MRcw
FQYDVQQKEw5IYXNoaUNvcnAgSW5jLjEYMBYGA1UEAxMPQ29uc3VsIEFnZW50IENB
MB4XDTIwMTIxNzIyMDUyMFoXDTIyMTIxNzIyMDYyMFowHDEaMBgGA1UEAxMRc2Vy
dmVyLmRjMS5jb25zdWwwWTATBgcqhkjOPQIBBggqhkjOPQMBBwNCAAS7oVQHhZsm
kmZd71xcLa9/3GW8sHlqP2V8DODi48v7JcdSkcxjJnK12xlrSMcxWQMis2gvyebG
7pfnvzE1jfTjo4IBdTCCAXEwDgYDVR0PAQH/BAQDAgWgMB0GA1UdJQQWMBQGCCsG
AQUFBwMBBggrBgEFBQcDAjAMBgNVHRMBAf8EAjAAMGoGA1UdIwRjMGGAXzJiOjli
OjA2OjZiOmY0OjU3OjNjOmY3OjIwOjhkOmUwOjY1OjY0OjI2OjIyOjQwOmE5OjZh
OjZiOjIzOjJlOjBmOjhiOmMyOjE5OjAyOmMwOjk5OjUxOjAxOmVjOmU0MIHFBgNV
HREEgb0wgbqCDWNvbnN1bC1zZXJ2ZXKCDyouY29uc3VsLXNlcnZlcoIXKi5jb25z
dWwtc2VydmVyLmRlZmF1bHSCGyouY29uc3VsLXNlcnZlci5kZWZhdWx0LnN2Y4IT
Ki5zZXJ2ZXIuZGMxLmNvbnN1bIIXYXNod2luLmNhbi5yb3RhdGUuY2VydHOCCnNv
LmNhbi55b3WCEXNlcnZlci5kYzEuY29uc3Vsgglsb2NhbGhvc3SHBAECAwSHBH8A
AAEwCgYIKoZIzj0EAwIDSQAwRgIhALrYVPNgn0zt+C/tIQHLPR4OVQ5I1IrTE2DL
/rnAjJqkAiEAt/3xto4eRFi7h9RNvSuoXCRGXJosCR8BRK1QR/aw4Bc=
-----END CERTIFICATE-----`

	serverKey string = `-----BEGIN EC PRIVATE KEY-----
MHcCAQEEINVuQ1fmOWH5HDG7wEqB1KObSs7q26czY7P+WLhtLDZPoAoGCCqGSM49
AwEHoUQDQgAEu6FUB4WbJpJmXe9cXC2vf9xlvLB5aj9lfAzg4uPL+yXHUpHMYyZy
tdsZa0jHMVkDIrNoL8nmxu6X578xNY304w==
-----END EC PRIVATE KEY-----`

	caCertRSA string = `-----BEGIN CERTIFICATE-----
MIIDGjCCAgICCQC9IJfDAbKSIjANBgkqhkiG9w0BAQsFADBPMQswCQYDVQQGEwJD
QTEZMBcGA1UECAwQQnJpdGlzaCBDb2x1bWJpYTERMA8GA1UEBwwIVmFuY292ZXIx
EjAQBgNVBAoMCUhhc2hpQ29ycDAeFw0yMTExMDQyMjQ0MjJaFw0yMTEyMDQyMjQ0
MjJaME8xCzAJBgNVBAYTAkNBMRkwFwYDVQQIDBBCcml0aXNoIENvbHVtYmlhMREw
DwYDVQQHDAhWYW5jb3ZlcjESMBAGA1UECgwJSGFzaGlDb3JwMIIBIjANBgkqhkiG
9w0BAQEFAAOCAQ8AMIIBCgKCAQEAxTVd5sFGuVOZxkPv3tE69khUToVcRb85NRWW
eWBSyrTE4UWr06kG4reSTVrGgR2hojrP4nzVynu7EslCJITb6Df5sn34bKVDpqWJ
gDFJoEYoTzajRxEDjSkwau+iPhuaJ6pB97+JimOg0Jnqe0QVZ2NtjwgpXYSGkevn
iVxZHaurLxnhDry5KyDJ79p48c7aKNxAxU2syrhKkrWNJaCg4WVTOc/eQU4elUZb
TwYIZ/Zi4gOkS+vz0ceggRmXg5MzYT6cBlccHRrA7BaSRkoD7bNbDh+mRTz67UgO
KUjIi+o1TsUhmvO2Know+zIGd1mfAf9qFT+4KXPFh3yN5DeMkQIDAQABMA0GCSqG
SIb3DQEBCwUAA4IBAQC6LAK0NnmnxuWvKZano3hI9DPRlktB4LVfYSBNFnQllxUC
ZYBIouJXFKK4dTccMkgLlQU7hFXj/YWdSRmf78w/w0GbWYAnUDnGfYro7+ZRUtFV
v7FT+xV2hFe+2cp4+btux5kfqD6OC58Gp9FWXMzRhJCWSDAk2rIYJ2MM7og+ad+Q
mJAYoOBLuY1rXc080v0Vdcl3tQ24UvvvLhuyOyL795OaZZl3uVvbaNHpM8lfJNEg
XfsbHpePEKd9ORLV6jUirl0YheqY8Mdx5hfwFHi1FL4eH6vzRm6GF2hUkkfjOUGO
x8JijLHx5rnkFyNOynhoH8QlwYeMPZbc4js7DWuG
-----END CERTIFICATE-----`

	caKeyRSA string = `-----BEGIN RSA PRIVATE KEY-----
MIIEogIBAAKCAQEAxTVd5sFGuVOZxkPv3tE69khUToVcRb85NRWWeWBSyrTE4UWr
06kG4reSTVrGgR2hojrP4nzVynu7EslCJITb6Df5sn34bKVDpqWJgDFJoEYoTzaj
RxEDjSkwau+iPhuaJ6pB97+JimOg0Jnqe0QVZ2NtjwgpXYSGkevniVxZHaurLxnh
Dry5KyDJ79p48c7aKNxAxU2syrhKkrWNJaCg4WVTOc/eQU4elUZbTwYIZ/Zi4gOk
S+vz0ceggRmXg5MzYT6cBlccHRrA7BaSRkoD7bNbDh+mRTz67UgOKUjIi+o1TsUh
mvO2Know+zIGd1mfAf9qFT+4KXPFh3yN5DeMkQIDAQABAoH/E0Ii6WX2giKn4bTA
uAG2wFZP5Vsgp68E5yo0h6Xgb+s3Tsh+/yyCf6FtqCA1QmaiYjVcF8IZHqz2l98P
loFi+Ep/F+81U2bQNHX1947Yoc44IYQ0bbw7nI1pLQg5z9biNv1pc8hApkMUcUqW
m3MKpA4RpOYnI/rNKXLgKYnbKgptyniJSyFm+pOLgpSnwZIiSIBqHnppHS1b8Bjq
H2ZYqEYNB7dHLu0HpHB1zGVC4CAzmBqtLw4fNDp1lsHTis0SJpRuBiD5IZU6+1TS
QvgkmfJKStRFIT+0YRap0J+rSYtqwPalPPbV4ePfZTpj4d9Ll0Xx97Pn3oinUQgl
auhBAoGBAOIeH5tEaj3U8DTNGGMWmMBVediJEhqDITyjPVGoKZAt+29tqwlDg+aw
hEEGgaIOPE+mQ3CEbvJnRC4Z/ntYRJpv1arBziRQKfznyb3yyFvy/JBSkNEkyFhz
KHYv/uQyg8XHIAIE41IpKdUk9MZ6BVvHjFdBbU01KerPl8Dfm7aJAoGBAN9FNFyv
A61q44oCwGRTxpYzRshHkk7GsO4Jg/vMKpU8bOCvz8GLD7cx38Xt1bV+uUlNGZc6
4EZxKrv0P9Fsj//WREc+0K11U3aN6HIYNdVV9Vel2v6Bis8+zTzNY6fSF4sx1Dw9
5q1BkE6sP3IPHz58Tt0pWNW8lmucZS7Q3KPJAoGBAJuy0GKytlFDOe+xtfQtEBuH
//GpWMzmtFEzujprB8uezf6JTnd/hOipbTf1SfgTw1W5D8D/gAHsN5djEMdQHVUW
YtNExjRc+ryJwnHIJkyiQWUDZXKN2GKHUToojGQHoJLkLVcWlIzziTmaS+4LAXuU
KT+/7op2bBmivkTx9B+5AoGBALyJxhPWPra8onSyqiCOlg3UMxuBRM18/3+jTW7e
E79+DTsXe8smURkT5rFPi739yx1ZHBkWwLj7a2jYcuO4V0lleLbpFnLDtr1QTE+8
ngkO02U2S13LqpojoFCN6G+Y/ASxCVXtt9Pqn5+v2MvKdUng0v/zoG6tGCC7Kr6D
5S3xAoGAZTYnX/rV1n2YuVvd12T9Xs2EBD9Q3FfL/oDfchU2nWiXyQA8HXhb8aBw
5Mw4BOuo0JgkSTqIxqda10tAViNeqlNiQKvOMt9y8Ugl4eEd4SdutmdTZuVPwK4/
yLi0ot+KP/8sbKAZjcAiJJIZFsqVY4wRdhSo3jzI72Zsx9CqJvE=
-----END RSA PRIVATE KEY-----`

	// caKeyPKCS8 is caKeyRSA converted to PKCS8 form.
	caKeyPKCS8 string = `-----BEGIN PRIVATE KEY-----
MIIEvAIBADANBgkqhkiG9w0BAQEFAASCBKYwggSiAgEAAoIBAQDFNV3mwUa5U5nG
Q+/e0Tr2SFROhVxFvzk1FZZ5YFLKtMThRavTqQbit5JNWsaBHaGiOs/ifNXKe7sS
yUIkhNvoN/myffhspUOmpYmAMUmgRihPNqNHEQONKTBq76I+G5onqkH3v4mKY6DQ
mep7RBVnY22PCCldhIaR6+eJXFkdq6svGeEOvLkrIMnv2njxztoo3EDFTazKuEqS
tY0loKDhZVM5z95BTh6VRltPBghn9mLiA6RL6/PRx6CBGZeDkzNhPpwGVxwdGsDs
FpJGSgPts1sOH6ZFPPrtSA4pSMiL6jVOxSGa87YqejD7MgZ3WZ8B/2oVP7gpc8WH
fI3kN4yRAgMBAAECgf8TQiLpZfaCIqfhtMC4AbbAVk/lWyCnrwTnKjSHpeBv6zdO
yH7/LIJ/oW2oIDVCZqJiNVwXwhkerPaX3w+WgWL4Sn8X7zVTZtA0dfX3jtihzjgh
hDRtvDucjWktCDnP1uI2/WlzyECmQxRxSpabcwqkDhGk5icj+s0pcuApidsqCm3K
eIlLIWb6k4uClKfBkiJIgGoeemkdLVvwGOofZlioRg0Ht0cu7QekcHXMZULgIDOY
Gq0vDh80OnWWwdOKzRImlG4GIPkhlTr7VNJC+CSZ8kpK1EUhP7RhFqnQn6tJi2rA
9qU89tXh499lOmPh30uXRfH3s+feiKdRCCVq6EECgYEA4h4fm0RqPdTwNM0YYxaY
wFV52IkSGoMhPKM9UagpkC37b22rCUOD5rCEQQaBog48T6ZDcIRu8mdELhn+e1hE
mm/VqsHOJFAp/OfJvfLIW/L8kFKQ0STIWHModi/+5DKDxccgAgTjUikp1ST0xnoF
W8eMV0FtTTUp6s+XwN+btokCgYEA30U0XK8DrWrjigLAZFPGljNGyEeSTsaw7gmD
+8wqlTxs4K/PwYsPtzHfxe3VtX65SU0ZlzrgRnEqu/Q/0WyP/9ZERz7QrXVTdo3o
chg11VX1V6Xa/oGKzz7NPM1jp9IXizHUPD3mrUGQTqw/cg8fPnxO3SlY1byWa5xl
LtDco8kCgYEAm7LQYrK2UUM577G19C0QG4f/8alYzOa0UTO6OmsHy57N/olOd3+E
6KltN/VJ+BPDVbkPwP+AAew3l2MQx1AdVRZi00TGNFz6vInCccgmTKJBZQNlco3Y
YodROiiMZAegkuQtVxaUjPOJOZpL7gsBe5QpP7/uinZsGaK+RPH0H7kCgYEAvInG
E9Y+tryidLKqII6WDdQzG4FEzXz/f6NNbt4Tv34NOxd7yyZRGRPmsU+Lvf3LHVkc
GRbAuPtraNhy47hXSWV4tukWcsO2vVBMT7yeCQ7TZTZLXcuqmiOgUI3ob5j8BLEJ
Ve230+qfn6/Yy8p1SeDS//Ogbq0YILsqvoPlLfECgYBlNidf+tXWfZi5W93XZP1e
zYQEP1DcV8v+gN9yFTadaJfJADwdeFvxoHDkzDgE66jQmCRJOojGp1rXS0BWI16q
U2JAq84y33LxSCXh4R3hJ262Z1Nm5U/Arj/IuLSi34o//yxsoBmNwCIkkhkWypVj
jBF2FKjePMjvZmzH0Kom8Q==
-----END PRIVATE KEY-----`
)
