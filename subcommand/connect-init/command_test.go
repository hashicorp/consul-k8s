package connectinit

import (
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

func TestRun_FlagValidation(t *testing.T) {
	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  []string{"-acl-auth-method", testAuthMethod},
			expErr: "-meta must be set",
		},
		{
			flags:  []string{"-meta", "pod=default/foo"},
			expErr: "-acl-auth-method must be set",
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
	code := cmd.Run([]string{"-acl-auth-method", testAuthMethod, "-meta", testPodMeta})
	require.Equal(t, 1, code)
	require.Contains(t, ui.ErrorWriter.String(), "Hit maximum retries for consul login")
}

func TestRun_withRetries(t *testing.T) {
	cases := []struct {
		Description        string
		TestRetry          bool
		LoginAttemptsCount int
		ExpCode            int
		ExpErr             string
	}{
		{
			Description:        "Login succeeds without retries",
			TestRetry:          false,
			LoginAttemptsCount: 1, // 1 because we dont actually retry
			ExpCode:            0,
			ExpErr:             "",
		},
		{
			Description:        "Login succeeds after 1 retry",
			TestRetry:          true,
			LoginAttemptsCount: 2,
			ExpCode:            0,
			ExpErr:             "",
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
						w.Write([]byte(loginResponse))
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
			code := cmd.Run([]string{"-bearer-token-file", bearerTokenFile,
				"-token-sink-file", tokenFile,
				"-meta", "host=foo",
				"-acl-auth-method", testAuthMethod, "-meta", testPodMeta})
			require.Equal(t, c.ExpCode, code)
			// Cmd will return 1 after numACLLoginRetries, so bound LoginAttemptsCount if we exceeded it.
			require.Equal(t, c.LoginAttemptsCount, counter)
			require.Contains(t, ui.ErrorWriter.String(), c.ExpErr)
			if c.ExpErr == "" {
				// Validate that the token was written to disk if we succeeded.
				data, err := ioutil.ReadFile(tokenFile)
				require.NoError(t, err)
				require.Contains(t, string(data), "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586")
			}
		})
	}
}

const testAuthMethod = "consul-k8s-auth-method"

// sample response from https://consul.io/api-docs/acl#sample-response
const loginResponse = `{
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

const testPodMeta = "pod=default/podName"
