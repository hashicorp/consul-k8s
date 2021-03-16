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

// NOTES: Test coverage works as follows:
// 1. All tests which are specific to 'login' are in subcommand/common/common_test.go
// 2. The rest are here:
// ACLS enabled happy path							// done
// ACLs disabled happy path (uses a valid agent)	// done
// ACLs disabled invalid response on service get 	// covered by ACLs enabled test bc its the same API call
// invalid proxyid file								// done
// ACLS enabled fails due to invalid server responses	// done
// ACLS enabled fails due to IO (invalid bearer, tokenfile)	// covered by common_test.go
// test that retries work for service polling

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()
	require := require.New(t)
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
			flags:  []string{"-pod-name", testPodName, "-pod-namespace", testPodNamespace, "-acl-auth-method", testAuthMethod},
			expErr: "-meta must be set",
		},
	}
	for _, c := range cases {
		t.Run(c.expErr, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			code := cmd.Run(c.flags)
			require.Equal(1, code)
			require.Contains(ui.ErrorWriter.String(), c.expErr)
		})
	}
}

// This test mocks ACL login and the service list response (/v1/agent/services),
// the later of which is also validated by an actual agent in TestRun_happyPathNoACLs().
func TestRun_happyPathACLs(t *testing.T) {
	require := require.New(t)

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
	require.NoError(err)
	clientConfig := &api.Config{Address: serverURL.String()}
	client, err := consul.NewClient(clientConfig)
	require.NoError(err)
	ui := cli.NewMockUi()
	cmd := Command{
		UI:              ui,
		consulClient:    client,
		BearerTokenFile: bearerFile,
		TokenSinkFile:   tokenFile,
		ProxyIDFile:     proxyFile,
	}
	flags := []string{"-pod-name", testPodName,
		"-pod-namespace", testPodNamespace,
		"-acl-auth-method", testAuthMethod, "-meta", testPodMeta,
		"-skip-service-registration-polling=false"}
	// Run the command.
	code := cmd.Run(flags)
	require.Equal(0, code)

	// Validate contents of proxyFile.
	data, err := ioutil.ReadFile(proxyFile)
	require.NoError(err)
	require.Contains(string(data), "counting-counting-sidecar-proxy")
}

// This test validates happy path without ACLs : wait on proxy+service to be registered and write out proxyid file
func TestRun_happyPathNoACLs(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	// This is the output file for the proxyid.
	proxyFile := common.WriteTempFile(t, "")

	// Start Consul server.
	server, err := testutil.NewTestServerConfigT(t, nil)
	defer server.Stop()
	require.NoError(err)
	server.WaitForLeader(t)
	consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
	require.NoError(err)

	// Register Consul services.
	testConsulServices := []api.AgentServiceRegistration{consulCountingSvc, consulCountingSvcSidecar}
	for _, svc := range testConsulServices {
		require.NoError(consulClient.Agent().ServiceRegister(&svc))
	}

	ui := cli.NewMockUi()
	cmd := Command{
		UI:           ui,
		consulClient: consulClient,
		ProxyIDFile:  proxyFile,
	}
	code := cmd.Run(defaultTestFlags)
	require.Equal(0, code)

	// Validate contents of proxyFile.
	data, err := ioutil.ReadFile(proxyFile)
	require.NoError(err)
	require.Contains(string(data), "counting-counting-sidecar-proxy")
}

// TestRun_RetryServicePolling starts the command and does not register the consul service
// for 2 seconds and then asserts that the proxyid file gets written correctly.
func TestRun_RetryServicePolling(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	proxyFile := common.WriteTempFile(t, "")

	// Start Consul server.
	server, err := testutil.NewTestServerConfigT(t, nil)
	defer server.Stop()
	require.NoError(err)
	server.WaitForLeader(t)
	consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
	require.NoError(err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:                                 ui,
		consulClient:                       consulClient,
		ProxyIDFile:                        proxyFile,
		ServiceRegistrationPollingAttempts: 10,
	}
	// Start the command asynchronously, later registering the services.
	exitChan := runCommandAsynchronously(&cmd, defaultTestFlags)
	// Wait a moment.
	time.Sleep(time.Second * 1)
	// Register Consul services.
	testConsulServices := []api.AgentServiceRegistration{consulCountingSvc, consulCountingSvcSidecar}
	for _, svc := range testConsulServices {
		require.NoError(consulClient.Agent().ServiceRegister(&svc))
		time.Sleep(time.Second * 2)
	}

	// Assert that it exits cleanly or timeout.
	select {
	case exitCode := <-exitChan:
		require.Equal(0, exitCode)
	case <-time.After(time.Second * 10):
		// Fail if the stopCh was not caught.
		require.Fail("timeout waiting for command to exit")
	}
	// Validate that we hit the retry logic when proxy service is not registered yet.
	require.Contains(ui.OutputWriter.String(), "Unable to find registered services; Retrying")

	// Validate contents of proxyFile.
	data, err := ioutil.ReadFile(proxyFile)
	require.NoError(err)
	require.Contains(string(data), "counting-counting-sidecar-proxy")
}

