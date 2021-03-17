package connectinit

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/consul"
	"github.com/hashicorp/consul-k8s/subcommand/common"
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

// This test mocks ACL login and the service list response (/v1/agent/services),
// the later of which is also validated by an actual agent in TestRun_happyPathNoACLs().
func TestRun_happyPathACLs(t *testing.T) {
	t.Parallel()
	bearerFile := common.WriteTempFile(t, "bearerTokenFile")
	proxyFile := common.WriteTempFile(t, "")
	tokenFile := common.WriteTempFile(t, "")

	// Start the mock Consul server.
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// ACL login request.
		if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
			w.Write([]byte(testLoginResponse))
		}
		// Get list of Agent Services.
		if r != nil && r.URL.Path == "/v1/agent/services" && r.Method == "GET" {
			w.Write([]byte(testServiceListResponse))
		}
	}))
	defer consulServer.Close()
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(t, err)
	clientConfig := &api.Config{Address: serverURL.String()}
	client, err := consul.NewClient(clientConfig)
	require.NoError(t, err)
	ui := cli.NewMockUi()
	cmd := Command{
		UI:              ui,
		consulClient:    client,
		bearerTokenFile: bearerFile,
		tokenSinkFile:   tokenFile,
		proxyIDFile:     proxyFile,
	}
	flags := []string{"-pod-name", testPodName,
		"-pod-namespace", testPodNamespace,
		"-acl-auth-method", testAuthMethod,
		"-skip-service-registration-polling=false"}
	// Run the command.
	code := cmd.Run(flags)
	require.Equal(t, 0, code)

	// Validate contents of proxyFile.
	data, err := ioutil.ReadFile(proxyFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "counting-counting-sidecar-proxy")
}

// This test validates happy path without ACLs : wait on proxy+service to be registered and write out proxyid file
func TestRun_happyPathNoACLs(t *testing.T) {
	t.Parallel()
	// This is the output file for the proxyid.
	proxyFile := common.WriteTempFile(t, "")

	// Start Consul server.
	server, err := testutil.NewTestServerConfigT(t, nil)
	defer server.Stop()
	require.NoError(t, err)
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
		UI:           ui,
		consulClient: consulClient,
		proxyIDFile:  proxyFile,
	}
	code := cmd.Run(defaultTestFlags)
	require.Equal(t, 0, code)

	// Validate contents of proxyFile.
	data, err := ioutil.ReadFile(proxyFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "counting-counting-sidecar-proxy")
}

// TestRun_RetryServicePolling runs the command but does not register the consul service
// for 2 seconds and then asserts that the proxyid file gets written correctly.
func TestRun_RetryServicePolling(t *testing.T) {
	t.Parallel()
	proxyFile := common.WriteTempFile(t, "")

	// Start Consul server.
	server, err := testutil.NewTestServerConfigT(t, nil)
	defer server.Stop()
	require.NoError(t, err)
	server.WaitForLeader(t)
	consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
	require.NoError(t, err)

	// Start the consul service registration in a go func and delay it so that it runs
	// after the cmd.Run() starts.
	go func() {
		// Wait a moment.
		time.Sleep(time.Second * 1)
		// Register counting service.
		require.NoError(t, consulClient.Agent().ServiceRegister(&consulCountingSvc))
		time.Sleep(time.Second * 2)
		// Register proxy sidecar service.
		require.NoError(t, consulClient.Agent().ServiceRegister(&consulCountingSvcSidecar))
	}()

	ui := cli.NewMockUi()
	cmd := Command{
		UI:                                 ui,
		consulClient:                       consulClient,
		proxyIDFile:                        proxyFile,
		serviceRegistrationPollingAttempts: 10,
	}
	code := cmd.Run(defaultTestFlags)
	require.Equal(t, 0, code)
	// Validate that we hit the retry logic when the service was registered but the proxy service is not registered yet.
	require.Contains(t, ui.OutputWriter.String(), "Unable to find registered services; retrying")

	// Validate contents of proxyFile.
	data, err := ioutil.ReadFile(proxyFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "counting-counting-sidecar-proxy")
}

