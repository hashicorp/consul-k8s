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

// TestRun_ServicePollingWithACLs bootstraps and starts a consul server using a mock
// kubernetes server to provide responses for setting up the consul AuthMethod
// then validates that the command runs end to end successfully.
func TestRun_ServicePollingWithACLs(t *testing.T) {
	t.Parallel()
	bearerFile := common.WriteTempFile(t, serviceAccountJWTToken)
	proxyFile := common.WriteTempFile(t, "")
	tokenFile := common.WriteTempFile(t, "")

	// Start Consul server with ACLs enabled and default deny policy.
	var masterToken = "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
	server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.DefaultPolicy = "deny"
		c.ACL.Tokens.Master = masterToken
	})
	defer server.Stop()
	require.NoError(t, err)
	server.WaitForLeader(t)
	consulClient, err := api.NewClient(&api.Config{Address: server.HTTPAddr, Token: masterToken})
	require.NoError(t, err)

	// Start the mock k8s server.
	k8sMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		if r != nil && r.URL.Path == "/apis/authentication.k8s.io/v1/tokenreviews" && r.Method == "POST" {
			w.Write([]byte(tokenReviewFoundResponse))
		}
		if r != nil && r.URL.Path == "/api/v1/namespaces/default/serviceaccounts/counting" && r.Method == "GET" {
			w.Write([]byte(readServiceAccountFound))
		}
	}))
	defer k8sMockServer.Close()

	// Set up Consul's auth method.
	authMethodTmpl := api.ACLAuthMethod{
		Name:        testAuthMethod,
		Description: "Kubernetes Auth Method",
		Type:        "kubernetes",
		Config: map[string]interface{}{
			"Host":              k8sMockServer.URL,
			"CACert":            serviceAccountCACert,
			"ServiceAccountJWT": serviceAccountJWTToken,
		},
	}
	_, _, err = consulClient.ACL().AuthMethodCreate(&authMethodTmpl, nil)
	require.NoError(t, err)

	// Create the binding rule.
	aclBindingRule := api.ACLBindingRule{
		Description: "Kubernetes binding rule",
		AuthMethod:  testAuthMethod,
		BindType:    api.BindingRuleBindTypeService,
		BindName:    "${serviceaccount.name}",
		Selector:    "serviceaccount.name!=default",
	}
	_, _, err = consulClient.ACL().BindingRuleCreate(&aclBindingRule, nil)
	require.NoError(t, err)

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
	flags := []string{"-pod-name", testPodName,
		"-pod-namespace", testPodNamespace,
		"-acl-auth-method", testAuthMethod,
		"-http-addr", server.HTTPAddr,
		"-skip-service-registration-polling=false"}
	// Run the command.
	code := cmd.Run(flags)
	require.Equal(t, 0, code, ui.ErrorWriter.String())

	// Validate the ACL token was written.
	tokenData, err := ioutil.ReadFile(tokenFile)
	require.NoError(t, err)
	require.NotEmpty(t, tokenData)

	// Check that the token has the metadata with pod name and pod namespace.
	consulClient,
		err = api.NewClient(&api.Config{Address: server.HTTPAddr, Token: string(tokenData)})
	require.NoError(t, err)
	token, _, err := consulClient.ACL().TokenReadSelf(nil)
	require.NoError(t, err)
	require.Equal(t, token.Description, "token created via login: {\"pod\":\"default/counting\"}")

	// Validate contents of proxyFile.
	data, err := ioutil.ReadFile(proxyFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "counting-counting-sidecar-proxy")
}

// This test validates service polling works in a happy case scenario.
func TestRun_ServicePollingOnly(t *testing.T) {
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
		UI:          ui,
		proxyIDFile: proxyFile,
	}
	flags := []string{"-http-addr", server.HTTPAddr}
	flags = append(flags, defaultTestFlags...)
	code := cmd.Run(flags)
	require.Equal(t, 0, code)

	// Validate contents of proxyFile.
	data, err := ioutil.ReadFile(proxyFile)
	require.NoError(t, err)
	require.Contains(t, string(data), "counting-counting-sidecar-proxy")
}

