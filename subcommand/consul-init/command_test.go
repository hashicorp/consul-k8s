package consulinit

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/hashicorp/consul-k8s/consul"
	"github.com/hashicorp/consul/api"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRun_FlagValidation(t *testing.T) {
	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  []string{"-method", "k8s-fake-auth-method"},
			expErr: "-meta must be set",
		},
		{
			flags:  []string{"-meta", "pod=abcdefg"},
			expErr: "-method must be set",
		},
		/*		{
				// TODO: decide if these are going to be required and update test accordingly
							flags:  []string{"-method", "foot", "-meta", "pod=abcdefg"},
							expErr: "-bearer-token-file must be set",
						},
						{
							flags:  []string{"-method", "foot", "-meta", "pod=abcdefg", "-bearer-token-file", "foot"},
							expErr: "-token-sink-file must be set",
						},
		*/
	}
	for _, c := range cases {
		t.Run(c.expErr, func(t *testing.T) {
			k8sClient := fake.NewSimpleClientset()
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8sClient,
			}
			code := cmd.Run(c.flags)
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

/// TestRun_RetryACLLoginFails tests that after retries the command fails
func TestRun_RetryACLLoginFails(t *testing.T) {
	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	code := cmd.Run([]string{"-method", "k8s-fake-auth-method", "-meta", "pod=default/podname"})
	require.Equal(t, 1, code)
	require.Contains(t, ui.ErrorWriter.String(), "unable to do consul login")
}

// Test that SIGINT/SIGTERM exits the command

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
		{
			Description:        "Login fails after 5 retries",
			TestRetry:          true,
			LoginAttemptsCount: 5,
			ExpCode:            1,
			ExpErr:             "unable to do consul login",
		},
	}
	for _, c := range cases {
		t.Run(c.Description, func(t *testing.T) {
			// Create a token File as input and load with some data.
			bearerTokenFile, err := ioutil.TempFile("", "bearerTokenFile")
			require.NoError(t, err)
			_, err = bearerTokenFile.WriteString("foo")
			require.NoError(t, err)
			// Create an output file.
			tokenFile, err := ioutil.TempFile("", "tokenFile")
			require.NoError(t, err)

			// Start the mock Consul server.
			counter := 0
			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
					counter++
					b := "{\n  \"AccessorID\": \"926e2bd2-b344-d91b-0c83-ae89f372cd9b\",\n  \"SecretID\": \"b78d37c7-0ca7-5f4d-99ee-6d9975ce4586\",\n  \"Description\": \"token created via login\",\n  \"Roles\": [\n    {\n      \"ID\": \"3356c67c-5535-403a-ad79-c1d5f9df8fc7\",\n      \"Name\": \"demo\"\n    }\n  ],\n  \"ServiceIdentities\": [\n    {\n      \"ServiceName\": \"example\"\n    }\n  ],\n  \"Local\": true,\n  \"AuthMethod\": \"minikube\",\n  \"CreateTime\": \"2019-04-29T10:08:08.404370762-05:00\",\n  \"Hash\": \"nLimyD+7l6miiHEBmN/tvCelAmE/SbIXxcnTzG3pbGY=\",\n  \"CreateIndex\": 36,\n  \"ModifyIndex\": 36\n}"
					if !c.TestRetry || (c.TestRetry && c.LoginAttemptsCount == counter) {
						w.Write([]byte(b))
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
				UI:                 ui,
				consulClient:       client,
				numACLLoginRetries: 3, // just here to help visualize # of retries
			}
			code := cmd.Run([]string{"-bearer-token-file", bearerTokenFile.Name(),
				"-method", "consul-k8s-auth-method", "-meta", "pod=default/podname",
				"-token-sink-file", tokenFile.Name()})
			require.Equal(t, c.ExpCode, code)
			// cmd will return 1 after cmd.numACLLoginRetries, so bound LoginAttemptsCount if we exceeded it
			require.Equal(t, min(c.LoginAttemptsCount, cmd.numACLLoginRetries), counter)
			require.Contains(t, ui.ErrorWriter.String(), c.ExpErr)
			if c.ExpErr == "" {
				// validate that the token was written to disk if we succeeded
				data, err := ioutil.ReadFile(tokenFile.Name())
				require.NoError(t, err)
				require.Contains(t, string(data), "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586")
			}
		})
	}
}

func min(x, y int) int {
	if x > y {
		return y
	}
	return x
}
