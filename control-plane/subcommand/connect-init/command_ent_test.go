//go:build enterprise

package connectinit

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"os"
	"testing"

	connectinject "github.com/hashicorp/consul-k8s/control-plane/connect-inject"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

func TestRun_WithNamespaces(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name                   string
		consulServiceNamespace string
		acls                   bool
		authMethodNamespace    string
		adminPartition         string
	}{
		{
			name:                   "ACLs enabled; serviceNS=default, authMethodNS=default, partition=default",
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs enabled, serviceNS=default, authMethodNS=default, partition=default",
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs enabled, serviceNS=default-ns, authMethodNS=default, partition=default",
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs enabled, serviceNS=default-ns, authMethodNS=default, partition=default",
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs enabled, serviceNS=other, authMethodNS=other, partition=default",
			consulServiceNamespace: "other",
			authMethodNamespace:    "other",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs enabled, serviceNS=other, authMethodNS=other, partition=default",
			consulServiceNamespace: "other",
			authMethodNamespace:    "other",
			acls:                   true,
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, serviceNS=default, authMethodNS=default, partition=default",
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, serviceNS=default, authMethodNS=default, partition=default",
			consulServiceNamespace: "default",
			authMethodNamespace:    "default",
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, serviceNS=default-ns, authMethodNS=default, partition=default",
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, serviceNS=default-ns, authMethodNS=default, partition=default",
			consulServiceNamespace: "default-ns",
			authMethodNamespace:    "default",
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, serviceNS=other, authMethodNS=other, partition=default",
			consulServiceNamespace: "other",
			authMethodNamespace:    "other",
			adminPartition:         "default",
		},
		{
			name:                   "ACLs disabled, serviceNS=other, authMethodNS=other, partition=default",
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

			// Start Consul server with ACLs enabled and default deny policy.
			initialMgmtToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
			server, err := testutil.NewTestServerConfigT(t, func(cfg *testutil.TestServerConfig) {
				if c.acls {
					cfg.ACL.Enabled = true
					cfg.ACL.DefaultPolicy = "deny"
					cfg.ACL.Tokens.InitialManagement = initialMgmtToken
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
				cfg.Token = initialMgmtToken
			}

			consulClient, err := api.NewClient(cfg)
			require.NoError(t, err)

			_, err = namespaces.EnsureExists(consulClient, c.consulServiceNamespace, "")
			require.NoError(t, err)

			if c.acls {
				test.SetupK8sAuthMethodWithNamespaces(t, consulClient, testServiceAccountName, "default-ns", c.authMethodNamespace, c.authMethodNamespace != c.consulServiceNamespace, "")
			}

			// Register Consul services.
			testConsulServices := []api.AgentService{consulCountingSvc, consulCountingSvcSidecar}
			for _, svc := range testConsulServices {
				serviceRegistration := &api.CatalogRegistration{
					Node:    connectinject.ConsulNodeName,
					Address: "127.0.0.1",
					Service: &svc,
				}
				_, err = consulClient.Catalog().Register(serviceRegistration, nil)
				require.NoError(t, err)
			}

			ui := cli.NewMockUi()
			cmd := Command{
				UI:                                 ui,
				serviceRegistrationPollingAttempts: 5,
			}
			// We build the http-addr because normally it's defined by the init container setting
			// CONSUL_HTTP_ADDR when it processes the command template.
			flags := []string{"-pod-name", testPodName,
				"-pod-namespace", testPodNamespace,
				"-service-account-name", testServiceAccountName,
				"-http-addr", fmt.Sprintf("%s://%s", cfg.Scheme, cfg.Address),
				"-consul-service-namespace", c.consulServiceNamespace,
				"-acl-token-sink", tokenFile,
				"-bearer-token-file", bearerFile,
				"-proxy-id-file", proxyFile,
				"-consul-api-timeout", "5s",
				"-consul-node-name", connectinject.ConsulNodeName,
			}
			if c.acls {
				flags = append(flags, "-acl-auth-method", test.AuthMethod, "-auth-method-namespace", c.authMethodNamespace)
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
