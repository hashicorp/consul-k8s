// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package preset

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/hcp-sdk-go/clients/cloud-global-network-manager-service/preview/2022-02-15/models"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/yaml"
)

const (
	hcpClientID                                    = "RAxJflDbxDXw8kLY6jWmwqMz3kVe7NnL"
	hcpClientSecret                                = "1fNzurLatQPLPwf7jnD4fRtU9f5nH31RKBHayy08uQ6P-6nwI1rFZjMXb4m3cCKH"
	observabilityHCPClientId                       = "fake-client-id"
	observabilityHCPClientSecret                   = "fake-client-secret"
	hcpResourceID                                  = "organization/ccbdd191-5dc3-4a73-9e05-6ac30ca67992/project/36019e0d-ed59-4df6-9990-05bb7fc793b6/hashicorp.consul.global-network-manager.cluster/prod-on-prem"
	expectedSecretNameHCPClientId                  = "consul-hcp-client-id"
	expectedSecretNameHCPClientSecret              = "consul-hcp-client-secret"
	expectedSecretNameHCPObservabilityClientId     = "consul-hcp-observability-client-id"
	expectedSecretNameHCPObservabilityClientSecret = "consul-hcp-observability-client-secret"
	expectedSecretNameHCPResourceId                = "consul-hcp-resource-id"
	expectedSecretNameHCPAuthURL                   = "consul-hcp-auth-url"
	expectedSecretNameHCPApiHostname               = "consul-hcp-api-host"
	expectedSecretNameHCPScadaAddress              = "consul-hcp-scada-address"
	expectedSecretNameGossipKey                    = "consul-gossip-key"
	expectedSecretNameBootstrap                    = "consul-bootstrap-token"
	expectedSecretNameServerCA                     = "consul-server-ca"
	expectedSecretNameServerCert                   = "consul-server-cert"
	namespace                                      = "consul"
	validResponse                                  = `
{
	"cluster":
	{
		"id": "Dc1",
		"bootstrap_expect" : 3
	},
	"bootstrap":
	{
		"gossip_key": "Wa6/XFAnYy0f9iqVH2iiG+yore3CqHSemUy4AIVTa/w=",
		"server_tls": {
			"certificate_authorities": [
				"-----BEGIN CERTIFICATE-----\nMIIC6TCCAo+gAwIBAgIQA3pUmJcy9uw8MNIDZPiaZjAKBggqhkjOPQQDAjCBtzEL\nMAkGA1UEBhMCVVMxCzAJBgNVBAgTAkNBMRYwFAYDVQQHEw1TYW4gRnJhbmNpc2Nv\nMRowGAYDVQQJExExMDEgU2Vjb25kIFN0cmVldDEOMAwGA1UEERMFOTQxMDUxFzAV\nBgNVBAoTDkhhc2hpQ29ycCBJbmMuMT4wPAYDVQQDEzVDb25zdWwgQWdlbnQgQ0Eg\nNDYyMjg2MDAxNTk3NzI1NDMzMTgxNDQ4OTAzODMyNjg5NzI1NDAeFw0yMjAzMjkx\nMTEyNDNaFw0yNzAzMjgxMTEyNDNaMIG3MQswCQYDVQQGEwJVUzELMAkGA1UECBMC\nQ0ExFjAUBgNVBAcTDVNhbiBGcmFuY2lzY28xGjAYBgNVBAkTETEwMSBTZWNvbmQg\nU3RyZWV0MQ4wDAYDVQQREwU5NDEwNTEXMBUGA1UEChMOSGFzaGlDb3JwIEluYy4x\nPjA8BgNVBAMTNUNvbnN1bCBBZ2VudCBDQSA0NjIyODYwMDE1OTc3MjU0MzMxODE0\nNDg5MDM4MzI2ODk3MjU0MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAERs73JA+K\n9xMorTz6fA5x8Dmin6l8pNgka3/Ye3SFWJD/0lKFTXEX7Li8+hXG31WMLdXgoWHS\nkL1HoLboV8hEAKN7MHkwDgYDVR0PAQH/BAQDAgGGMA8GA1UdEwEB/wQFMAMBAf8w\nKQYDVR0OBCIEICst9kpfDK0LtEbUghWf4ahjpzd7Mlh07OLT/e38PKDmMCsGA1Ud\nIwQkMCKAICst9kpfDK0LtEbUghWf4ahjpzd7Mlh07OLT/e38PKDmMAoGCCqGSM49\nBAMCA0gAMEUCIQCuk/n49np4m76jTFLk2zeiSi7UfubMeS2BD4bkMt6v/wIgbO0R\npTqCOYQr3cji1EpEQca95VCZ26lBEjqLQF3osGc=\n-----END CERTIFICATE-----\n"
			  ],
			  "private_key": "-----BEGIN EC PRIVATE KEY-----\nMHcCAQEEIA+DFWCFz+SujFCuWM3GpoTLPX8igerwMw+8efNbx7a+oAoGCCqGSM49\nAwEHoUQDQgAE7LdWJpna88mohlnuTyGJ+WZ3P6BCxGqBRWNJn3+JEoHhmaifx7Sq\nWLMCEB1UNbH5Z1esaS4h33Gb0pyyiCy19A==\n-----END EC PRIVATE KEY-----\n",
			  "cert": "-----BEGIN CERTIFICATE-----\nMIICmzCCAkGgAwIBAgIRAKZ77a2h+plK2yXFsW0kfgAwCgYIKoZIzj0EAwIwgbcx\nCzAJBgNVBAYTAlVTMQswCQYDVQQIEwJDQTEWMBQGA1UEBxMNU2FuIEZyYW5jaXNj\nbzEaMBgGA1UECRMRMTAxIFNlY29uZCBTdHJlZXQxDjAMBgNVBBETBTk0MTA1MRcw\nFQYDVQQKEw5IYXNoaUNvcnAgSW5jLjE+MDwGA1UEAxM1Q29uc3VsIEFnZW50IENB\nIDQ2MjI4NjAwMTU5NzcyNTQzMzE4MTQ0ODkwMzgzMjY4OTcyNTQwHhcNMjIwMzI5\nMTExMjUwWhcNMjMwMzI5MTExMjUwWjAcMRowGAYDVQQDExFzZXJ2ZXIuZGMxLmNv\nbnN1bDBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABOy3ViaZ2vPJqIZZ7k8hiflm\ndz+gQsRqgUVjSZ9/iRKB4Zmon8e0qlizAhAdVDWx+WdXrGkuId9xm9KcsogstfSj\ngccwgcQwDgYDVR0PAQH/BAQDAgWgMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEF\nBQcDAjAMBgNVHRMBAf8EAjAAMCkGA1UdDgQiBCDaH9x1CRRqM5BYCMKBnAFyZjQq\nSY9IcJnhZUZIIJHU4jArBgNVHSMEJDAigCArLfZKXwytC7RG1IIVn+GoY6c3ezJY\ndOzi0/3t/Dyg5jAtBgNVHREEJjAkghFzZXJ2ZXIuZGMxLmNvbnN1bIIJbG9jYWxo\nb3N0hwR/AAABMAoGCCqGSM49BAMCA0gAMEUCIQCOxQHGF2483Cdd9nXcqAoOcxYP\nIqNP/WM03qyERyYNNQIgbtFBLIAgrhdXdjEvHMjU5ceHSwle/K0p0OTSIwSk8xI=\n-----END CERTIFICATE-----\n"
		},
		"consul_config": "{\"acl\":{\"default_policy\":\"deny\",\"enable_token_persistence\":true,\"enabled\":true,\"tokens\":{\"agent\":\"74044c72-03c8-42b0-b57f-728bb22ca7fb\",\"initial_management\":\"74044c72-03c8-42b0-b57f-728bb22ca7fb\"}},\"auto_encrypt\":{\"allow_tls\":true},\"bootstrap_expect\":1,\"encrypt\":\"yUPhgtteok1/bHoVIoRnJMfOrKrb1TDDyWJRh9rlUjg=\",\"encrypt_verify_incoming\":true,\"encrypt_verify_outgoing\":true,\"ports\":{\"http\":-1,\"https\":8501},\"retry_join\":[],\"verify_incoming\":true,\"verify_outgoing\":true,\"verify_server_hostname\":true}"
	}
}`
	observabilityResponse = `
{
	"id": "Dc1",
	"location": {
		"organization_id": "abc123",
		"project_id": "123abc"
	},
	"keys": [
		{
			"created_at":"",
			"client_id": "fake-client-id",
			"client_secret": "fake-client-secret"
		}
	]
}
`
)

