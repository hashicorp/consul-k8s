package connectinit

import (
	"fmt"
	"github.com/hashicorp/consul-k8s/consul"
	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// Tests:
//  ACLS disabled (polling on/off)
//  ACLS enabled (polling on/off)
// TODO: when endpoints controller is ready "polling" as a flag will be removed and this will just be a test of the command
// passing with ACLs enabled or disabled.
// TestRun_LoginAndPolling tests basic happy path of combinations of ACL/Polling
func TestRun_LoginAndPolling(t *testing.T) {
	cases := []struct {
		name    string
		flags   []string
		secure  bool
		polling bool
	}{
		{
			name: "acls enabled, registration polling enabled",
			flags: []string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-acl-auth-method", testAuthMethod, "-meta", testPodMeta,
				"-service-account-name", testServiceAccountName},
			secure:  true,
			polling: true,
		},
		{
			name: "acls enabled, registration polling disabled",
			flags: []string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-acl-auth-method", testAuthMethod, "-meta", testPodMeta,
				"-service-account-name", testServiceAccountName},
			secure:  true,
			polling: false,
		},
		{
			name:    "acls disabled, registration polling enabled",
			flags:   []string{"-pod-name", testPodName, "-pod-namespace", testPodNamespace},
			secure:  false,
			polling: true,
		},
		{
			name:    "acls disabled, registration polling disabled",
			flags:   []string{"-pod-name", testPodName, "-pod-namespace", testPodNamespace},
			secure:  false,
			polling: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			extraFlags := []string{}
			bearerTokenFile := common.WriteTempFile(t, "bearerTokenFile")
			proxyFile := common.WriteTempFile(t, "")
			tokenFile := common.WriteTempFile(t, "")
			extraFlags = append(extraFlags, "-bearer-token-file", bearerTokenFile, "-token-sink-file", tokenFile, "-proxyid-file", proxyFile)

			// Start the mock Consul server.
			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if c.secure {
					// ACL login request
					if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
						w.Write([]byte(testLoginResponse))
					}
					// Agent service get, this is used when ACLs are enabled
					if r != nil && r.URL.Path == fmt.Sprintf("/v1/agent/service/%s", testServiceAccountName) && r.Method == "GET" {
						w.Write([]byte(testServiceGetResponse))
					}
				} else {
					// Agent services list request, used when ACLs are disabled
					if r != nil && r.URL.Path == "/v1/agent/services" && r.Method == "GET" {
						w.Write([]byte(testServiceListResponse))
					}
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
				UI:           ui,
				consulClient: client,
			}
			c.flags = append(c.flags, extraFlags...)
			c.flags = append(c.flags, fmt.Sprintf("-skip-service-registration-polling=%t", !c.polling))
			code := cmd.Run(c.flags)
			require.Equal(t, 0, code)
		})
	}
}

func TestRun_FlagValidation(t *testing.T) {
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
		{
			flags:  []string{"-pod-name", testPodName, "-pod-namespace", testPodNamespace, "-acl-auth-method", testAuthMethod, "-meta", "pod=default/foo"},
			expErr: "-service-account-name must be set",
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

// TestRun_RetryACLLoginFails tests that after retries the command fails
func TestRun_RetryACLLoginFails(t *testing.T) {
	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	code := cmd.Run([]string{"-pod-name", testPodName, "-pod-namespace", testPodNamespace,
		"-acl-auth-method", testAuthMethod, "-meta", testPodMeta,
		"-skip-service-registration-polling", "-service-account-name", testServiceAccountName})
	require.Equal(t, 1, code)
	require.Contains(t, ui.ErrorWriter.String(), "Hit maximum retries for consul login")
}

func TestRun_LoginwithRetries(t *testing.T) {
	cases := []struct {
		Description        string
		TestRetry          bool
		LoginAttemptsCount int
		ExpCode            int
	}{
		{
			Description:        "Login succeeds without retries",
			TestRetry:          false,
			LoginAttemptsCount: 1, // 1 because we dont actually retry
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
			bearerTokenFile := common.WriteTempFile(t, "bearerTokenFile")
			tokenFile := common.WriteTempFile(t, "")

			// Start the mock Consul server.
			counter := 0
			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
					counter++
					if !c.TestRetry || (c.TestRetry && c.LoginAttemptsCount == counter) {
						w.Write([]byte(testLoginResponse))
					}
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
				UI:           ui,
				consulClient: client,
			}
			code := cmd.Run([]string{
				"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-token-sink-file", tokenFile, "-bearer-token-file", bearerTokenFile,
				"-acl-auth-method", testAuthMethod, "-meta", testPodMeta,
				"-service-account-name", testServiceAccountName,
				"-skip-service-registration-polling"})
			require.Equal(t, c.ExpCode, code)
			// Cmd will return 1 after numACLLoginRetries, so bound LoginAttemptsCount if we exceeded it.
			require.Equal(t, c.LoginAttemptsCount, counter)
			// Validate that the token was written to disk if we succeeded.
			data, err := ioutil.ReadFile(tokenFile)
			require.NoError(t, err)
			require.Contains(t, "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586", string(data))
		})
	}
}

const testPodNamespace = "default"
const testPodName = "counting"
const testServiceAccountName = "counting"
const testPodMeta = "pod=default/counting"
const testAuthMethod = "consul-k8s-auth-method"

const testServiceGetResponse = `{
  "ID": "counting-counting",
  "Service": "counting",
  "Tags": [],
  "Meta": {
    "k8s-namespace": "default",
    "pod-name": "counting"
  },
  "Port": 9001,
  "Address": "10.32.3.22",
  "TaggedAddresses": {
    "lan_ipv4": {
      "Address": "10.32.3.22",
      "Port": 9001
    },
    "wan_ipv4": {
      "Address": "10.32.3.22",
      "Port": 9001
    }
  },
  "Weights": {
    "Passing": 1,
    "Warning": 1
  },
  "EnableTagOverride": false,
  "ContentHash": "43efce0313d03c9",
  "Datacenter": "dc1"
}
`

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