// TestRun_invalidProxyFile validates that we correctly fail in case the proxyid file
// is not writable. This functions as coverage for both ACL and non-ACL codepaths.
func TestRun_invalidProxyFile(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	// This is the output file for the proxyid.
	randFileName := fmt.Sprintf("/foo/%d/%d", rand.Int(), rand.Int())

	// Start Consul server.
	server, err := testutil.NewTestServerConfigT(t, nil)
	defer server.Stop()
	require.NoError(err)
	server.WaitForLeader(t)
	consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr})
	require.NoError(err)

	// Register Consul services.
	testConsulServices := []api.AgentServiceRegistration{consulCountingSvc, consulCountingSvcSidecar}
	for _, svc := range testConsulServices {
		require.NoError(consulClient.Agent().ServiceRegister(&svc))
	}
	ui := cli.NewMockUi()
	cmd := Command{
		UI:                                 ui,
		consulClient:                       consulClient,
		ProxyIDFile:                        randFileName,
		ServiceRegistrationPollingAttempts: 3,
	}
	expErr := fmt.Sprintf("Unable to write proxyid out: open %s: no such file or directory\n", randFileName)
	code := cmd.Run(defaultTestFlags)
	require.Equal(1, code)
	require.Equal(expErr, ui.ErrorWriter.String())
}

// TestRun_ServiceRegistrationFailsWithBadServerResponses tests error handling with invalid server responses.
func TestRun_ServiceRegistrationFailsWithBadServerResponses(t *testing.T) {
	require := require.New(t)
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
			require.NoError(err)
			clientConfig := &api.Config{Address: serverURL.String()}
			client, err := consul.NewClient(clientConfig)
			require.NoError(err)

			// Setup the Command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				consulClient:                       client,
				BearerTokenFile:                    bearerFile,
				TokenSinkFile:                      tokenFile,
				ServiceRegistrationPollingAttempts: 2,
			}

			flags := []string{
				"-pod-name", testPodName, "-pod-namespace", testPodNamespace,
				"-meta", testPodMeta, "-acl-auth-method", testAuthMethod,
				"-skip-service-registration-polling=false"}
			code := cmd.Run(flags)
			require.Equal(1, code)
			require.Contains(ui.ErrorWriter.String(), c.expErr)
		})
	}
}

// TestRun_RetryACLLoginFails tests that after retry the command fails.
func TestRun_RetryACLLoginFails(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	code := cmd.Run([]string{"-pod-name", testPodName, "-pod-namespace", testPodNamespace,
		"-acl-auth-method", testAuthMethod, "-meta", testPodMeta})
	require.Equal(1, code)
	require.Contains(ui.ErrorWriter.String(), "Hit maximum retries for consul login")
}

// Tests ACL Login with Retries.
func TestRun_LoginwithRetries(t *testing.T) {
	require := require.New(t)
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
			require.NoError(err)
			clientConfig := &api.Config{Address: serverURL.String()}
			client, err := consul.NewClient(clientConfig)
			require.NoError(err)

			ui := cli.NewMockUi()
			cmd := Command{
				UI:              ui,
				consulClient:    client,
				TokenSinkFile:   tokenFile,
				BearerTokenFile: bearerFile,
				ProxyIDFile:     proxyFile,
			}
			code := cmd.Run([]string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-acl-auth-method", testAuthMethod, "-meta", testPodMeta,
				"-skip-service-registration-polling=false"})
			require.Equal(c.ExpCode, code)
			// Cmd will return 1 after numACLLoginRetries, so bound LoginAttemptsCount if we exceeded it.
			require.Equal(c.LoginAttemptsCount, counter)
			// Validate that the token was written to disk if we succeeded.
			data, err := ioutil.ReadFile(tokenFile)
			require.NoError(err)
			require.Contains(string(data), "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586")
			// Validate contents of proxyFile.
			proxydata, err := ioutil.ReadFile(proxyFile)
			require.NoError(err)
			require.Contains(string(proxydata), "counting-counting-sidecar-proxy")
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

// This function starts the command asynchronously and returns a non-blocking chan.
// When finished, the command will send its exit code to the channel.
// Note that it's the responsibility of the caller to terminate the command by calling stopCommand,
// otherwise it can run forever.
func runCommandAsynchronously(cmd *Command, args []string) chan int {
	// We have to run cmd.init() to ensure that the channel the command is
	// using to watch for os interrupts is initialized. If we don't do this,
	// then if stopCommand is called immediately, it will block forever
	// because it calls interrupt() which will attempt to send on a nil channel.
	cmd.init()
	exitChan := make(chan int, 1)
	go func() {
		exitChan <- cmd.Run(args)
	}()
	return exitChan
}
