package connectinit

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"

	connectinject "github.com/hashicorp/consul-k8s/control-plane/connect-inject"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
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
			flags: []string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-acl-auth-method", test.AuthMethod},
			expErr: "-service-account-name must be set when ACLs are enabled",
		},
		{
			flags: []string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace},
			expErr: "-consul-node-name must be set",
		},
		{
			flags: []string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-acl-auth-method", test.AuthMethod,
				"-service-account-name", "foo",
				"-consul-node-name", "bar",
			},
			expErr: "-consul-api-timeout must be set to a value greater than 0",
		},
		{
			flags: []string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-acl-auth-method", test.AuthMethod,
				"-service-account-name", "foo",
				"-consul-api-timeout", "5s",
				"-log-level", "invalid",
				"-consul-node-name", "bar",
			},
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

// TestRun tests that the command can log in to Consul (if ACLs are enabled) using a kubernetes
// auth method and using the obtained token find the services for the provided pod name
// and namespace provided and write the proxy ID of the proxy service to a file.
func TestRun(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                       string
		aclsEnabled                bool
		serviceAccountName         string
		serviceName                string
		includeServiceAccountName  bool
		serviceAccountNameMismatch bool
		expFail                    bool
		multiport                  bool
	}{
		{
			name:               "acls disabled; service-name not provided",
			serviceAccountName: "counting",
		},
		{
			name:               "acls enabled; K8s service name matches service account name",
			aclsEnabled:        true,
			serviceAccountName: "counting",
		},
		{
			name:               "acls enabled; service name annotation matches service account name",
			aclsEnabled:        true,
			serviceAccountName: "web",
			serviceName:        "web",
		},
		{
			name:               "acls enabled; multi-port service",
			aclsEnabled:        true,
			serviceAccountName: "counting-admin",
			serviceName:        "counting-admin",
			multiport:          true,
		},
		{
			name:               "acls disabled; multi-port service",
			serviceAccountName: "counting-admin",
			serviceName:        "counting-admin",
			multiport:          true,
		},
		{
			name:               "acls enabled; service name annotation doesn't match service account name",
			aclsEnabled:        true,
			serviceAccountName: "not-a-match",
			serviceName:        "web",
			expFail:            true,
		},
		{
			name:               "acls enabled; K8s service name doesn't match service account name",
			aclsEnabled:        true,
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
				_ = os.Remove(proxyFile)
				_ = os.Remove(tokenFile)
			})

			// Start Consul server with ACLs enabled and default deny policy.
			initialMgmtToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
			server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				if tt.aclsEnabled {
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.InitialManagement = initialMgmtToken
				}
			})
			require.NoError(t, err)
			defer server.Stop()
			server.WaitForLeader(t)
			cfg := &api.Config{
				Scheme:  "http",
				Address: server.HTTPAddr,
			}
			if tt.aclsEnabled {
				cfg.Token = initialMgmtToken
			}
			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			if tt.aclsEnabled {
				test.SetupK8sAuthMethod(t, consulClient, testServiceAccountName, "default")
			}

			// Register Consul services.
			testConsulServices := []api.AgentService{consulCountingSvc, consulCountingSvcSidecar}
			if tt.multiport {
				testConsulServices = append(testConsulServices, consulCountingSvcMultiport, consulCountingSvcSidecarMultiport)
			}
			for _, svc := range testConsulServices {
				serviceRegistration := &api.CatalogRegistration{
					Node:    connectinject.ConsulNodeName,
					Address: "127.0.0.1",
					Service: &svc,
				}
				_, err = consulClient.Catalog().Register(serviceRegistration, nil)
				require.NoError(t, err)
			}

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				serviceRegistrationPollingAttempts: 3,
			}

			// We build the http-addr because normally it's defined by the init container setting
			// CONSUL_HTTP_ADDR when it processes the command template.
			flags := []string{"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-service-name", tt.serviceName,
				"-http-addr", fmt.Sprintf("%s://%s", cfg.Scheme, cfg.Address),
				"-proxy-id-file", proxyFile,
				"-multiport=" + strconv.FormatBool(tt.multiport),
				"-consul-node-name", connectinject.ConsulNodeName,
				"-consul-api-timeout=5s",
			}
			if tt.aclsEnabled {
				flags = append(flags, "-acl-auth-method", test.AuthMethod,
					"-service-account-name", tt.serviceAccountName,
					"-bearer-token-file", bearerFile,
					"-acl-token-sink", tokenFile)
			}

			// Run the command.
			code := cmd.Run(flags)
			if tt.expFail {
				require.Equal(t, 1, code)
				return
			}
			require.Equal(t, 0, code, ui.ErrorWriter.String())

			if tt.aclsEnabled {
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
			}

			// Validate contents of proxyFile.
			data, err := ioutil.ReadFile(proxyFile)
			require.NoError(t, err)
			if tt.multiport {
				require.Contains(t, string(data), "counting-admin-sidecar-proxy-id")
			} else {
				require.Contains(t, string(data), "counting-counting-sidecar-proxy")
			}
		})
	}
}