var validBootstrapReponse *models.HashicorpCloudGlobalNetworkManager20220215AgentBootstrapResponse = &models.HashicorpCloudGlobalNetworkManager20220215AgentBootstrapResponse{
	Bootstrap: &models.HashicorpCloudGlobalNetworkManager20220215ClusterBootstrap{
		ID:              "Dc1",
		GossipKey:       "Wa6/XFAnYy0f9iqVH2iiG+yore3CqHSemUy4AIVTa/w=",
		BootstrapExpect: 3,
		ServerTLS: &models.HashicorpCloudGlobalNetworkManager20220215ServerTLS{
			CertificateAuthorities: []string{"-----BEGIN CERTIFICATE-----\nMIIC6TCCAo+gAwIBAgIQA3pUmJcy9uw8MNIDZPiaZjAKBggqhkjOPQQDAjCBtzEL\nMAkGA1UEBhMCVVMxCzAJBgNVBAgTAkNBMRYwFAYDVQQHEw1TYW4gRnJhbmNpc2Nv\nMRowGAYDVQQJExExMDEgU2Vjb25kIFN0cmVldDEOMAwGA1UEERMFOTQxMDUxFzAV\nBgNVBAoTDkhhc2hpQ29ycCBJbmMuMT4wPAYDVQQDEzVDb25zdWwgQWdlbnQgQ0Eg\nNDYyMjg2MDAxNTk3NzI1NDMzMTgxNDQ4OTAzODMyNjg5NzI1NDAeFw0yMjAzMjkx\nMTEyNDNaFw0yNzAzMjgxMTEyNDNaMIG3MQswCQYDVQQGEwJVUzELMAkGA1UECBMC\nQ0ExFjAUBgNVBAcTDVNhbiBGcmFuY2lzY28xGjAYBgNVBAkTETEwMSBTZWNvbmQg\nU3RyZWV0MQ4wDAYDVQQREwU5NDEwNTEXMBUGA1UEChMOSGFzaGlDb3JwIEluYy4x\nPjA8BgNVBAMTNUNvbnN1bCBBZ2VudCBDQSA0NjIyODYwMDE1OTc3MjU0MzMxODE0\nNDg5MDM4MzI2ODk3MjU0MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAERs73JA+K\n9xMorTz6fA5x8Dmin6l8pNgka3/Ye3SFWJD/0lKFTXEX7Li8+hXG31WMLdXgoWHS\nkL1HoLboV8hEAKN7MHkwDgYDVR0PAQH/BAQDAgGGMA8GA1UdEwEB/wQFMAMBAf8w\nKQYDVR0OBCIEICst9kpfDK0LtEbUghWf4ahjpzd7Mlh07OLT/e38PKDmMCsGA1Ud\nIwQkMCKAICst9kpfDK0LtEbUghWf4ahjpzd7Mlh07OLT/e38PKDmMAoGCCqGSM49\nBAMCA0gAMEUCIQCuk/n49np4m76jTFLk2zeiSi7UfubMeS2BD4bkMt6v/wIgbO0R\npTqCOYQr3cji1EpEQca95VCZ26lBEjqLQF3osGc=\n-----END CERTIFICATE-----\n"},
			PrivateKey:             "-----BEGIN EC PRIVATE KEY-----\nMHcCAQEEIA+DFWCFz+SujFCuWM3GpoTLPX8igerwMw+8efNbx7a+oAoGCCqGSM49\nAwEHoUQDQgAE7LdWJpna88mohlnuTyGJ+WZ3P6BCxGqBRWNJn3+JEoHhmaifx7Sq\nWLMCEB1UNbH5Z1esaS4h33Gb0pyyiCy19A==\n-----END EC PRIVATE KEY-----\n",
			Cert:                   "-----BEGIN CERTIFICATE-----\nMIICmzCCAkGgAwIBAgIRAKZ77a2h+plK2yXFsW0kfgAwCgYIKoZIzj0EAwIwgbcx\nCzAJBgNVBAYTAlVTMQswCQYDVQQIEwJDQTEWMBQGA1UEBxMNU2FuIEZyYW5jaXNj\nbzEaMBgGA1UECRMRMTAxIFNlY29uZCBTdHJlZXQxDjAMBgNVBBETBTk0MTA1MRcw\nFQYDVQQKEw5IYXNoaUNvcnAgSW5jLjE+MDwGA1UEAxM1Q29uc3VsIEFnZW50IENB\nIDQ2MjI4NjAwMTU5NzcyNTQzMzE4MTQ0ODkwMzgzMjY4OTcyNTQwHhcNMjIwMzI5\nMTExMjUwWhcNMjMwMzI5MTExMjUwWjAcMRowGAYDVQQDExFzZXJ2ZXIuZGMxLmNv\nbnN1bDBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABOy3ViaZ2vPJqIZZ7k8hiflm\ndz+gQsRqgUVjSZ9/iRKB4Zmon8e0qlizAhAdVDWx+WdXrGkuId9xm9KcsogstfSj\ngccwgcQwDgYDVR0PAQH/BAQDAgWgMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEF\nBQcDAjAMBgNVHRMBAf8EAjAAMCkGA1UdDgQiBCDaH9x1CRRqM5BYCMKBnAFyZjQq\nSY9IcJnhZUZIIJHU4jArBgNVHSMEJDAigCArLfZKXwytC7RG1IIVn+GoY6c3ezJY\ndOzi0/3t/Dyg5jAtBgNVHREEJjAkghFzZXJ2ZXIuZGMxLmNvbnN1bIIJbG9jYWxo\nb3N0hwR/AAABMAoGCCqGSM49BAMCA0gAMEUCIQCOxQHGF2483Cdd9nXcqAoOcxYP\nIqNP/WM03qyERyYNNQIgbtFBLIAgrhdXdjEvHMjU5ceHSwle/K0p0OTSIwSk8xI=\n-----END CERTIFICATE-----\n"},
		ConsulConfig: "{\"acl\":{\"default_policy\":\"deny\",\"enable_token_persistence\":true,\"enabled\":true,\"tokens\":{\"agent\":\"74044c72-03c8-42b0-b57f-728bb22ca7fb\",\"initial_management\":\"74044c72-03c8-42b0-b57f-728bb22ca7fb\"}},\"auto_encrypt\":{\"allow_tls\":true},\"bootstrap_expect\":1,\"encrypt\":\"yUPhgtteok1/bHoVIoRnJMfOrKrb1TDDyWJRh9rlUjg=\",\"encrypt_verify_incoming\":true,\"encrypt_verify_outgoing\":true,\"ports\":{\"http\":-1,\"https\":8501},\"retry_join\":[],\"verify_incoming\":true,\"verify_outgoing\":true,\"verify_server_hostname\":true}",
	},
	Cluster: &models.HashicorpCloudGlobalNetworkManager20220215Cluster{
		ID:              "dc1",
		BootstrapExpect: 3,
	},
}

