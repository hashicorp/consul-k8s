package connectinit

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  []string{},
			expErr: "-pod-name must be set",
		},
		{
			flags:  []string{"-pod-name", testPodName},
			expErr: "-pod-namespace must be set",
		},
		{
			flags:  []string{"-pod-name", testPodName, "-pod-namespace", testPodNamespace, "-acl-auth-method", test.AuthMethod},
			expErr: "-service-account-name must be set when ACLs are enabled",
		},
		{
			flags:  []string{"-pod-name", testPodName, "-pod-namespace", testPodNamespace, "-acl-auth-method", test.AuthMethod, "-service-account-name", "foo", "-log-level", "invalid"},
			expErr: "unknown log level: invalid",
		},
	}
	for _, c := range cases {
		t.Run(c.expErr, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			code := cmd.Run(c.flags)
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

// TestRun_ServicePollingWithACLsAndTLS bootstraps and starts a consul server using a mock
// kubernetes server to provide responses for setting up the consul AuthMethod
// then validates that the command runs end to end successfully. Also tests with TLS on/off.
func TestRun_ServicePollingWithACLsAndTLS(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                       string
		tls                        bool
		serviceAccountName         string
		serviceName                string
		includeServiceAccountName  bool
		serviceAccountNameMismatch bool
		expFail                    bool
	}{
		{
			name:               "ACLs enabled, no tls",
			tls:                false,
			serviceAccountName: "counting",
		},
		{
			name:               "ACLs enabled, tls",
			tls:                true,
			serviceAccountName: "counting",
		},
		{
			name:               "ACLs enabled, K8s service name matches service account name",
			tls:                false,
			serviceAccountName: "counting",
			serviceName:        "",
		},
		{
			name:               "ACLs enabled, service name annotation matches service account name",
			tls:                false,
			serviceAccountName: "web",
			serviceName:        "web",
		},
		{
			name:               "ACLs enabled, service name annotation doesn't match service account name",
			tls:                false,
			serviceAccountName: "not-a-match",
			serviceName:        "web",
			expFail:            true,
		},
		{
			name:               "ACLs enabled, K8s service name doesn't match service account name",
			tls:                false,
			serviceAccountName: "not-a-match",
			serviceName:        "",
			expFail:            true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			bearerFile := common.WriteTempFile(t, test.ServiceAccountJWTToken)
			tokenFile := fmt.Sprintf("/tmp/%d1", rand.Int())
			proxyFile := fmt.Sprintf("/tmp/%d2", rand.Int())
			t.Cleanup(func() {
				os.Remove(proxyFile)
				os.Remove(tokenFile)
			})

			var caFile, certFile, keyFile string
			// Start Consul server with ACLs enabled and default deny policy.
			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
			server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.ACL.Enabled = true
				c.ACL.DefaultPolicy = "deny"
				c.ACL.Tokens.Master = masterToken
				if tt.tls {
					caFile, certFile, keyFile = test.GenerateServerCerts(t)
					c.CAFile = caFile
					c.CertFile = certFile
					c.KeyFile = keyFile
				}
			})
			require.NoError(t, err)
			defer server.Stop()
			server.WaitForLeader(t)
			cfg := &api.Config{
				Scheme:  "http",
				Address: server.HTTPAddr,
				Token:   masterToken,
			}
			if tt.tls {
				cfg.Address = server.HTTPSAddr
				cfg.Scheme = "https"
				cfg.TLSConfig = api.TLSConfig{
					CAFile: caFile,
				}
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			test.SetupK8sAuthMethod(t, consulClient, testServiceAccountName, "default")

			// Register Consul services.
			testConsulServices := []api.AgentServiceRegistration{consulCountingSvc, consulCountingSvcSidecar}
			for _, svc := range testConsulServices {
				require.NoError(t, consulClient.Agent().ServiceRegister(&svc))
			}

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				bearerTokenFile:                    bearerFile,
				tokenSinkFile:                      tokenFile,
				proxyIDFile:                        proxyFile,
				serviceRegistrationPollingAttempts: 3,
			}

			// We build the http-addr because normally it's defined by the init container setting
			// CONSUL_HTTP_ADDR when it processes the command template.
			flags := []string{"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-acl-auth-method", test.AuthMethod,
				"-service-account-name", tt.serviceAccountName,
				"-service-name", tt.serviceName,
				"-http-addr", fmt.Sprintf("%s://%s", cfg.Scheme, cfg.Address),
			}
			// Add the CA File if necessary since we're not setting CONSUL_CACERT in tt ENV.
			if tt.tls {
				flags = append(flags, "-ca-file", caFile)
			}
			// Run the command.
			code := cmd.Run(flags)
			if tt.expFail {
				require.Equal(t, 1, code)
				return
			}
			require.Equal(t, 0, code, ui.ErrorWriter.String())

			// Validate the ACL token was written.
			tokenData, err := ioutil.ReadFile(tokenFile)
			require.NoError(t, err)
			require.NotEmpty(t, tokenData)

			// Check that the token has the metadata with pod name and pod namespace.
			consulClient, err = api.NewClient(&api.Config{Address: server.HTTPAddr, Token: string(tokenData)})
			require.NoError(t, err)
			token, _, err := consulClient.ACL().TokenReadSelf(nil)
			require.NoError(t, err)
			require.Equal(t, "token created via login: {\"pod\":\"default-ns/counting-pod\"}", token.Description)

			// Validate contents of proxyFile.
			data, err := ioutil.ReadFile(proxyFile)
			require.NoError(t, err)
			require.Contains(t, string(data), "counting-counting-sidecar-proxy")
		})
	}
}

