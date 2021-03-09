package common

import (
	"fmt"
	"io/ioutil"
	"math/rand"
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

	counter := 0
	bearerTokenFile := WriteTempFile(t, "foo")
	tokenFile := WriteTempFile(t, "")

	client := startMockServer(t, &counter)
	err := ConsulLogin(
		client,
		bearerTokenFile,
		testAuthMethod,
		tokenFile,
		testPodMeta,
	)
	require.NoError(err)
	require.Equal(counter, 1)
	// Validate that the token file was written to disk.
	data, err := ioutil.ReadFile(tokenFile)
	require.NoError(err)
	require.Equal(string(data), "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586")
}

func TestConsulLogin_EmptyBearerTokenFile(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	bearerTokenFile := WriteTempFile(t, "")
	err := ConsulLogin(
		nil,
		bearerTokenFile,
		testAuthMethod,
		"",
		testPodMeta,
	)
	require.EqualError(err, fmt.Sprintf("no bearer token found in %s", bearerTokenFile))
}

func TestConsulLogin_BearerTokenFileDoesNotExist(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	randFileName := fmt.Sprintf("/foo/%d/%d", rand.Int(), rand.Int())
	err := ConsulLogin(
		nil,
		randFileName,
		testAuthMethod,
		"",
		testPodMeta,
	)
	require.Error(err)
	require.Contains(err.Error(), "unable to read bearerTokenFile")
}

func TestConsulLogin_TokenFileUnwritable(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	counter := 0
	bearerTokenFile := WriteTempFile(t, "foo")
	client := startMockServer(t, &counter)
	randFileName := fmt.Sprintf("/foo/%d/%d", rand.Int(), rand.Int())
	err := ConsulLogin(
		client,
		bearerTokenFile,
		testAuthMethod,
		randFileName,
		testPodMeta,
	)
	require.Error(err)
	require.Contains(err.Error(), "error writing token to file sink")
}

// startMockServer starts an httptest server used to mock a Consul server's
// /v1/acl/login endpoint. apiCallCounter will be incremented on each call to /v1/acl/login.
// It returns a consul client pointing at the server.
func startMockServer(t *testing.T, apiCallCounter *int) *api.Client {

	// Start the Consul server.
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
			*apiCallCounter++
		}
		w.Write([]byte(testLoginResponse))
	}))
	t.Cleanup(consulServer.Close)

	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(t, err)
	clientConfig := &api.Config{Address: serverURL.String()}
	client, err := api.NewClient(clientConfig)
	require.NoError(t, err)

	return client
}

const testAuthMethod = "consul-k8s-auth-method"
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

var testPodMeta = map[string]string{"pod": "default/podName"}