var hcpConfig *HCPConfig = &HCPConfig{
	ResourceID:                hcpResourceID,
	ClientID:                  hcpClientID,
	ClientSecret:              hcpClientSecret,
	AuthURL:                   "https://foobar",
	APIHostname:               "https://foo.bar",
	ScadaAddress:              "10.10.10.10",
	ObservabilityClientID:     observabilityHCPClientId,
	ObservabilityClientSecret: observabilityHCPClientSecret,
}

var validBootstrapConfig *CloudBootstrapConfig = &CloudBootstrapConfig{
	HCPConfig: *hcpConfig,
	ConsulConfig: ConsulConfig{
		ACL: ACL{
			Tokens: Tokens{
				Agent:             "74044c72-03c8-42b0-b57f-728bb22ca7fb",
				InitialManagement: "74044c72-03c8-42b0-b57f-728bb22ca7fb",
			},
		},
	},
	BootstrapResponse: validBootstrapReponse,
}

func TestGetValueMap(t *testing.T) {
	// Create fake k8s.
	k8s := fake.NewClientset()
	namespace := "consul"

	// Start the mock HCP server.
	hcpMockServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		if r != nil && r.Method == "GET" {
			switch r.URL.Path {
			case "/global-network-manager/2022-02-15/organizations/ccbdd191-5dc3-4a73-9e05-6ac30ca67992/projects/36019e0d-ed59-4df6-9990-05bb7fc793b6/clusters/prod-on-prem/agent/bootstrap_config":
				w.Write([]byte(validResponse))
			case "/global-network-manager/2022-02-15/organizations/ccbdd191-5dc3-4a73-9e05-6ac30ca67992/projects/36019e0d-ed59-4df6-9990-05bb7fc793b6/clusters/prod-on-prem/credentials/observability":
				w.Write([]byte(observabilityResponse))
			default:
				w.Write([]byte(`
				{
					"access_token": "dummy-token"
				}
				`))
			}
		} else {
			w.Write([]byte(`
			{
				"access_token": "dummy-token"
			}
			`))
		}
	}))
	hcpMockServer.StartTLS()
	t.Cleanup(hcpMockServer.Close)
	mockServerURL, err := url.Parse(hcpMockServer.URL)
	require.NoError(t, err)
	os.Setenv("HCP_AUTH_URL", hcpMockServer.URL)
	os.Setenv("HCP_API_HOST", mockServerURL.Host)
	os.Setenv("HCP_CLIENT_ID", "fGY34fkOxcQmpkcygQmGHQZkEcLDhBde")
	os.Setenv("HCP_CLIENT_SECRET", "8EWngREObMe90HNDN6oQv3YKQlRtVkg-28AgZylz1en0DHwyiE2pYCbwi61oF8dr")
	bsConfig := getDeepCopyOfValidBootstrapConfig()
	bsConfig.HCPConfig.APIHostname = mockServerURL.Host
	bsConfig.HCPConfig.AuthURL = hcpMockServer.URL

	testCases := []struct {
		description        string
		installer          *CloudPreset
		postProcessingFunc func()
		requireCheck       func()
	}{
		{
			"Should save secrets when SkipSavingSecrets is false.",
			&CloudPreset{
				HCPConfig:           &bsConfig.HCPConfig,
				KubernetesClient:    k8s,
				KubernetesNamespace: namespace,
				UI:                  terminal.NewBasicUI(context.Background()),
				HTTPClient:          hcpMockServer.Client(),
				Context:             context.Background(),
			},
			func() {
				deleteSecrets(k8s)
			},
			func() {
				checkAllSecretsWereSaved(t, k8s, bsConfig)
			},
		},
		{
			"Should not save secrets when SkipSavingSecrets is true.",
			&CloudPreset{
				HCPConfig:           &bsConfig.HCPConfig,
				KubernetesClient:    k8s,
				KubernetesNamespace: namespace,
				UI:                  terminal.NewBasicUI(context.Background()),
				SkipSavingSecrets:   true,
				HTTPClient:          hcpMockServer.Client(),
				Context:             context.Background(),
			},
			func() {
				deleteSecrets(k8s)
			},
			func() {
				checkAllSecretsWereSaved(t, k8s, bsConfig)
			},
		},
		{
			"Should not save save api-hostname, scada-address, or auth-url keys as empty strings if they are not configured.",
			&CloudPreset{
				HCPConfig: &HCPConfig{
					ResourceID:   hcpResourceID,
					ClientID:     hcpClientID,
					ClientSecret: hcpClientSecret,
				},
				KubernetesClient:    k8s,
				KubernetesNamespace: namespace,
				UI:                  terminal.NewBasicUI(context.Background()),
				SkipSavingSecrets:   false,
				HTTPClient:          hcpMockServer.Client(),
				Context:             context.Background(),
			},
			func() {
				deleteSecrets(k8s)
			},
			func() {
				// Check the hcp resource id secret is as expected.
				ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPResourceID, secretKeyHCPResourceID,
					bsConfig.HCPConfig.ResourceID, corev1.SecretTypeOpaque)

				// Check the hcp client id secret is as expected.
				ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPClientID, secretKeyHCPClientID,
					bsConfig.HCPConfig.ClientID, corev1.SecretTypeOpaque)

				// Check the hcp client secret secret is as expected.
				ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPClientSecret, secretKeyHCPClientSecret,
					bsConfig.HCPConfig.ClientSecret, corev1.SecretTypeOpaque)

				// Check the observability hcp client id secret is as expected.
				ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPObservabilityClientID, secretKeyHCPClientID,
					bsConfig.HCPConfig.ObservabilityClientID, corev1.SecretTypeOpaque)

				// Check the observability hcp client secret secret is as expected.
				ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPObservabilityClientSecret, secretKeyHCPClientSecret,
					bsConfig.HCPConfig.ObservabilityClientSecret, corev1.SecretTypeOpaque)

				// Check the bootstrap token secret is as expected.
				ensureSecretKeyValueMatchesExpected(t, k8s, secretNameBootstrapToken, secretKeyBootstrapToken,
					bsConfig.ConsulConfig.ACL.Tokens.InitialManagement, corev1.SecretTypeOpaque)

				// Check the gossip key secret is as expected.
				ensureSecretKeyValueMatchesExpected(t, k8s, secretNameGossipKey, secretKeyGossipKey,
					bsConfig.BootstrapResponse.Bootstrap.GossipKey, corev1.SecretTypeOpaque)

				// Check the server cert secret is as expected.
				ensureSecretKeyValueMatchesExpected(t, k8s, secretNameServerCert, corev1.TLSCertKey,
					bsConfig.BootstrapResponse.Bootstrap.ServerTLS.Cert, corev1.SecretTypeTLS)
				ensureSecretKeyValueMatchesExpected(t, k8s, secretNameServerCert, corev1.TLSPrivateKeyKey,
					bsConfig.BootstrapResponse.Bootstrap.ServerTLS.PrivateKey, corev1.SecretTypeTLS)

				// Check the server CA secret is as expected.
				ensureSecretKeyValueMatchesExpected(t, k8s, secretNameServerCA, corev1.TLSCertKey,
					bsConfig.BootstrapResponse.Bootstrap.ServerTLS.CertificateAuthorities[0], corev1.SecretTypeOpaque)

				// Check that HCP scada address, auth url, and api hostname are not saved
				hcpAuthURLSecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameHCPAuthURL, metav1.GetOptions{})
				require.Nil(t, hcpAuthURLSecret.Data)
				hcpApiHostnameSecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameHCPAPIHostname, metav1.GetOptions{})
				require.Nil(t, hcpApiHostnameSecret.Data)
				hcpScadaAddress, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameHCPScadaAddress, metav1.GetOptions{})
				require.Nil(t, hcpScadaAddress.Data)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			config, err := tc.installer.GetValueMap()
			require.NoError(t, err)
			require.NotNil(t, config)
			if tc.installer.SkipSavingSecrets {
				checkSecretsWereNotSaved(k8s)
			} else {
				tc.requireCheck()
			}
			tc.postProcessingFunc()
		})
	}
	os.Unsetenv("HCP_AUTH_URL")
	os.Unsetenv("HCP_API_HOST")
	os.Unsetenv("HCP_CLIENT_ID")
	os.Unsetenv("HCP_CLIENT_SECRET")
}

