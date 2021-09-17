// +build enterprise

package connectinit

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
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
		acls                   bool
		authMethodNamespace    string
		adminPartition         string
	}{
		{
			name:                   "ACLs enabled, no tls, serviceNS=default, authMethodNS=default, partition=default",
			tls:                    false,
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs enabled, tls, serviceNS=default, authMethodNS=default, partition=default",
			tls:                    true,
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs enabled, no tls, serviceNS=default-ns, authMethodNS=default, partition=default",
			tls:                    false,
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs enabled, tls, serviceNS=default-ns, authMethodNS=default, partition=default",
			tls:                    true,
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs enabled, no tls, serviceNS=other, authMethodNS=other, partition=default",
			tls:                    false,
			consulServiceNamespace: "other",
			authMethodNamespace:    "other",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs enabled, tls, serviceNS=other, authMethodNS=other, partition=default",
			tls:                    true,
			consulServiceNamespace: "other",
			authMethodNamespace:    "other",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, no tls, serviceNS=default, authMethodNS=default, partition=default",
			tls:                    false,
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, tls, serviceNS=default, authMethodNS=default, partition=default",
			tls:                    true,
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, no tls, serviceNS=default-ns, authMethodNS=default, partition=default",
			tls:                    false,
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, tls, serviceNS=default-ns, authMethodNS=default, partition=default",
			tls:                    true,
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, no tls, serviceNS=other, authMethodNS=other, partition=default",
			tls:                    false,
			consulServiceNamespace: "other",
			authMethodNamespace:    "other",
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, tls, serviceNS=other, authMethodNS=other, partition=default",
			tls:                    true,
			consulServiceNamespace: "other",
			authMethodNamespace:    "other",
			adminPartition:         "default",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			bearerFile := common.WriteTempFile(t, test.ServiceAccountJWTToken)
			tokenFile := fmt.Sprintf("/tmp/%d1", rand.Int())
			proxyFile := fmt.Sprintf("/tmp/%d2", rand.Int())
			t.Cleanup(func() {
				os.Remove(proxyFile)
				os.Remove(tokenFile)
			})

			var caFile, certFile, keyFile string
			// Start Consul server with ACLs enabled and default deny policy.
			masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
			server, err := testutil.NewTestServerConfigT(t, func(cfg *testutil.TestServerConfig) {
				if c.acls {
					cfg.ACL.Enabled = true
					cfg.ACL.DefaultPolicy = "deny"
					cfg.ACL.Tokens.Master = masterToken
				}
				if c.tls {
					caFile, certFile, keyFile = test.GenerateServerCerts(t)
					cfg.CAFile = caFile
					cfg.CertFile = certFile
					cfg.KeyFile = keyFile
				}
			})
			require.NoError(t, err)
			defer server.Stop()
			server.WaitForLeader(t)
			cfg := &api.Config{
				Scheme:    "http",
				Address:   server.HTTPAddr,
				Namespace: c.consulServiceNamespace,
				Partition: c.adminPartition,
			}
			if c.acls {
				cfg.Token = masterToken
			}
			if c.tls {
				cfg.Address = server.HTTPSAddr
				cfg.Scheme = "https"
				cfg.TLSConfig = api.TLSConfig{
					CAFile: caFile,
				}
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			_, err = namespaces.EnsureExists(consulClient, c.consulServiceNamespace, "")
			require.NoError(t, err)

			if c.acls {
				test.SetupK8sAuthMethodWithNamespaces(t, consulClient, testServiceAccountName, "default-ns", c.authMethodNamespace, c.authMethodNamespace != c.consulServiceNamespace, "")
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
				"-service-account-name", testServiceAccountName,
				"-http-addr", fmt.Sprintf("%s://%s", cfg.Scheme, cfg.Address),
				"-consul-service-namespace", c.consulServiceNamespace,
			}
			if c.acls {
				flags = append(flags, "-acl-auth-method", test.AuthMethod, "-auth-method-namespace", c.authMethodNamespace)
			}
			// Add the CA File if necessary since we're not setting CONSUL_CACERT in test ENV.
			if c.tls {
				flags = append(flags, "-ca-file", caFile)
			}
			// Run the command.
			code := cmd.Run(flags)
			require.Equal(t, 0, code, ui.ErrorWriter.String())

			if c.acls {
				// Validate the ACL token was written.
				tokenData, err := ioutil.ReadFile(tokenFile)
				require.NoError(t, err)
				require.NotEmpty(t, tokenData)

				// Check that the token has the metadata with pod name and pod namespace.
				consulClient, err = api.NewClient(&api.Config{Address: server.HTTPAddr, Token: string(tokenData), Namespace: c.consulServiceNamespace})
				require.NoError(t, err)
				token, _, err := consulClient.ACL().TokenReadSelf(&api.QueryOptions{Namespace: c.authMethodNamespace})
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