// TestRun_Errors tests that when registered services could not be found,
// we error out.
func TestRun_Errors(t *testing.T) {
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
				serviceRegistrationPollingAttempts: 1,
			}
			flags := []string{
				"-http-addr", server.HTTPAddr,
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-proxy-id-file", proxyFile,
				"-consul-api-timeout", "5s",
				"-consul-node-name", connectinject.ConsulNodeName,
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
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Wait a moment, this ensures that we are already in the retry logic.
		time.Sleep(time.Second * 2)
		// Register counting service.
		serviceRegistration := &api.CatalogRegistration{
			Node:    connectinject.ConsulNodeName,
			Address: "127.0.0.1",
			Service: &consulCountingSvc,
		}
		_, err = consulClient.Catalog().Register(serviceRegistration, nil)
		require.NoError(t, err)
		// Register proxy sidecar service.
		serviceRegistration.Service = &consulCountingSvcSidecar
		_, err = consulClient.Catalog().Register(serviceRegistration, nil)
		require.NoError(t, err)
	}()

	ui := cli.NewMockUi()
	cmd := Command{
		UI:                                 ui,
		serviceRegistrationPollingAttempts: 10,
	}
	flags := []string{
		"-pod-name", testPodName,
		"-pod-namespace", testPodNamespace,
		"-http-addr", server.HTTPAddr,
		"-proxy-id-file", proxyFile,
		"-consul-api-timeout", "5s",
		"-consul-node-name", connectinject.ConsulNodeName,
	}
	code := cmd.Run(flags)
	wg.Wait()
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
	testConsulServices := []api.AgentService{consulCountingSvc, consulCountingSvcSidecar}
	for _, svc := range testConsulServices {
		serviceRegistration := &api.CatalogRegistration{
			Node:    connectinject.ConsulNodeName,
			Address: "127.0.0.1",
			Service: &svc,
		}
		_, err = consulClient.Catalog().Register(serviceRegistration, nil)
		require.NoError(t, err)
	}
	ui := cli.NewMockUi()
	cmd := Command{
		UI:                                 ui,
		serviceRegistrationPollingAttempts: 3,
	}
	flags := []string{
		"-pod-name", testPodName,
		"-pod-namespace", testPodNamespace,
		"-http-addr", server.HTTPAddr,
		"-proxy-id-file", randFileName,
		"-consul-api-timeout", "5s",
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
		name                string
		loginResponse       string
		expectedServiceGets int
	}{
		{
			name:                "acls enabled, acl login response invalid",
			loginResponse:       "",
			expectedServiceGets: 0,
		},
		{
			name:                "acls enabled, get service response invalid",
			loginResponse:       testLoginResponse,
			expectedServiceGets: servicesGetRetries + 1, // Plus 1 because we RETRY after an initial attempt.
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
				// Token read request.
				if r != nil && r.URL.Path == "/v1/acl/token/self" && r.Method == "GET" {
					w.Write([]byte(testTokenReadSelfResponse))
				}
				// Services list request.
				if r != nil && r.URL.Path == "/v1/catalog/node-services/"+connectinject.ConsulNodeName && r.Method == "GET" {
					servicesGetCounter++
					w.Write([]byte(""))
				}
			}))
			defer consulServer.Close()

			// Set up the Command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				flagBearerTokenFile:                bearerFile,
				flagACLTokenSink:                   tokenFile,
				serviceRegistrationPollingAttempts: 2,
				loginAttempts:                      2,
			}

			serverURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)
			flags := []string{
				"-pod-name", testPodName, "-pod-namespace", testPodNamespace,
				"-acl-auth-method", test.AuthMethod,
				"-service-account-name", testServiceAccountName,
				"-bearer-token-file", bearerFile,
				"-acl-token-sink", tokenFile,
				"-http-addr", serverURL.String(),
				"-consul-api-timeout", "5s",
				"-consul-node-name", connectinject.ConsulNodeName,
			}
			code := cmd.Run(flags)
			require.Equal(t, 1, code)
			// We use the counter to ensure we failed at ACL Login (when counter = 0) or proceeded to the service get portion of the command.
			require.Equal(t, c.expectedServiceGets, servicesGetCounter)
		})
	}
}