// TestParseBootstrapConfigResponse tests that response string from agent bootstrap
// config endpoint can be converted into CloudBootstrapConfig bootstrap object.
func TestParseBootstrapConfigResponse(t *testing.T) {
	testCases := []struct {
		description    string
		input          string
		expectedConfig *CloudBootstrapConfig
	}{
		{
			"Should properly parse a valid response.",
			validResponse,
			validBootstrapConfig,
		},
	}

	cloudPreset := &CloudPreset{
		HCPConfig:           hcpConfig,
		KubernetesNamespace: namespace,
		UI:                  terminal.NewBasicUI(context.Background()),
	}
	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			config, err := cloudPreset.parseBootstrapConfigResponse(validBootstrapReponse)
			require.NoError(t, err)
			require.Equal(t, tc.expectedConfig, config)
		})
	}
}

func TestSaveSecretsFromBootstrapConfig(t *testing.T) {
	t.Parallel()

	// Create fake k8s.
	k8s := fake.NewClientset()

	testCases := []struct {
		description          string
		expectsError         bool
		expectedErrorMessage string
		preProcessingFunc    func()
		postProcessingFunc   func()
	}{
		{
			"Properly saves secrets with a full bootstrapConfig.",
			false,
			"",
			func() {},
			func() {
				deleteSecrets(k8s)
			},
		},
		{
			"Errors when hcp client id secret already exists",
			true,
			fmt.Sprintf("'%s' secret in '%s' namespace already exists", expectedSecretNameHCPClientId, namespace),
			func() {
				savePlaceholderSecret(expectedSecretNameHCPClientId, k8s)
			},
			func() {
				deleteSecrets(k8s)
			},
		},
		{
			"Errors when hcp client secret secret already exists",
			true,
			fmt.Sprintf("'%s' secret in '%s' namespace already exists", expectedSecretNameHCPClientSecret, namespace),
			func() {
				savePlaceholderSecret(expectedSecretNameHCPClientSecret, k8s)
			},
			func() {
				deleteSecrets(k8s)
			},
		},
		{
			"Errors when hcp resource id secret already exists",
			true,
			fmt.Sprintf("'%s' secret in '%s' namespace already exists", expectedSecretNameHCPResourceId, namespace),
			func() {
				savePlaceholderSecret(expectedSecretNameHCPResourceId, k8s)
			},
			func() {
				deleteSecrets(k8s)
			},
		},
		{
			"Errors when hcp auth url secret already exists",
			true,
			fmt.Sprintf("'%s' secret in '%s' namespace already exists", expectedSecretNameHCPAuthURL, namespace),
			func() {
				savePlaceholderSecret(expectedSecretNameHCPAuthURL, k8s)
			},
			func() {
				deleteSecrets(k8s)
			},
		},
		{
			"Errors when hcp api hostname secret already exists",
			true,
			fmt.Sprintf("'%s' secret in '%s' namespace already exists", expectedSecretNameHCPApiHostname, namespace),
			func() {
				savePlaceholderSecret(expectedSecretNameHCPApiHostname, k8s)
			},
			func() {
				deleteSecrets(k8s)
			},
		},
		{
			"Errors when hcp scada address secret already exists",
			true,
			fmt.Sprintf("'%s' secret in '%s' namespace already exists", expectedSecretNameHCPScadaAddress, namespace),
			func() {
				savePlaceholderSecret(expectedSecretNameHCPScadaAddress, k8s)
			},
			func() {
				deleteSecrets(k8s)
			},
		},
		{
			"Errors when bootstrap token secret already exists",
			true,
			fmt.Sprintf("'%s' secret in '%s' namespace already exists", expectedSecretNameBootstrap, namespace),
			func() {
				savePlaceholderSecret(expectedSecretNameBootstrap, k8s)
			},
			func() {
				deleteSecrets(k8s)
			},
		},
		{
			"Errors when gossip key secret already exists",
			true,
			fmt.Sprintf("'%s' secret in '%s' namespace already exists", expectedSecretNameGossipKey, namespace),
			func() {
				savePlaceholderSecret(expectedSecretNameGossipKey, k8s)
			},
			func() {
				deleteSecrets(k8s)
			},
		},
		{
			"Errors when server cert secret already exists",
			true,
			fmt.Sprintf("'%s' secret in '%s' namespace already exists", expectedSecretNameServerCert, namespace),
			func() {
				savePlaceholderSecret(expectedSecretNameServerCert, k8s)
			},
			func() {
				deleteSecrets(k8s)
			},
		},
		{
			"Errors when server CA secret already exists",
			true,
			fmt.Sprintf("'%s' secret in '%s' namespace already exists", expectedSecretNameServerCA, namespace),
			func() {
				savePlaceholderSecret(expectedSecretNameServerCA, k8s)
			},
			func() {
				deleteSecrets(k8s)
			},
		},
	}
	cloudPreset := &CloudPreset{
		HCPConfig:           hcpConfig,
		KubernetesClient:    k8s,
		KubernetesNamespace: namespace,
		UI:                  terminal.NewBasicUI(context.Background()),
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			tc.preProcessingFunc()
			err := cloudPreset.saveSecretsFromBootstrapConfig(validBootstrapConfig)
			if tc.expectsError && err != nil {
				require.Equal(t, tc.expectedErrorMessage, err.Error())

			} else {
				require.NoError(t, err)
				require.Equal(t, expectedSecretNameBootstrap, secretNameBootstrapToken)
				require.Equal(t, expectedSecretNameGossipKey, secretNameGossipKey)
				require.Equal(t, expectedSecretNameHCPClientId, secretNameHCPClientID)
				require.Equal(t, expectedSecretNameHCPClientSecret, secretNameHCPClientSecret)
				require.Equal(t, expectedSecretNameHCPResourceId, secretNameHCPResourceID)
				require.Equal(t, expectedSecretNameServerCA, secretNameServerCA)
				require.Equal(t, expectedSecretNameServerCert, secretNameServerCert)

				checkAllSecretsWereSaved(t, k8s, validBootstrapConfig)

			}
			tc.postProcessingFunc()
		})
	}

}

