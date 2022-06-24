package common

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

	"github.com/hashicorp/consul-k8s/control-plane/helper/go-discover/mocks"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-discover"
	"github.com/hashicorp/go-hclog"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestLogger_InvalidLogLevel(t *testing.T) {
	_, err := Logger("invalid", false)
	require.EqualError(t, err, "unknown log level: invalid")
}

func TestZapLogger_InvalidLogLevel(t *testing.T) {
	_, err := ZapLogger("invalid", false)
	require.EqualError(t, err, "unknown log level \"invalid\": unrecognized level: \"invalid\"")
}

// ZapLogger should convert "trace" log level to "debug".
func TestZapLogger_TraceLogLevel(t *testing.T) {
	_, err := ZapLogger("trace", false)
	require.NoError(t, err)
}

func TestLogger(t *testing.T) {
	lgr, err := Logger("debug", false)
	require.NoError(t, err)
	require.NotNil(t, lgr)
	require.True(t, lgr.IsDebug())
}

func TestValidateUnprivilegedPort(t *testing.T) {
	err := ValidateUnprivilegedPort("-test-flag-name", "1234")
	require.NoError(t, err)
	err = ValidateUnprivilegedPort("-test-flag-name", "invalid-port")
	require.EqualError(t, err, "-test-flag-name value of invalid-port is not a valid integer")
	err = ValidateUnprivilegedPort("-test-flag-name", "22")
	require.EqualError(t, err, "-test-flag-name value of 22 is not in the unprivileged port range 1024-65535")
}

// TestConsulLogin ensures that our implementation of consul login hits `/v1/acl/login`.
func TestConsulLogin(t *testing.T) {
	t.Parallel()

	bearerTokenFile := WriteTempFile(t, "foo")
	tokenFile := WriteTempFile(t, "")

	// This is a common.Logger.
	log, err := Logger("INFO", false)
	require.NoError(t, err)
	client := startMockServer(t)
	params := LoginParams{
		AuthMethod:      testAuthMethod,
		Datacenter:      "dc1",
		BearerTokenFile: bearerTokenFile,
		TokenSinkFile:   tokenFile,
	}
	_, err = ConsulLogin(client, params, log)
	require.NoError(t, err)
	// Validate that the token file was written to disk.
	data, err := ioutil.ReadFile(tokenFile)
	require.NoError(t, err)
	require.Equal(t, string(data), "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586")
}

// TestConsulLogin_Retries tests we retry /v1/acl/login call if it fails.
func TestConsulLogin_Retries(t *testing.T) {
	t.Parallel()

	numLoginCalls := 0
	bearerTokenFile := WriteTempFile(t, "foo")
	tokenFile := WriteTempFile(t, "")

	// This is a common.Logger.
	log, err := Logger("INFO", false)
	require.NoError(t, err)
	// Start the Consul server.
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
			if numLoginCalls == 0 {
				w.WriteHeader(500)
			} else {
				w.Write([]byte(testLoginResponse))
			}
			numLoginCalls++
		}
		if r != nil && r.URL.Path == "/v1/acl/token/self" && r.Method == "GET" {
			w.Write([]byte(testLoginResponse))
		}
	}))
	t.Cleanup(consulServer.Close)

	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(t, err)
	clientConfig := &api.Config{Address: serverURL.String()}
	client, err := api.NewClient(clientConfig)
	require.NoError(t, err)
	params := LoginParams{
		AuthMethod:      testAuthMethod,
		Datacenter:      "dc1",
		BearerTokenFile: bearerTokenFile,
		TokenSinkFile:   tokenFile,
	}
	_, err = ConsulLogin(client, params, log)
	require.NoError(t, err)
	require.Equal(t, 2, numLoginCalls)
	// Validate that the token file was written to disk.
	data, err := ioutil.ReadFile(tokenFile)
	require.NoError(t, err)
	require.Equal(t, string(data), "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586")
}

// TestConsulLogin_TokenNotReplicated tests that if we can't read the token in stale consistency mode
// we return an error.
func TestConsulLogin_TokenNotReplicated(t *testing.T) {
	t.Parallel()

	bearerTokenFile := WriteTempFile(t, "foo")
	tokenFile := WriteTempFile(t, "")

	// This is a common.Logger.
	log, err := Logger("INFO", false)
	require.NoError(t, err)
	// Start the Consul server.
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
			w.Write([]byte(testLoginResponse))
		}
		if r != nil && r.URL.Path == "/v1/acl/token/self" && r.Method == "GET" {
			w.WriteHeader(500)
		}
	}))
	t.Cleanup(consulServer.Close)

	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(t, err)
	clientConfig := &api.Config{Address: serverURL.String()}
	client, err := api.NewClient(clientConfig)
	require.NoError(t, err)
	params := LoginParams{
		AuthMethod:      testAuthMethod,
		Datacenter:      "dc1",
		BearerTokenFile: bearerTokenFile,
		TokenSinkFile:   tokenFile,
	}
	_, err = ConsulLogin(client, params, log)
	require.EqualError(t, err, "Unexpected response code: 500 ()")
}

func TestConsulLogin_EmptyBearerTokenFile(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	bearerTokenFile := WriteTempFile(t, "")
	params := LoginParams{
		BearerTokenFile: bearerTokenFile,
	}
	_, err := ConsulLogin(nil, params, hclog.NewNullLogger())
	require.EqualError(err, fmt.Sprintf("no bearer token found in %q", bearerTokenFile))
}