// Test that we check token exists when reading it in the stale consistency mode.
func TestRun_EnsureTokenExists(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		neverSucceed bool
	}{
		"succeed after first retry": {neverSucceed: false},
		"never succeed":             {neverSucceed: true},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			// Create a fake input bearer token file and an output file.
			bearerFile := common.WriteTempFile(t, "bearerTokenFile")
			tokenFile := common.WriteTempFile(t, "")
			proxyFile := common.WriteTempFile(t, "")

			// Start the mock Consul server.
			counter := 0
			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// ACL Login.
				if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
					w.Write([]byte(testLoginResponse))
				}
				// Token read request.
				if r != nil &&
					r.URL.Path == "/v1/acl/token/self" &&
					r.Method == "GET" &&
					r.URL.Query().Has("stale") {

					// Fail the first request but succeed on the next.
					if counter == 0 || c.neverSucceed {
						counter++
						w.WriteHeader(http.StatusForbidden)
						w.Write([]byte("ACL not found"))
					} else {
						w.Write([]byte(testTokenReadSelfResponse))
					}
				}
				// Node Services list.
				if r != nil && r.URL.Path == "/v1/catalog/node-services/"+connectinject.ConsulNodeName && r.Method == "GET" {
					w.Write([]byte(testServiceListResponse))
				}
			}))
			defer consulServer.Close()

			serverURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)

			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			code := cmd.Run([]string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-acl-auth-method", test.AuthMethod,
				"-service-account-name", testServiceAccountName,
				"-acl-token-sink", tokenFile,
				"-bearer-token-file", bearerFile,
				"-proxy-id-file", proxyFile,
				"-http-addr", serverURL.String(),
				"-consul-api-timeout", "5s",
				"-consul-node-name", connectinject.ConsulNodeName,
			})
			if c.neverSucceed {
				require.Equal(t, 1, code, ui.ErrorWriter)
			} else {
				require.Equal(t, 0, code, ui.ErrorWriter)
				require.Equal(t, 1, counter)
			}
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

	// Sample response from https://consul.io/api-docs/acl#sample-response.
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

	// Sample response from https://www.consul.io/api-docs/acl/tokens#read-self-token.
	testTokenReadSelfResponse = `
{
  "AccessorID": "6a1253d2-1785-24fd-91c2-f8e78c745511",
  "SecretID": "45a3bd52-07c7-47a4-52fd-0745e0cfe967",
  "Description": "Agent token for 'node1'",
  "Policies": [
    {
      "ID": "165d4317-e379-f732-ce70-86278c4558f7",
      "Name": "node1-write"
    },
    {
      "ID": "e359bd81-baca-903e-7e64-1ccd9fdc78f5",
      "Name": "node-read"
    }
  ],
  "Local": false,
  "CreateTime": "2018-10-24T12:25:06.921933-04:00",
  "Hash": "UuiRkOQPRCvoRZHRtUxxbrmwZ5crYrOdZ0Z1FTFbTbA=",
  "CreateIndex": 59,
  "ModifyIndex": 59
}
`

	testServiceListResponse = `{
 "Node": {
    "ID": "40e4a748-2192-161a-0510-9bf59fe950b5",
    "Node": "k8s-service-mesh",
    "Address": "127.0.0.1",
    "Datacenter": "dc1"
  },
  "Services": [
	  {
		"ID": "counting-counting",
		"Service": "counting",
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
	  {
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
  ]
}`
)

var (
	consulCountingSvc = api.AgentService{
		ID:      "counting-counting",
		Service: "counting",
		Address: "127.0.0.1",
		Meta: map[string]string{
			metaKeyPodName:         "counting-pod",
			metaKeyKubeNS:          "default-ns",
			metaKeyKubeServiceName: "counting",
		},
	}
	consulCountingSvcSidecar = api.AgentService{
		ID:      "counting-counting-sidecar-proxy",
		Service: "counting-sidecar-proxy",
		Kind:    "connect-proxy",
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
	consulCountingSvcMultiport = api.AgentService{
		ID:      "counting-admin-id",
		Service: "counting-admin",
		Address: "127.0.0.1",
		Meta: map[string]string{
			metaKeyPodName:         "counting-pod",
			metaKeyKubeNS:          "default-ns",
			metaKeyKubeServiceName: "counting-admin",
		},
	}
	consulCountingSvcSidecarMultiport = api.AgentService{
		ID:      "counting-admin-sidecar-proxy-id",
		Service: "counting-admin-sidecar-proxy",
		Kind:    "connect-proxy",
		Proxy: &api.AgentServiceConnectProxyConfig{
			DestinationServiceName: "counting-admin",
			DestinationServiceID:   "counting-admin-id",
			Config:                 nil,
			Upstreams:              nil,
		},
		Port:    9999,
		Address: "127.0.0.1",
		Meta: map[string]string{
			metaKeyPodName:         "counting-pod",
			metaKeyKubeNS:          "default-ns",
			metaKeyKubeServiceName: "counting-admin",
		},
	}
)