func TestGetHelmConfigWithMapSecretNames(t *testing.T) {
	t.Parallel()

	const expectedFull = `connectInject:
  enabled: true
controller:
  enabled: true
global:
  acls:
    bootstrapToken:
      secretKey: token
      secretName: consul-bootstrap-token
    manageSystemACLs: true
  cloud:
    apiHost:
      secretKey: api-hostname
      secretName: consul-hcp-api-host
    authUrl:
      secretKey: auth-url
      secretName: consul-hcp-auth-url
    clientId:
      secretKey: client-id
      secretName: consul-hcp-client-id
    clientSecret:
      secretKey: client-secret
      secretName: consul-hcp-client-secret
    enabled: true
    resourceId:
      secretKey: resource-id
      secretName: consul-hcp-resource-id
    scadaAddress:
      secretKey: scada-address
      secretName: consul-hcp-scada-address
  datacenter: dc1
  gossipEncryption:
    secretKey: key
    secretName: consul-gossip-key
  metrics:
    enableTelemetryCollector: true
  tls:
    caCert:
      secretKey: tls.crt
      secretName: consul-server-ca
    enableAutoEncrypt: true
    enabled: true
server:
  affinity: null
  replicas: 3
  serverCert:
    secretName: consul-server-cert
telemetryCollector:
  cloud:
    clientId:
      secretKey: client-id
      secretName: consul-hcp-observability-client-id
    clientSecret:
      secretKey: client-secret
      secretName: consul-hcp-observability-client-secret
  enabled: true
`

	const expectedWithoutOptional = `connectInject:
  enabled: true
controller:
  enabled: true
global:
  acls:
    bootstrapToken:
      secretKey: token
      secretName: consul-bootstrap-token
    manageSystemACLs: true
  cloud:
    clientId:
      secretKey: client-id
      secretName: consul-hcp-client-id
    clientSecret:
      secretKey: client-secret
      secretName: consul-hcp-client-secret
    enabled: true
    resourceId:
      secretKey: resource-id
      secretName: consul-hcp-resource-id
  datacenter: dc1
  gossipEncryption:
    secretKey: key
    secretName: consul-gossip-key
  metrics:
    enableTelemetryCollector: true
  tls:
    caCert:
      secretKey: tls.crt
      secretName: consul-server-ca
    enableAutoEncrypt: true
    enabled: true
server:
  affinity: null
  replicas: 3
  serverCert:
    secretName: consul-server-cert
telemetryCollector:
  cloud:
    clientId:
      secretKey: client-id
      secretName: consul-hcp-observability-client-id
    clientSecret:
      secretKey: client-secret
      secretName: consul-hcp-observability-client-secret
  enabled: true
`

	const expectedWithoutObservability = `connectInject:
  enabled: true
controller:
  enabled: true
global:
  acls:
    bootstrapToken:
      secretKey: token
      secretName: consul-bootstrap-token
    manageSystemACLs: true
  cloud:
    clientId:
      secretKey: client-id
      secretName: consul-hcp-client-id
    clientSecret:
      secretKey: client-secret
      secretName: consul-hcp-client-secret
    enabled: true
    resourceId:
      secretKey: resource-id
      secretName: consul-hcp-resource-id
  datacenter: dc1
  gossipEncryption:
    secretKey: key
    secretName: consul-gossip-key
  metrics:
    enableTelemetryCollector: true
  tls:
    caCert:
      secretKey: tls.crt
      secretName: consul-server-ca
    enableAutoEncrypt: true
    enabled: true
server:
  affinity: null
  replicas: 3
  serverCert:
    secretName: consul-server-cert
telemetryCollector:
  cloud:
    clientId:
      secretKey: client-id
      secretName: consul-hcp-client-id
    clientSecret:
      secretKey: client-secret
      secretName: consul-hcp-client-secret
  enabled: true
`

	cloudPreset := &CloudPreset{}

	testCases := map[string]struct {
		config       *CloudBootstrapConfig
		expectedYaml string
	}{
		"Config_including_optional_parameters_with_mixedcase_DC": {
			&CloudBootstrapConfig{
				BootstrapResponse: &models.HashicorpCloudGlobalNetworkManager20220215AgentBootstrapResponse{
					Cluster: &models.HashicorpCloudGlobalNetworkManager20220215Cluster{
						BootstrapExpect: 3,
						ID:              "Dc1",
					},
				},
				HCPConfig: HCPConfig{
					ResourceID:                "consul-hcp-resource-id",
					ClientID:                  "consul-hcp-client-id",
					ClientSecret:              "consul-hcp-client-secret",
					AuthURL:                   "consul-hcp-auth-url",
					APIHostname:               "consul-hcp-api-host",
					ScadaAddress:              "consul-hcp-scada-address",
					ObservabilityClientID:     "consul-hcp-observability-client-id",
					ObservabilityClientSecret: "consul-hcp-observability-client-secret",
				},
			},
			expectedFull,
		},
		"Config_including_optional_parameters": {
			&CloudBootstrapConfig{
				BootstrapResponse: &models.HashicorpCloudGlobalNetworkManager20220215AgentBootstrapResponse{
					Cluster: &models.HashicorpCloudGlobalNetworkManager20220215Cluster{
						BootstrapExpect: 3,
						ID:              "dc1",
					},
				},
				HCPConfig: HCPConfig{
					ResourceID:                "consul-hcp-resource-id",
					ClientID:                  "consul-hcp-client-id",
					ClientSecret:              "consul-hcp-client-secret",
					AuthURL:                   "consul-hcp-auth-url",
					APIHostname:               "consul-hcp-api-host",
					ScadaAddress:              "consul-hcp-scada-address",
					ObservabilityClientID:     "consul-hcp-observability-client-id",
					ObservabilityClientSecret: "consul-hcp-observability-client-secret",
				},
			},
			expectedFull,
		},
		"Config_without_optional_parameters": {
			&CloudBootstrapConfig{
				BootstrapResponse: &models.HashicorpCloudGlobalNetworkManager20220215AgentBootstrapResponse{
					Cluster: &models.HashicorpCloudGlobalNetworkManager20220215Cluster{
						BootstrapExpect: 3,
						ID:              "dc1",
					},
				},
				HCPConfig: HCPConfig{
					ResourceID:                "consul-hcp-resource-id",
					ClientID:                  "consul-hcp-client-id",
					ClientSecret:              "consul-hcp-client-secret",
					ObservabilityClientID:     "consul-hcp-observability-client-id",
					ObservabilityClientSecret: "consul-hcp-observability-client-secret",
				},
			},
			expectedWithoutOptional,
		},
		"Config_without_observability_parameters": {
			&CloudBootstrapConfig{
				BootstrapResponse: &models.HashicorpCloudGlobalNetworkManager20220215AgentBootstrapResponse{
					Cluster: &models.HashicorpCloudGlobalNetworkManager20220215Cluster{
						BootstrapExpect: 3,
						ID:              "dc1",
					},
				},
				HCPConfig: HCPConfig{
					ResourceID:   "consul-hcp-resource-id",
					ClientID:     "consul-hcp-client-id",
					ClientSecret: "consul-hcp-client-secret",
				},
			},
			expectedWithoutObservability,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			cloudHelmValues := cloudPreset.getHelmConfigWithMapSecretNames(tc.config)
			require.NotNil(t, cloudHelmValues)
			valuesYaml, err := yaml.Marshal(cloudHelmValues)
			yml := string(valuesYaml)
			require.NoError(t, err)
			require.Equal(t, tc.expectedYaml, yml)
		})
	}

}