// TestRun_ServicePollingErrors tests that when registered services could not be found,
// we error out.
func TestRun_ServicePollingErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		services []api.AgentServiceRegistration
		expError string
	}{
		{
			name: "only service is registered; proxy service is missing",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting",
						metaKeyKubeNS:  "default",
					},
				},
			},
			expError: "Timed out waiting for service registration: did not find correct number of services: 1",
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
						metaKeyPodName: "counting",
						metaKeyKubeNS:  "default",
					},
				},
			},
			expError: "Timed out waiting for service registration: did not find correct number of services: 1",
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
			expError: "Timed out waiting for service registration: did not find correct number of services: 0",
		},
		{
			name: "service and proxy with pod-name meta but without k8s-namespace meta",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting",
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
						metaKeyPodName: "counting",
					},
				},
			},
			expError: "Timed out waiting for service registration: did not find correct number of services: 0",
		},
		{
			name: "service and proxy with k8s-namespace meta but pod-name meta",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyKubeNS: "default",
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
						metaKeyKubeNS: "default",
					},
				},
			},
			expError: "Timed out waiting for service registration: did not find correct number of services: 0",
		},
		{
			name: "both services are non-proxy services",
			services: []api.AgentServiceRegistration{
				{
					ID:      "counting-counting",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting",
						metaKeyKubeNS:  "default",
					},
				},
				{
					ID:      "counting-counting-1",
					Name:    "counting",
					Address: "127.0.0.1",
					Meta: map[string]string{
						metaKeyPodName: "counting",
						metaKeyKubeNS:  "default",
					},
				},
			},
			expError: "Timed out waiting for service registration: unable to find registered connect-proxy service",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			proxyFile := common.WriteTempFile(t, "")

			// Start Consul server.
			server, err := testutil.NewTestServerConfigT(t, nil)
			defer server.Stop()
			require.NoError(t, err)
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
				"-skip-service-registration-polling=false",
			}

			code := cmd.Run(flags)
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.expError)
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
		proxyIDFile:                        proxyFile,
		serviceRegistrationPollingAttempts: 10,
	}
	flags := []string{"-http-addr", server.HTTPAddr}
	flags = append(flags, defaultTestFlags...)
	code := cmd.Run(flags)
	require.Equal(t, 0, code)
	// Validate that we hit the retry logic when the service was registered but the proxy service is not registered yet.
	require.Contains(t, ui.OutputWriter.String(), "Unable to find registered services; retrying")

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
		proxyIDFile:                        randFileName,
		serviceRegistrationPollingAttempts: 3,
	}
	expErr := fmt.Sprintf("Unable to write proxy ID to file: open %s: no such file or directory\n", randFileName)
	flags := []string{"-http-addr", server.HTTPAddr}
	flags = append(flags, defaultTestFlags...)
	code := cmd.Run(flags)
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

			// Setup the Command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				bearerTokenFile:                    bearerFile,
				tokenSinkFile:                      tokenFile,
				serviceRegistrationPollingAttempts: 2,
			}

			serverURL, err := url.Parse(consulServer.URL)
			require.NoError(t, err)
			flags := []string{
				"-pod-name", testPodName, "-pod-namespace", testPodNamespace,
				"-acl-auth-method", testAuthMethod,
				"-skip-service-registration-polling=false", "-http-addr", serverURL.String()}
			code := cmd.Run(flags)
			require.Equal(t, 1, code)
			require.Contains(t, ui.ErrorWriter.String(), c.expErr)
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
				"-acl-auth-method", testAuthMethod,
				"-skip-service-registration-polling=false",
				"-http-addr", serverURL.String()})
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
	metaKeyPodName   = "pod-name"
	metaKeyKubeNS    = "k8s-namespace"
	testPodNamespace = "default"
	testPodName      = "counting"
	testAuthMethod   = "consul-k8s-auth-method"

	serviceAccountJWTToken = `eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6ImtoYWtpLWFyYWNobmlkLWNvbnN1bC1jb25uZWN0LWluamVjdG9yLWF1dGhtZXRob2Qtc3ZjLWFjY29obmRidiIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50Lm5hbWUiOiJraGFraS1hcmFjaG5pZC1jb25zdWwtY29ubmVjdC1pbmplY3Rvci1hdXRobWV0aG9kLXN2Yy1hY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZXJ2aWNlLWFjY291bnQudWlkIjoiN2U5NWUxMjktZTQ3My0xMWU5LThmYWEtNDIwMTBhODAwMTIyIiwic3ViIjoic3lzdGVtOnNlcnZpY2VhY2NvdW50OmRlZmF1bHQ6a2hha2ktYXJhY2huaWQtY29uc3VsLWNvbm5lY3QtaW5qZWN0b3ItYXV0aG1ldGhvZC1zdmMtYWNjb3VudCJ9.Yi63MMtzh5MBWKKd3a7dzCJjTITE15ikFy_Tnpdk_AwdwA9J4AMSGEeHN5vWtCuuFjo_lMJqBBPHkK2AqbnoFUj9m5CopWyqICJQlvEOP4fUQ-Rc0W1P_JjU1rZERHG39b5TMLgKPQguyhaiZEJ6CjVtm9wUTagrgiuqYV2iUqLuF6SYNm6SrKtkPS-lqIO-u7C06wVk5m5uqwIVQNpZSIC_5Ls5aLmyZU3nHvH-V7E3HmBhVyZAB76jgKB0TyVX1IOskt9PDFarNtU3suZyCjvqC-UJA6sYeySe4dBNKsKlSZ6YuxUUmn1Rgv32YMdImnsWg8khf-zJvqgWk7B5EA`
	serviceAccountCACert   = `-----BEGIN CERTIFICATE-----
MIIDCzCCAfOgAwIBAgIQKzs7Njl9Hs6Xc8EXou25hzANBgkqhkiG9w0BAQsFADAv
MS0wKwYDVQQDEyQ1OWU2ZGM0MS0yMDhmLTQwOTUtYTI4OS0xZmM3MDBhYzFjYzgw
HhcNMTkwNjA3MTAxNzMxWhcNMjQwNjA1MTExNzMxWjAvMS0wKwYDVQQDEyQ1OWU2
ZGM0MS0yMDhmLTQwOTUtYTI4OS0xZmM3MDBhYzFjYzgwggEiMA0GCSqGSIb3DQEB
AQUAA4IBDwAwggEKAoIBAQDZjHzwqofzTpGpc0MdICS7euvfujUKE3PC/apfDAgB
4jzEFKA78/9+KUGw/c/0SHeSQhN+a8gwlHRnAz1NJcfOIXy4dweUuOkAiFxH8pht
ECwkeNO7z8DoV8ceminCRHGjaRmoMxpZ7g2pZAJNZePxi3y1aNkFAXe9gSUSdjRZ
RXYka7wh2AO9k2dlGFAYB+t3vWwJ6twjG0TtKQrhYM9Od1/oN0E01LzBcZuxkN1k
8gfIHy7bOFCBM2WTEDW/0aAvcAPrO8DLqDJ+6Mjc3r7+zlzl8aQspb0S08pVzki5
Dz//83kyu0phJuij5eB88V7UfPXxXF/EtV6fvrL7MN4fAgMBAAGjIzAhMA4GA1Ud
DwEB/wQEAwICBDAPBgNVHRMBAf8EBTADAQH/MA0GCSqGSIb3DQEBCwUAA4IBAQBv
QsaG6qlcaRktJ0zGhxxJ52NnRV2GcIYPeN3Zv2VXe3ML3Vd6G32PV7lIOhjx3KmA
/uMh6NhqBzsekkTz0PuC3wJyM2OGonVQisFlqx9sFQ3fU2mIGXCa3wC8e/qP8BHS
w7/VeA7lzmj3TQRE/W0U0ZGeoAxn9b6JtT0iMucYvP0hXKTPBWlnzIijamU50r2Y
7ia065Ug2xUN5FLX/vxOA3y4rjpkjWoVQcu1p8TZrVoM3dsGFWp10fDMRiAHTvOH
Z23jGuk6rn9DUHC2xPj3wCTmd8SGEJoV31noJV5dVeQ90wusXz3vTG7ficKnvHFS
xtr5PSwH1DusYfVaGH2O
-----END CERTIFICATE-----`

	readServiceAccountFound = `{
 "kind": "ServiceAccount",
 "apiVersion": "v1",
 "metadata": {
   "name": "counting",
   "namespace": "default",
   "selfLink": "/api/v1/namespaces/default/serviceaccounts/counting",
   "uid": "9ff51ff4-557e-11e9-9687-48e6c8b8ecb5",
   "resourceVersion": "2101",
   "creationTimestamp": "2019-04-02T19:36:34Z"
 },
 "secrets": [
   {
	 "name": "counting-token-m9cvn"
   }
 ]
}`

	tokenReviewFoundResponse = `{
 "kind": "TokenReview",
 "apiVersion": "authentication.k8s.io/v1",
 "metadata": {
   "creationTimestamp": null
 },
 "spec": {
   "token": "eyJhbGciOiJSUzI1NiIsImtpZCI6IiJ9.eyJpc3MiOiJrdWJlcm5ldGVzL3NlcnZpY2VhY2NvdW50Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9uYW1lc3BhY2UiOiJkZWZhdWx0Iiwia3ViZXJuZXRlcy5pby9zZXJ2aWNlYWNjb3VudC9zZWNyZXQubmFtZSI6ImRlbW8tdG9rZW4tbTljdm4iLCJrdWJlcm5ldGVzLmlvL3NlcnZpY2VhY2NvdW50L3NlcnZpY2UtYWNjb3VudC5uYW1lIjoiZGVtbyIsImt1YmVybmV0ZXMuaW8vc2VydmljZWFjY291bnQvc2VydmljZS1hY2NvdW50LnVpZCI6IjlmZjUxZmY0LTU1N2UtMTFlOS05Njg3LTQ4ZTZjOGI4ZWNiNSIsInN1YiI6InN5c3RlbTpzZXJ2aWNlYWNjb3VudDpkZWZhdWx0OmRlbW8ifQ.UJEphtrN261gy9WCl4ZKjm2PRDLDkc3Xg9VcDGfzyroOqFQ6sog5dVAb9voc5Nc0-H5b1yGwxDViEMucwKvZpA5pi7VEx_OskK-KTWXSmafM0Xg_AvzpU9Ed5TSRno-OhXaAraxdjXoC4myh1ay2DMeHUusJg_ibqcYJrWx-6MO1bH_ObORtAKhoST_8fzkqNAlZmsQ87FinQvYN5mzDXYukl-eeRdBgQUBkWvEb-Ju6cc0-QE4sUQ4IH_fs0fUyX_xc0om0SZGWLP909FTz4V8LxV8kr6L7irxROiS1jn3Fvyc9ur1PamVf3JOPPrOyfmKbaGRiWJM32b3buQw7cg"
 },
 "status": {
   "authenticated": true,
   "user": {
	 "username": "system:serviceaccount:default:counting",
	 "uid": "9ff51ff4-557e-11e9-9687-48e6c8b8ecb5",
	 "groups": [
	   "system:serviceaccounts",
	   "system:serviceaccounts:default",
	   "system:authenticated"
	 ]
   }
 }
}`
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
)

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