// This test validates service polling works in a happy case scenario with and without TLS.
func TestRun_ServicePollingOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		tls  bool
	}{
		{
			name: "ACLs disabled, no tls",
			tls:  false,
		},
		{
			name: "ACLs disabled, tls",
			tls:  true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			proxyFile := fmt.Sprintf("/tmp/%d", rand.Int())
			t.Cleanup(func() {
				os.Remove(proxyFile)
			})

			var caFile, certFile, keyFile string
			// Start Consul server with TLS enabled if required.
			server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				if tt.tls {
					caFile, certFile, keyFile = test.GenerateServerCerts(t)
					c.CAFile = caFile
					c.CertFile = certFile
					c.KeyFile = keyFile
				}
			})
			require.NoError(t, err)
			defer server.Stop()
			server.WaitForLeader(t)

			// Get the Consul Client.
			cfg := &api.Config{
				Scheme:  "http",
				Address: server.HTTPAddr,
			}
			if tt.tls {
				cfg.Address = server.HTTPSAddr
				cfg.Scheme = "https"
				cfg.TLSConfig = api.TLSConfig{
					CAFile: caFile,
				}
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			// Register Consul services.
			testConsulServices := []api.AgentServiceRegistration{consulCountingSvc, consulCountingSvcSidecar}
			for _, svc := range testConsulServices {
				require.NoError(t, consulClient.Agent().ServiceRegister(&svc))
			}

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				proxyIDFile:                        proxyFile,
				serviceRegistrationPollingAttempts: 3,
			}
			// We build the http-addr because normally it's defined by the init container setting
			// CONSUL_HTTP_ADDR when it processes the command template.
			flags := []string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-http-addr", fmt.Sprintf("%s://%s", cfg.Scheme, cfg.Address)}
			// Add the CA File if necessary since we're not setting CONSUL_CACERT in tt ENV.
			if tt.tls {
				flags = append(flags, "-ca-file", caFile)
			}

			// Run the command.
			code := cmd.Run(flags)
			require.Equal(t, 0, code, ui.ErrorWriter.String())

			// Validate contents of proxyFile.
			data, err := ioutil.ReadFile(proxyFile)
			require.NoError(t, err)
			require.Contains(t, string(data), "counting-counting-sidecar-proxy")
		})
	}

}