func savePlaceholderSecret(secretName string, k8sClient kubernetes.Interface) {
	data := map[string][]byte{}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels:    map[string]string{common.CLILabelKey: common.CLILabelValue},
		},
		Data: data,
		Type: corev1.SecretTypeOpaque,
	}
	k8sClient.CoreV1().Secrets(namespace).Create(context.Background(), secret, metav1.CreateOptions{})
}

func deleteSecrets(k8sClient kubernetes.Interface) {
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameHCPClientId, metav1.DeleteOptions{})
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameHCPClientSecret, metav1.DeleteOptions{})
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameHCPObservabilityClientId, metav1.DeleteOptions{})
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameHCPObservabilityClientSecret, metav1.DeleteOptions{})
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameHCPResourceId, metav1.DeleteOptions{})
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameHCPAuthURL, metav1.DeleteOptions{})
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameHCPApiHostname, metav1.DeleteOptions{})
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameHCPScadaAddress, metav1.DeleteOptions{})
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameBootstrap, metav1.DeleteOptions{})
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameGossipKey, metav1.DeleteOptions{})
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameServerCert, metav1.DeleteOptions{})
	k8sClient.CoreV1().Secrets(namespace).Delete(context.Background(), expectedSecretNameServerCA, metav1.DeleteOptions{})
}

