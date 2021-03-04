package common

import (
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/stretchr/testify/require"
)

func TestLogger_InvalidLogLevel(t *testing.T) {
	_, err := Logger("invalid")
	require.EqualError(t, err, "unknown log level: invalid")
}

func TestLogger(t *testing.T) {
	lgr, err := Logger("debug")
	require.NoError(t, err)
	require.NotNil(t, lgr)
	require.True(t, lgr.IsDebug())
}

func TestValidatePort(t *testing.T) {
	err := ValidatePort("-test-flag-name", "1234")
	require.NoError(t, err)
	err = ValidatePort("-test-flag-name", "invalid-port")
	require.EqualError(t, err, "-test-flag-name value of invalid-port is not a valid integer.")
	err = ValidatePort("-test-flag-name", "22")
	require.EqualError(t, err, "-test-flag-name value of 22 is not in the port range 1024-65535.")
}

// TestConsulLogin ensures that our implementation of consul login hits `/v1/acl/login`.
func TestConsulLogin(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall

	// Stub the read of the bearerTokenFile inside the login function.
	bearerTokenFile, err := ioutil.TempFile("", "bearerTokenFile")
	require.NoError(err)
	_, err = bearerTokenFile.WriteString("foo")
	require.NoError(err)
	tokenFile, err := ioutil.TempFile("", "tokenFile")
	require.NoError(err)

	// Start the Consul server.
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})
		b := "{\n  \"AccessorID\": \"926e2bd2-b344-d91b-0c83-ae89f372cd9b\",\n  \"SecretID\": \"b78d37c7-0ca7-5f4d-99ee-6d9975ce4586\",\n  \"Description\": \"token created via login\",\n  \"Roles\": [\n    {\n      \"ID\": \"3356c67c-5535-403a-ad79-c1d5f9df8fc7\",\n      \"Name\": \"demo\"\n    }\n  ],\n  \"ServiceIdentities\": [\n    {\n      \"ServiceName\": \"example\"\n    }\n  ],\n  \"Local\": true,\n  \"AuthMethod\": \"minikube\",\n  \"CreateTime\": \"2019-04-29T10:08:08.404370762-05:00\",\n  \"Hash\": \"nLimyD+7l6miiHEBmN/tvCelAmE/SbIXxcnTzG3pbGY=\",\n  \"CreateIndex\": 36,\n  \"ModifyIndex\": 36\n}"
		w.Write([]byte(b))
	}))
	defer consulServer.Close()
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	clientConfig := &api.Config{Address: serverURL.String()}
	client, err := api.NewClient(clientConfig)
	require.NoError(err)

	err = ConsulLogin(
		client,
		bearerTokenFile.Name(),
		"foo",
		tokenFile.Name(),
		nil,
	)
	require.NoError(err)
	// Ensure that the /v1/acl/login url was correctly hit.
	require.Equal([]APICall{
		{
			"POST",
			"/v1/acl/login",
		},
	}, consulAPICalls)
	// validate that the token file was written to disk
	data, err := ioutil.ReadFile(tokenFile.Name())
	require.NoError(err)
	require.Contains(string(data), "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586")
}