// TestRun_ServicePollingErrors tests that when registered services could not be found,
// we error out.
func TestRun_ServicePollingErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		services []api.AgentServiceRegistration
	}{
		{
			name: "only service is registered; proxy service is missing",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
						metaKeyKubeNS:  "default-ns",
					},
				},
			},
		},
		{
			name: "only proxy is registered; service is missing",
			services: []api.AgentServiceRegistration{
				{
					ID:   "counting-counting-sidecar-proxy",
					Name: "counting-sidecar-proxy",
					Kind: "connect-proxy",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "counting",
						DestinationServiceID:   "counting-counting",
					},
					Port:    9999,
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
						metaKeyKubeNS:  "default-ns",
					},
				},
			},
		},
		{
			name: "service and proxy without pod-name and k8s-namespace meta",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
				},
				{
					ID:   "counting-counting-sidecar-proxy",
					Name: "counting-sidecar-proxy",
					Kind: "connect-proxy",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "counting",
						DestinationServiceID:   "counting-counting",
					},
					Port:    9999,
					Address: "127.0.0.1",
				},
			},
		},
		{
			name: "service and proxy with pod-name meta but without k8s-namespace meta",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
					},
				},
				{
					ID:   "counting-counting-sidecar-proxy",
					Name: "counting-sidecar-proxy",
					Kind: "connect-proxy",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "counting",
						DestinationServiceID:   "counting-counting",
					},
					Port:    9999,
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
					},
				},
			},
		},
		{
			name: "service and proxy with k8s-namespace meta but pod-name meta",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyKubeNS: "default-ns",
					},
				},
				{
					ID:   "counting-counting-sidecar-proxy",
					Name: "counting-sidecar-proxy",
					Kind: "connect-proxy",
					Proxy: &api.AgentServiceConnectProxyConfig{
						DestinationServiceName: "counting",
						DestinationServiceID:   "counting-counting",
					},
					Port:    9999,
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyKubeNS: "default-ns",
					},
				},
			},
		},
		{
			name: "both services are non-proxy services",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
						metaKeyKubeNS:  "default-ns",
					},
				},
				{
					ID:      "counting-counting-1",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting-pod",
						metaKeyKubeNS:  "default-ns",
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			proxyFile := fmt.Sprintf("/tmp/%d", rand.Int())
			t.Cleanup(func() {
				os.Remove(proxyFile)
			})

			// Start Consul server.
			server, err := testutil.NewTestServerConfigT(t, nil)
			require.NoError(t, err)
			defer server.Stop()
			server.WaitForLeader(t)
			consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
			require.NoError(t, err)

			// Register Consul services.
			for _, svc := range c.services {
				require.NoError(t, consulClient.Agent().ServiceRegister(&svc))
			}

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				proxyIDFile:                        proxyFile,
				serviceRegistrationPollingAttempts: 1,
			}
			flags := []string{
				"-http-addr", server.HTTPAddr,
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
			}

			code := cmd.Run(flags)
			require.Equal(t, 1, code)
		})
	}
}

// TestRun_RetryServicePolling runs the command but does not register the consul service
// for 2 seconds and then asserts that the proxyid file gets written correctly.
func TestRun_RetryServicePolling(t *testing.T) {
	t.Parallel()
	proxyFile := common.WriteTempFile(t, "")

	// Start Consul server.
	server, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(t, err)
	defer server.Stop()
	server.WaitForLeader(t)
	consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
	require.NoError(t, err)

	// Start the consul service registration in a go func and delay it so that it runs
	// after the cmd.Run() starts.
	go func() {
		// Wait a moment, this ensures that we are already in the retry logic.
		time.Sleep(time.Second * 2)
		// Register counting service.
		require.NoError(t, consulClient.Agent().ServiceRegister(&consulCountingSvc))
		time.Sleep(time.Second * 2)
		// Register proxy sidecar service.
		require.NoError(t, consulClient.Agent().ServiceRegister(&consulCountingSvcSidecar))
	}()

	ui := cli.NewMockUi()
	cmd := Command{
		UI:                                 ui,
		proxyIDFile:                        proxyFile,
		serviceRegistrationPollingAttempts: 10,
	}
	flags := []string{
		"-pod-name", testPodName,
		"-pod-namespace", testPodNamespace,
		"-http-addr", server.HTTPAddr,
	}
	code := cmd.Run(flags)
	require.Equal(t, 0, code)

	// Validate contents of proxyFile.
	data, err := ioutil.ReadFile(proxyFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "counting-counting-sidecar-proxy")
}

// TestRun_InvalidProxyFile validates that we correctly fail in case the proxyid file
// is not writable. This functions as coverage for both ACL and non-ACL codepaths.
func TestRun_InvalidProxyFile(t *testing.T) {
	t.Parallel()
	// This is the output file for the proxyid.
	randFileName := fmt.Sprintf("/foo/%d/%d", rand.Int(), rand.Int())

	// Start Consul server.
	server, err := testutil.NewTestServerConfigT(t, nil)
	require.NoError(t, err)
	defer server.Stop()
	server.WaitForLeader(t)
	consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
	require.NoError(t, err)

	// Register Consul services.
	testConsulServices := []api.AgentServiceRegistration{consulCountingSvc, consulCountingSvcSidecar}
	for _, svc := range testConsulServices {
		require.NoError(t, consulClient.Agent().ServiceRegister(&svc))
	}
	ui := cli.NewMockUi()
	cmd := Command{
		UI:                                 ui,
		proxyIDFile:                        randFileName,
		serviceRegistrationPollingAttempts: 3,
	}
	flags := []string{
		"-pod-name", testPodName,
		"-pod-namespace", testPodNamespace,
		"-http-addr", server.HTTPAddr,
	}
	code := cmd.Run(flags)
	require.Equal(t, 1, code)
	_, err = os.Stat(randFileName)
	require.Error(t, err)
}

