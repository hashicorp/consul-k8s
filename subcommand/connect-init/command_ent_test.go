// +build enterprise

package connectinit

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/namespaces"
	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func TestRun_ServicePollingWithACLsAndTLSWithNamespaces(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                   string
		tls                    bool
		consulServiceNamespace string
		authMethod             string
		authMethodNamespace    string
	}{
		{
			name:                   "ACLs enabled, no tls, serviceNS=default, authMethodNS=default",
			tls:                    false,
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
			authMethod:             "consul-k8s-auth-method",
		},
		{
			name:                   "ACLs enabled, tls, serviceNS=default, authMethodNS=default",
			tls:                    true,
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
			authMethod:             "consul-k8s-auth-method",
		},
		{
			name:                   "ACLs enabled, no tls, serviceNS=default-ns, authMethodNS=default",
			tls:                    false,
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
			authMethod:             "consul-k8s-auth-method",
		},
		{
			name:                   "ACLs enabled, tls, serviceNS=default-ns, authMethodNS=default",
			tls:                    true,
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
			authMethod:             "consul-k8s-auth-method",
		},
		{
			name:                   "ACLs enabled, no tls, serviceNS=other, authMethodNS=other",
			tls:                    false,
			consulServiceNamespace: "other",
			authMethodNamespace:    "other",
			authMethod:             "consul-k8s-auth-method",
		},
		{
			name:                   "ACLs enabled, tls, serviceNS=other, authMethodNS=other",
			tls:                    true,
			consulServiceNamespace: "other",
			authMethodNamespace:    "other",
			authMethod:             "consul-k8s-auth-method",
		},
		{
			name:                   "ACLs disabled, no tls, serviceNS=default, authMethodNS=default",
			tls:                    false,
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
		},
		{
			name:                   "ACLs disabled, tls, serviceNS=default, authMethodNS=default",
			tls:                    true,
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
		},
		{
			name:                   "ACLs disabled, no tls, serviceNS=default-ns, authMethodNS=default",
			tls:                    false,
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
		},
		{
			name:                   "ACLs disabled, tls, serviceNS=default-ns, authMethodNS=default",
			tls:                    true,
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
		},
		{
			name:                   "ACLs disabled, no tls, serviceNS=other, authMethodNS=other",
			tls:                    false,
			consulServiceNamespace: "other",
			authMethodNamespace:    "other",
		},
		{
			name:                   "ACLs disabled, tls, serviceNS=other, authMethodNS=other",
			tls:                    true,
			consulServiceNamespace: "other",
			authMethodNamespace:    "other",
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			bearerFile := common.WriteTempFile(t, serviceAccountJWTToken)
			tokenFile := fmt.Sprintf("/tmp/%d1", rand.Int())
			proxyFile := fmt.Sprintf("/tmp/%d2", rand.Int())
			t.Cleanup(func() {
				os.Remove(proxyFile)
				os.Remove(tokenFile)
			})

			var caFile, certFile, keyFile string
			// Start Consul server with ACLs enabled and default deny policy.
			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
			server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				if test.authMethod != "" {
					c.ACL.Enabled = true
					c.ACL.DefaultPolicy = "deny"
					c.ACL.Tokens.Master = masterToken
				}
				if test.tls {
					caFile, certFile, keyFile = common.GenerateServerCerts(t)
					c.CAFile = caFile
					c.CertFile = certFile
					c.KeyFile = keyFile
				}
			})
			require.NoError(t, err)
			defer server.Stop()
			server.WaitForLeader(t)
			cfg := &api.Config{
				Scheme:    "http",
				Address:   server.HTTPAddr,
				Namespace: test.consulServiceNamespace,
			}
			if test.authMethod != "" {
				cfg.Token = masterToken
			}
			if test.tls {
				cfg.Address = server.HTTPSAddr
				cfg.Scheme = "https"
				cfg.TLSConfig = api.TLSConfig{
					CAFile: caFile,
				}
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			_, err = namespaces.EnsureExists(consulClient, test.consulServiceNamespace, "")
			require.NoError(t, err)

			// Start the mock k8s server.
			k8sMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				if r != nil && r.URL.Path == "/apis/authentication.k8s.io/v1/tokenreviews" && r.Method == "POST" {
					w.Write([]byte(tokenReviewFoundResponseForNamespaces))
				}
				if r != nil && r.URL.Path == "/api/v1/namespaces/default-ns/serviceaccounts/counting" && r.Method == "GET" {
					w.Write([]byte(readServiceAccountFoundForNamespaces))
				}
			}))
			defer k8sMockServer.Close()

			if test.authMethod != "" {
				// Set up Consul's auth method.
				authMethod := &api.ACLAuthMethod{
					Name:        testAuthMethod,
					Type:        "kubernetes",
					Description: "Kubernetes Auth Method",
					Config: map[string]interface{}{
						"Host":              k8sMockServer.URL,
						"CACert":            serviceAccountCACert,
						"ServiceAccountJWT": serviceAccountJWTToken,
					},
					Namespace: test.authMethodNamespace,
				}
				// This will be the case when we are emulating "namespace mirroring" where the
				// authMethodNamespace is not equal to the consulServiceNamespace.
				if test.authMethodNamespace != test.consulServiceNamespace {
					authMethod.Config["MapNamespaces"] = true
				}
				_, _, err = consulClient.ACL().AuthMethodCreate(authMethod, &api.WriteOptions{Namespace: test.authMethodNamespace})
				require.NoError(t, err)

				// Create the binding rule.
				aclBindingRule := api.ACLBindingRule{
					Description: "Kubernetes binding rule",
					AuthMethod:  testAuthMethod,
					BindType:    api.BindingRuleBindTypeService,
					BindName:    "${serviceaccount.name}",
					Selector:    "serviceaccount.name!=default",
					Namespace:   test.authMethodNamespace,
				}
				_, _, err = consulClient.ACL().BindingRuleCreate(&aclBindingRule, &api.WriteOptions{Namespace: test.authMethodNamespace})
				require.NoError(t, err)
			}

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
				serviceRegistrationPollingAttempts: 5,
			}
			// We build the http-addr because normally it's defined by the init container setting
			// CONSUL_HTTP_ADDR when it processes the command template.
			flags := []string{"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-acl-auth-method", test.authMethod,
				"-service-account-name", testServiceAccountName,
				"-http-addr", fmt.Sprintf("%s://%s", cfg.Scheme, cfg.Address),
				"-consul-service-namespace", test.consulServiceNamespace,
				"-auth-method-namespace", test.authMethodNamespace,
			}
			// Add the CA File if necessary since we're not setting CONSUL_CACERT in test ENV.
			if test.tls {
				flags = append(flags, "-ca-file", caFile)
			}
			// Run the command.
			code := cmd.Run(flags)
			require.Equal(t, 0, code, ui.ErrorWriter.String())

			if test.authMethod != "" {
				// Validate the ACL token was written.
				tokenData, err := ioutil.ReadFile(tokenFile)
				require.NoError(t, err)
				require.NotEmpty(t, tokenData)

				// Check that the token has the metadata with pod name and pod namespace.
				consulClient, err = api.NewClient(&api.Config{Address: server.HTTPAddr, Token: string(tokenData), Namespace: test.consulServiceNamespace})
				require.NoError(t, err)
				token, _, err := consulClient.ACL().TokenReadSelf(&api.QueryOptions{Namespace: test.authMethodNamespace})
				require.NoError(t, err)
				require.Equal(t, "token created via login: {\"pod\":\"default-ns/counting-pod\"}", token.Description)
			}

			// Validate contents of proxyFile.
			data, err := ioutil.ReadFile(proxyFile)
			require.NoError(t, err)
			require.Contains(t, string(data), "counting-counting-sidecar-proxy")
		})
	}
}

// The namespace here is default-ns as the k8s-auth method
// relies on the namespace in the response from Kubernetes to
// correctly create the token in the same namespace as the Kubernetes
// namespace which is required when namespace mirroring is enabled.
// Note that this namespace is incorrect for other test cases but
// Consul only cares about this namespace when mirroring is enabled.
const (
	readServiceAccountFoundForNamespaces = `{
 "kind": "ServiceAccount",
 "apiVersion": "v1",
 "metadata": {
   "name": "counting",
   "namespace": "default-ns",
   "selfLink": "/api/v1/namespaces/default-ns/serviceaccounts/counting",
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

	tokenReviewFoundResponseForNamespaces = `{
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
	 "username": "system:serviceaccount:default-ns:counting",
	 "uid": "9ff51ff4-557e-11e9-9687-48e6c8b8ecb5",
	 "groups": [
	   "system:serviceaccounts",
	   "system:serviceaccounts:default-ns",
	   "system:authenticated"
	 ]
   }
 }
}`
)
