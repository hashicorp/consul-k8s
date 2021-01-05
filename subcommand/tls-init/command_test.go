package tls_init

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
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
	ui := cli.NewMockUi()
	cmd := Command{UI: ui}
	k8s := fake.NewSimpleClientset()
	cmd.clientset = k8s

	ca, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.RemoveAll(ca.Name())
	err = ioutil.WriteFile(ca.Name(), []byte(caCert), 0644)
	require.NoError(t, err)

	key, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.RemoveAll(key.Name())
	err = ioutil.WriteFile(key.Name(), []byte(caKey), 0644)
	require.NoError(t, err)

	flags := []string{"-name-prefix", "consul", "-ca", ca.Name(), "-key", key.Name()}

	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode)

	caCertBlock, _ := pem.Decode([]byte(caCert))
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
	require.Equal(t, []net.IP{net.ParseIP("127.0.0.1").To4()}, certificate.IPAddresses)

	keyBlock, _ := pem.Decode(serverKey)
	privateKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	require.NoError(t, err)
	require.Equal(t, &privateKey.PublicKey, certificate.PublicKey)

	require.NoError(t, certificate.CheckSignatureFrom(caCertificate))
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

	ca, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.RemoveAll(ca.Name())
	err = ioutil.WriteFile(ca.Name(), []byte(caCert), 0644)
	require.NoError(t, err)

	key, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.RemoveAll(key.Name())
	err = ioutil.WriteFile(key.Name(), []byte(caKey), 0644)
	require.NoError(t, err)

	flags := []string{"-name-prefix", "consul", "-ca", ca.Name(), "-key", key.Name(), "-additional-dnsname", "test.dns.name"}

	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode)

	caCertBlock, _ := pem.Decode([]byte(caCert))
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
			corev1.TLSCertKey: []byte(caCert),
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
			corev1.TLSPrivateKeyKey: []byte(caKey),
		},
		Type: corev1.SecretTypeOpaque,
	}, metav1.CreateOptions{})
	require.NoError(t, err)

	cmd.clientset = k8s

	flags := []string{"-name-prefix", "consul"}

	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode)

	caCertBlock, _ := pem.Decode([]byte(caCert))
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
	require.Equal(t, []net.IP{net.ParseIP("127.0.0.1").To4()}, certificate.IPAddresses)

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
	require.Equal(t, []net.IP{net.ParseIP("127.0.0.1").To4()}, certificate.IPAddresses)

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
			corev1.TLSCertKey: []byte(caCert),
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
			corev1.TLSPrivateKeyKey: []byte(caKey),
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

	caCertBlock, _ := pem.Decode([]byte(caCert))
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
			corev1.TLSCertKey: []byte(caCert),
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
			corev1.TLSPrivateKeyKey: []byte(caKey),
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
	require.Equal(t, time.Now().AddDate(1, 0, 0).Unix(), certificate.NotAfter.Unix())
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
			corev1.TLSCertKey: []byte(caCert),
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
			corev1.TLSPrivateKeyKey: []byte(caKey),
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
	require.Equal(t, []net.IP{net.ParseIP(`10.0.0.1`).To4(), net.ParseIP(`127.0.0.1`).To4()}, certificate.IPAddresses)
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
			corev1.TLSCertKey: []byte(caCert),
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
			corev1.TLSPrivateKeyKey: []byte(caKey),
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
			corev1.TLSCertKey: []byte(caCert),
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
			corev1.TLSPrivateKeyKey: []byte(caKey),
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
	caCert string = `-----BEGIN CERTIFICATE-----
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

	caKey string = `-----BEGIN EC PRIVATE KEY-----
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
)