// TestRun_FailsWithBadServerResponses tests error handling with invalid server responses.
func TestRun_FailsWithBadServerResponses(t *testing.T) {
	t.Parallel()
	const servicesGetRetries int = 2
	cases := []struct {
		name                    string
		loginResponse           string
		getServicesListResponse string
		expectedServiceGets     int
	}{
		{
			name:                "acls enabled, acl login response invalid",
			loginResponse:       "",
			expectedServiceGets: 0,
		},
		{
			name:                    "acls enabled, get service response invalid",
			loginResponse:           testLoginResponse,
			getServicesListResponse: "",
			expectedServiceGets:     servicesGetRetries + 1, // Plus 1 because we RETRY after an initial attempt.
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bearerFile := common.WriteTempFile(t, "bearerTokenFile")
			tokenFile := common.WriteTempFile(t, "")

			servicesGetCounter := 0
			// Start the mock Consul server.
			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// ACL login request.
				if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
					w.Write([]byte(c.loginResponse))
				}
				// Agent Services get.
				if r != nil && r.URL.Path == "/v1/agent/services" && r.Method == "GET" {
					servicesGetCounter++
					w.Write([]byte(c.getServicesListResponse))
				}
			}))
			defer consulServer.Close()

			// Setup the Command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				bearerTokenFile:                    bearerFile,
				tokenSinkFile:                      tokenFile,
				serviceRegistrationPollingAttempts: uint64(servicesGetRetries),
			}

			serverURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)
			flags := []string{
				"-pod-name", testPodName, "-pod-namespace", testPodNamespace,
				"-acl-auth-method", test.AuthMethod,
				"-service-account-name", testServiceAccountName,
				"-http-addr", serverURL.String()}
			code := cmd.Run(flags)
			require.Equal(t, 1, code)
			// We use the counter to ensure we failed at ACL Login (when counter = 0) or proceeded to the service get portion of the command.
			require.Equal(t, c.expectedServiceGets, servicesGetCounter)
		})
	}
}

// Tests ACL Login with Retries.
func TestRun_LoginWithRetries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		Description        string
		TestRetry          bool
		LoginAttemptsCount int
		ExpCode            int
	}{
		{
			Description:        "Login succeeds without retries",
			TestRetry:          false,
			LoginAttemptsCount: 1, // 1 because we dont actually retry.
			ExpCode:            0,
		},
		{
			Description:        "Login succeeds after 1 retry",
			TestRetry:          true,
			LoginAttemptsCount: 2,
			ExpCode:            0,
		},
	}
	for _, c := range cases {
		t.Run(c.Description, func(t *testing.T) {
			// Create a fake input bearer token file and an output file.
			bearerFile := common.WriteTempFile(t, "bearerTokenFile")
			tokenFile := common.WriteTempFile(t, "")
			proxyFile := common.WriteTempFile(t, "")

			// Start the mock Consul server.
			counter := 0
			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// ACL Login.
				if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
					counter++
					if !c.TestRetry || (c.TestRetry && c.LoginAttemptsCount == counter) {
						w.Write([]byte(testLoginResponse))
					}
				}
				// Agent Services get.
				if r != nil && r.URL.Path == "/v1/agent/services" && r.Method == "GET" {
					w.Write([]byte(testServiceListResponse))
				}
			}))
			defer consulServer.Close()

			serverURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)

			ui := cli.NewMockUi()
			cmd := Command{
				UI:              ui,
				tokenSinkFile:   tokenFile,
				bearerTokenFile: bearerFile,
				proxyIDFile:     proxyFile,
			}
			code := cmd.Run([]string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-acl-auth-method", test.AuthMethod,
				"-service-account-name", testServiceAccountName,
				"-http-addr", serverURL.String()})
			fmt.Println(ui.ErrorWriter.String())
			require.Equal(t, c.ExpCode, code)
			// Cmd will return 1 after numACLLoginRetries, so bound LoginAttemptsCount if we exceeded it.
			require.Equal(t, c.LoginAttemptsCount, counter)
			// Validate that the token was written to disk if we succeeded.
			tokenData, err := ioutil.ReadFile(tokenFile)
			require.NoError(t, err)
			require.Equal(t, "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586", string(tokenData))
			// Validate contents of proxyFile.
			proxydata, err := ioutil.ReadFile(proxyFile)
			require.NoError(t, err)
			require.Equal(t, "counting-counting-sidecar-proxy", string(proxydata))
		})
	}
}