func checkAllSecretsWereSaved(t require.TestingT, k8s kubernetes.Interface, expectedConfig *CloudBootstrapConfig) {

	// Check that namespace is created
	_, err := k8s.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	require.NoError(t, err)

	// Check the hcp resource id secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPResourceID, secretKeyHCPResourceID,
		expectedConfig.HCPConfig.ResourceID, corev1.SecretTypeOpaque)

	// Check the hcp client id secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPClientID, secretKeyHCPClientID,
		expectedConfig.HCPConfig.ClientID, corev1.SecretTypeOpaque)

	// Check the hcp client secret secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPClientSecret, secretKeyHCPClientSecret,
		expectedConfig.HCPConfig.ClientSecret, corev1.SecretTypeOpaque)

	// Check the hcp client id secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPObservabilityClientID, secretKeyHCPClientID,
		expectedConfig.HCPConfig.ObservabilityClientID, corev1.SecretTypeOpaque)

	// Check the hcp client secret secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPObservabilityClientSecret, secretKeyHCPClientSecret,
		expectedConfig.HCPConfig.ObservabilityClientSecret, corev1.SecretTypeOpaque)

	// Check the hcp auth URL secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPAuthURL, secretKeyHCPAuthURL,
		expectedConfig.HCPConfig.AuthURL, corev1.SecretTypeOpaque)

	// Check the hcp api hostname secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPAPIHostname, secretKeyHCPAPIHostname,
		expectedConfig.HCPConfig.APIHostname, corev1.SecretTypeOpaque)

	// Check the hcp scada address secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameHCPScadaAddress, secretKeyHCPScadaAddress,
		expectedConfig.HCPConfig.ScadaAddress, corev1.SecretTypeOpaque)

	// Check the bootstrap token secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameBootstrapToken, secretKeyBootstrapToken,
		expectedConfig.ConsulConfig.ACL.Tokens.InitialManagement, corev1.SecretTypeOpaque)

	// Check the gossip key secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameGossipKey, secretKeyGossipKey,
		expectedConfig.BootstrapResponse.Bootstrap.GossipKey, corev1.SecretTypeOpaque)

	// Check the server cert secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameServerCert, corev1.TLSCertKey,
		expectedConfig.BootstrapResponse.Bootstrap.ServerTLS.Cert, corev1.SecretTypeTLS)
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameServerCert, corev1.TLSPrivateKeyKey,
		expectedConfig.BootstrapResponse.Bootstrap.ServerTLS.PrivateKey, corev1.SecretTypeTLS)

	// Check the server CA secret is as expected.
	ensureSecretKeyValueMatchesExpected(t, k8s, secretNameServerCA, corev1.TLSCertKey,
		expectedConfig.BootstrapResponse.Bootstrap.ServerTLS.CertificateAuthorities[0], corev1.SecretTypeOpaque)
}