func TestConsulLogin_BearerTokenFileDoesNotExist(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	randFileName := fmt.Sprintf("/foo/%d/%d", rand.Int(), rand.Int())
	params := LoginParams{
		BearerTokenFile: randFileName,
	}
	_, err := ConsulLogin(nil, params, hclog.NewNullLogger())
	require.Error(err)
	require.Contains(err.Error(), "unable to read bearer token file")
}

func TestConsulLogin_TokenFileUnwritable(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	bearerTokenFile := WriteTempFile(t, "foo")
	client := startMockServer(t)
	// This is a common.Logger.
	log, err := Logger("INFO", false)
	require.NoError(err)
	randFileName := fmt.Sprintf("/foo/%d/%d", rand.Int(), rand.Int())
	params := LoginParams{
		AuthMethod:      testAuthMethod,
		BearerTokenFile: bearerTokenFile,
		TokenSinkFile:   randFileName,
		NumRetries:      2,
	}
	_, err = ConsulLogin(client, params, log)
	require.Error(err)
	require.Contains(err.Error(), "error writing token to file sink")
}

func TestWriteFileWithPerms_InvalidOutputFile(t *testing.T) {
	t.Parallel()
	rand.Seed(time.Now().UnixNano())
	randFileName := fmt.Sprintf("/tmp/tmp/tmp/%d", rand.Int())
	t.Cleanup(func() {
		os.Remove(randFileName)
	})
	err := WriteFileWithPerms(randFileName, "", os.FileMode(0444))
	require.Errorf(t, err, "unable to create file: %s", randFileName)
}

func TestWriteFileWithPerms_OutputFileExists(t *testing.T) {
	t.Parallel()
	rand.Seed(time.Now().UnixNano())
	randFileName := fmt.Sprintf("/tmp/%d", rand.Int())
	err := ioutil.WriteFile(randFileName, []byte("foo"), os.FileMode(0444))
	require.NoError(t, err)
	t.Cleanup(func() {
		os.Remove(randFileName)
	})
	payload := "abcd"
	err = WriteFileWithPerms(randFileName, payload, os.FileMode(0444))
	require.NoError(t, err)
	data, err := ioutil.ReadFile(randFileName)
	require.NoError(t, err)
	require.Equal(t, payload, string(data))
}

func TestWriteFileWithPerms(t *testing.T) {
	t.Parallel()
	payload := "foo-foo-foo-foo"
	rand.Seed(time.Now().UnixNano())
	randFileName := fmt.Sprintf("/tmp/%d", rand.Int())
	t.Cleanup(func() {
		os.Remove(randFileName)
	})
	// Issue the write.
	mode := os.FileMode(0444)
	err := WriteFileWithPerms(randFileName, payload, mode)
	require.NoError(t, err)
	file, err := os.Stat(randFileName)
	require.NoError(t, err)
	// Validate the size and mode are correct.
	require.Equal(t, file.Mode(), mode)
	require.Equal(t, file.Size(), int64(len(payload)))
	// Validate the data was written correctly.
	data, err := ioutil.ReadFile(randFileName)
	require.NoError(t, err)
	require.Equal(t, payload, string(data))
}

func TestGetResolvedServerAddresses(t *testing.T) {
	cases := map[string]struct {
		inputServerAddresses    []string
		providerMap             func() map[string]discover.Provider
		expectedServerAddresses []string
	}{
		"without providers and single address": {
			inputServerAddresses: []string{"foo.bar"},
			providerMap: func() map[string]discover.Provider {
				return nil
			},
			expectedServerAddresses: []string{"foo.bar"},
		},
		"without providers and multiple addresses": {
			inputServerAddresses: []string{"foo.bar", "hello.car"},
			providerMap: func() map[string]discover.Provider {
				return nil
			},
			expectedServerAddresses: []string{"foo.bar", "hello.car"},
		},
		"mock provider": {
			inputServerAddresses: []string{"provider=mock"},
			providerMap: func() map[string]discover.Provider {
				provider := new(mocks.MockProvider)
				provider.On("Addrs", mock.Anything, mock.Anything).Return([]string{"127.0.0.1", "foo.bar"}, nil)
				providers := make(map[string]discover.Provider)
				providers["mock"] = provider
				return providers
			},
			expectedServerAddresses: []string{"127.0.0.1", "foo.bar"},
		},
	}
	for _, testCase := range cases {
		addresses, err := GetResolvedServerAddresses(testCase.inputServerAddresses, testCase.providerMap(), hclog.NewNullLogger())
		require.NoError(t, err)
		require.Equal(t, testCase.expectedServerAddresses, addresses)
	}
}

// startMockServer starts an httptest server used to mock a Consul server's
// /v1/acl/login endpoint. apiCallCounter will be incremented on each call to /v1/acl/login.
// It returns a consul client pointing at the server.
func startMockServer(t *testing.T) *api.Client {
	// Start the Consul server.
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		if r != nil && r.URL.Path == "/v1/acl/login" && r.Method == "POST" {
			w.Write([]byte(testLoginResponse))
		}
		if r != nil && r.URL.Path == "/v1/acl/token/self" && r.Method == "GET" {
			w.Write([]byte(testLoginResponse))
		}
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