const (
	metaKeyPodName         = "pod-name"
	metaKeyKubeNS          = "k8s-namespace"
	metaKeyKubeServiceName = "k8s-service-name"
	testPodNamespace       = "default-ns"
	testPodName            = "counting-pod"
	testServiceAccountName = "counting"

	// sample response from https://consul.io/api-docs/acl#sample-response
	testLoginResponse = `{
  "AccessorID": "926e2bd2-b344-d91b-0c83-ae89f372cd9b",
  "SecretID": "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586",
  "Description": "token created via login",
  "Roles": [
    {
      "ID": "3356c67c-5535-403a-ad79-c1d5f9df8fc7",
      "Name": "demo"
    }
  ],
  "ServiceIdentities": [
    {
      "ServiceName": "example"
    }
  ],
  "Local": true,
  "AuthMethod": "minikube",
  "CreateTime": "2019-04-29T10:08:08.404370762-05:00",
  "Hash": "nLimyD+7l6miiHEBmN/tvCelAmE/SbIXxcnTzG3pbGY=",
  "CreateIndex": 36,
  "ModifyIndex": 36
}`

	testServiceListResponse = `{
  "counting-counting": {
    "ID": "counting-counting",
    "Service": "counting",
    "Tags": [],
    "Meta": {
      "k8s-namespace": "default",
      "pod-name": "counting-pod",
      "k8s-service-name": "counting"
    },
    "Port": 9001,
    "Address": "10.32.3.26",
    "TaggedAddresses": {
      "lan_ipv4": {
        "Address": "10.32.3.26",
        "Port": 9001
      },
      "wan_ipv4": {
        "Address": "10.32.3.26",
        "Port": 9001
      }
    },
    "Weights": {
      "Passing": 1,
      "Warning": 1
    },
    "EnableTagOverride": false,
    "Datacenter": "dc1"
  },
  "counting-counting-sidecar-proxy": {
    "Kind": "connect-proxy",
    "ID": "counting-counting-sidecar-proxy",
    "Service": "counting-sidecar-proxy",
    "Tags": [],
    "Meta": {
      "k8s-namespace": "default",
      "pod-name": "counting-pod",
      "k8s-service-name": "counting"
    },
    "Port": 20000,
    "Address": "10.32.3.26",
    "TaggedAddresses": {
      "lan_ipv4": {
        "Address": "10.32.3.26",
        "Port": 20000
      },
      "wan_ipv4": {
        "Address": "10.32.3.26",
        "Port": 20000
      }
    },
    "Weights": {
      "Passing": 1,
      "Warning": 1
    },
    "EnableTagOverride": false,
    "Proxy": {
      "DestinationServiceName": "counting",
      "DestinationServiceID": "counting-counting",
      "LocalServiceAddress": "127.0.0.1",
      "LocalServicePort": 9001,
      "MeshGateway": {},
      "Expose": {}
    },
    "Datacenter": "dc1"
  }
}`
)

var (
	consulCountingSvc = api.AgentServiceRegistration{
		ID:      "counting-counting",
		Name:    "counting",
		Address: "127.0.0.1",
		Meta: map[string]string{
			metaKeyPodName:         "counting-pod",
			metaKeyKubeNS:          "default-ns",
			metaKeyKubeServiceName: "counting",
		},
	}
	consulCountingSvcSidecar = api.AgentServiceRegistration{
		ID:   "counting-counting-sidecar-proxy",
		Name: "counting-sidecar-proxy",
		Kind: "connect-proxy",
		Proxy: &api.AgentServiceConnectProxyConfig{
			DestinationServiceName: "counting",
			DestinationServiceID:   "counting-counting",
			Config:                 nil,
			Upstreams:              nil,
		},
		Port:    9999,
		Address: "127.0.0.1",
		Meta: map[string]string{
			metaKeyPodName:         "counting-pod",
			metaKeyKubeNS:          "default-ns",
			metaKeyKubeServiceName: "counting",
		},
	}
)