// TestRun_invalidProxyFile validates that we correctly fail in case the proxyid file
// is not writable. This functions as coverage for both ACL and non-ACL codepaths.
func TestRun_invalidProxyFile(t *testing.T) {
	t.Parallel()
	// This is the output file for the proxyid.
	randFileName := fmt.Sprintf("/foo/%d/%d", rand.Int(), rand.Int())

	// Start Consul server.
	server, err := testutil.NewTestServerConfigT(t, nil)
	defer server.Stop()
	require.NoError(t, err)
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
		consulClient:                       consulClient,
		proxyIDFile:                        randFileName,
		serviceRegistrationPollingAttempts: 3,
	}
	expErr := fmt.Sprintf("unable to write proxy ID to file: open %s: no such file or directory\n", randFileName)
	code := cmd.Run(defaultTestFlags)
	require.Equal(t, 1, code)
	require.Equal(t, expErr, ui.ErrorWriter.String())
}

// TestRun_FailsWithBadServerResponses tests error handling with invalid server responses.
func TestRun_FailsWithBadServerResponses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                    string
		loginResponse           string
		getServicesListResponse string
		expErr                  string
	}{
		{
			name:          "acls enabled, acl login response invalid",
			loginResponse: "",
			expErr:        "Hit maximum retries for consul login",
		},
		{
			name:                    "acls enabled, get service response invalid",
			loginResponse:           testLoginResponse,
			getServicesListResponse: "",
			expErr:                  "Timed out waiting for service registration",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bearerFile := common.WriteTempFile(t, "bearerTokenFile")
			tokenFile := common.WriteTempFile(t, "")

			// Start the mock Consul server.
			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// ACL login request.
				if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
					w.Write([]byte(c.loginResponse))
				}
				// Agent Services get.
				if r != nil && r.URL.Path == "/v1/agent/services" && r.Method == "GET" {
					w.Write([]byte(c.getServicesListResponse))
				}
			}))
			defer consulServer.Close()
			// Setup the Client.
			serverURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)
			clientConfig := &api.Config{Address: serverURL.String()}
			client, err := consul.NewClient(clientConfig)
			require.NoError(t, err)

			// Setup the Command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				consulClient:                       client,
				bearerTokenFile:                    bearerFile,
				tokenSinkFile:                      tokenFile,
				serviceRegistrationPollingAttempts: 2,
			}

			flags := []string{
				"-pod-name", testPodName, "-pod-namespace", testPodNamespace,
				"-acl-auth-method", testAuthMethod,
				"-skip-service-registration-polling=false"}
			code := cmd.Run(flags)
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

// Tests ACL Login with Retries.
func TestRun_LoginwithRetries(t *testing.T) {
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
			clientConfig := &api.Config{Address: serverURL.String()}
			client, err := consul.NewClient(clientConfig)
			require.NoError(t, err)

			ui := cli.NewMockUi()
			cmd := Command{
				UI:              ui,
				consulClient:    client,
				tokenSinkFile:   tokenFile,
				bearerTokenFile: bearerFile,
				proxyIDFile:     proxyFile,
			}
			code := cmd.Run([]string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-acl-auth-method", testAuthMethod,
				"-skip-service-registration-polling=false"})
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

const testPodNamespace = "default"
const testPodName = "counting"
const testPodMeta = "pod=default/counting"
const testAuthMethod = "consul-k8s-auth-method"

// sample response from https://consul.io/api-docs/acl#sample-response
const testLoginResponse = `{
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

const testServiceListResponse = `{
  "counting-counting": {
    "ID": "counting-counting",
    "Service": "counting",
    "Tags": [],
    "Meta": {
      "k8s-namespace": "default",
      "pod-name": "counting"
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
      "pod-name": "counting"
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
const metaKeyPodName = "pod-name"
const metaKeyKubeNS = "k8s-namespace"

var (
	consulCountingSvc = api.AgentServiceRegistration{
		ID:      "counting-counting",
		Name:    "counting",
		Address: "127.0.0.1",
		Meta: map[string]string{
			metaKeyPodName: "counting",
			metaKeyKubeNS:  "default",
		},
	}
	consulCountingSvcSidecar = api.AgentServiceRegistration{
		ID:   "counting-counting-sidecar-proxy",
		Name: "counting-sidecar-proxy",
		Kind: "connect-proxy",
		Proxy: &api.AgentServiceConnectProxyConfig{
			DestinationServiceName: "foo",
			DestinationServiceID:   "foo",
			Config:                 nil,
			Upstreams:              nil,
		},
		Port:    9999,
		Address: "127.0.0.1",
		Meta: map[string]string{
			metaKeyPodName: "counting",
			metaKeyKubeNS:  "default",
		},
	}
	defaultTestFlags = []string{"-pod-name", testPodName, "-pod-namespace", testPodNamespace, "-skip-service-registration-polling=false"}
)