func ensureSecretKeyValueMatchesExpected(t require.TestingT, k8s kubernetes.Interface,
	secretName, secretKey,
	expectedValue string, expectedSecretType corev1.SecretType) {
	secret, err := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Equal(t, expectedValue, string(secret.Data[secretKey]))
	require.Equal(t, expectedSecretType, secret.Type)
	require.Equal(t, common.CLILabelValue, secret.Labels[common.CLILabelKey])
}

func checkSecretsWereNotSaved(k8s kubernetes.Interface) bool {
	ns, _ := k8s.CoreV1().Namespaces().Get(context.Background(), namespace, metav1.GetOptions{})
	hcpClientIdSecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameHCPClientID, metav1.GetOptions{})
	hcpClientSecretSecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameHCPClientSecret, metav1.GetOptions{})
	hcpObservabilityClientIdSecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameHCPObservabilityClientID, metav1.GetOptions{})
	hcpObservabilityClientSecretSecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameHCPObservabilityClientSecret, metav1.GetOptions{})
	hcpResourceIdSecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameHCPResourceID, metav1.GetOptions{})
	bootstrapSecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameBootstrapToken, metav1.GetOptions{})
	gossipKeySecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameGossipKey, metav1.GetOptions{})
	serverCertSecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameServerCert, metav1.GetOptions{})
	serverCASecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secretNameServerCA, metav1.GetOptions{})
	return ns == nil && hcpClientIdSecret == nil && hcpClientSecretSecret == nil &&
		hcpResourceIdSecret == nil && bootstrapSecret == nil &&
		gossipKeySecret == nil && serverCASecret == nil && serverCertSecret == nil && hcpObservabilityClientIdSecret == nil && hcpObservabilityClientSecretSecret == nil
}

func getDeepCopyOfValidBootstrapConfig() *CloudBootstrapConfig {
	data, err := json.Marshal(validBootstrapConfig)
	if err != nil {
		panic(err)
	}

	var copy *CloudBootstrapConfig
	if err := json.Unmarshal(data, &copy); err != nil {
		panic(err)
	}
	return copy
}
