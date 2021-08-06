package createfederationsecret

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/freeport"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()
	f, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  nil,
			expErr: "-resource-prefix must be set",
		},
		{
			flags:  []string{"-resource-prefix=prefix"},
			expErr: "-k8s-namespace must be set",
		},
		{
			flags:  []string{"-resource-prefix=prefix", "-k8s-namespace=default"},
			expErr: "-server-ca-cert-file must be set",
		},
		{
			flags:  []string{"-resource-prefix=prefix", "-k8s-namespace=default", "-server-ca-cert-file=file"},
			expErr: "-server-ca-key-file must be set",
		},
		{
			flags:  []string{"-resource-prefix=prefix", "-k8s-namespace=default", "-server-ca-cert-file=file", "-server-ca-key-file=file"},
			expErr: "-mesh-gateway-service-name must be set",
		},
		{
			flags:  []string{"-resource-prefix=prefix", "-k8s-namespace=default", "-server-ca-cert-file=file", "-server-ca-key-file=file", "-mesh-gateway-service-name=mesh-gateway"},
			expErr: "-ca-file or CONSUL_CACERT must be set",
		},
		{
			flags: []string{
				"-resource-prefix=prefix",
				"-k8s-namespace=default",
				"-server-ca-cert-file=file",
				"-server-ca-key-file=file",
				"-ca-file", f.Name(),
				"-mesh-gateway-service-name=name",
				"-log-level=invalid",
			},
			expErr: "unknown log level: invalid",
		},
	}

	for _, c := range cases {
		t.Run(c.expErr, func(tt *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			exitCode := cmd.Run(c.flags)
			require.Equal(tt, 1, exitCode, ui.ErrorWriter.String())
			require.Contains(tt, ui.ErrorWriter.String(), c.expErr)
		})
	}
}

func TestRun_CAFileMissing(t *testing.T) {
	t.Parallel()
	f, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	exitCode := cmd.Run([]string{
		"-resource-prefix=prefix",
		"-k8s-namespace=default",
		"-mesh-gateway-service-name=name",
		"-server-ca-cert-file", f.Name(),
		"-server-ca-key-file", f.Name(),
		"-ca-file=/this/does/not/exist",
	})
	require.Equal(t, 1, exitCode, ui.ErrorWriter.String())
	require.Contains(t, ui.ErrorWriter.String(), "error reading CA file")
}

func TestRun_ServerCACertFileMissing(t *testing.T) {
	t.Parallel()
	f, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	exitCode := cmd.Run([]string{
		"-resource-prefix=prefix",
		"-k8s-namespace=default",
		"-mesh-gateway-service-name=name",
		"-ca-file", f.Name(),
		"-server-ca-cert-file=/this/does/not/exist",
		"-server-ca-key-file", f.Name(),
	})
	require.Equal(t, 1, exitCode, ui.ErrorWriter.String())
	require.Contains(t, ui.ErrorWriter.String(), "Error reading server CA cert file")
}

func TestRun_ServerCAKeyFileMissing(t *testing.T) {
	t.Parallel()
	f, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	exitCode := cmd.Run([]string{
		"-resource-prefix=prefix",
		"-k8s-namespace=default",
		"-mesh-gateway-service-name=name",
		"-ca-file", f.Name(),
		"-server-ca-cert-file", f.Name(),
		"-server-ca-key-file=/this/does/not/exist",
	})
	require.Equal(t, 1, exitCode, ui.ErrorWriter.String())
	require.Contains(t, ui.ErrorWriter.String(), "Error reading server CA key file")
}

func TestRun_GossipEncryptionKeyFileMissing(t *testing.T) {
	t.Parallel()
	f, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	exitCode := cmd.Run([]string{
		"-resource-prefix=prefix",
		"-k8s-namespace=default",
		"-mesh-gateway-service-name=name",
		"-ca-file", f.Name(),
		"-server-ca-cert-file", f.Name(),
		"-server-ca-key-file", f.Name(),
		"-gossip-key-file=/this/does/not/exist",
	})
	require.Equal(t, 1, exitCode, ui.ErrorWriter.String())
	require.Contains(t, ui.ErrorWriter.String(), "Error reading gossip encryption key file")
}

func TestRun_GossipEncryptionKeyFileEmpty(t *testing.T) {
	t.Parallel()
	f, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	exitCode := cmd.Run([]string{
		"-resource-prefix=prefix",
		"-k8s-namespace=default",
		"-mesh-gateway-service-name=name",
		"-ca-file", f.Name(),
		"-server-ca-cert-file", f.Name(),
		"-server-ca-key-file", f.Name(),
		"-gossip-key-file", f.Name(),
	})
	require.Equal(t, 1, exitCode, ui.ErrorWriter.String())
	require.Contains(t, ui.ErrorWriter.String(), fmt.Sprintf("gossip key file %q was empty", f.Name()))
}

// Test when the replication secret exists but it's missing the expected
// token key, we return error.
func TestRun_ReplicationTokenMissingExpectedKey(t *testing.T) {
	t.Parallel()
	f, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	defer os.Remove(f.Name())

	ui := cli.NewMockUi()
	k8s := fake.NewSimpleClientset()
	k8sNS := "default"
	k8s.CoreV1().Secrets(k8sNS).Create(
		context.Background(),
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: "prefix-" + common.ACLReplicationTokenName + "-acl-token",
			},
		},
		metav1.CreateOptions{})
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	exitCode := cmd.Run([]string{
		"-resource-prefix=prefix",
		"-k8s-namespace=default",
		"-mesh-gateway-service-name=name",
		"-ca-file", f.Name(),
		"-server-ca-cert-file", f.Name(),
		"-server-ca-key-file", f.Name(),
		"-export-replication-token",
	})
	require.Equal(t, 1, exitCode, ui.ErrorWriter.String())
}

// Our main test testing most permutations.
// Tests running with ACLs on/off, different kubernetes namespaces, with/without
// gossip key flag, different resource prefixes.
func TestRun_ACLs_K8SNamespaces_ResourcePrefixes(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		// aclsEnabled will enable ACLs and also set the -export-replication-token
		// flag because the helm chart won't allow this command to be run without
		// that flag when ACLs are enabled.
		aclsEnabled bool
		// k8sNS is the kubernetes namespace.
		k8sNS string
		// resourcePrefix is passed into -resource-prefix.
		resourcePrefix string
		// gossipKey controls whether we pass -gossip-key-file flag and expect
		// the output to contain the gossip key.
		gossipKey bool
	}{
		"acls disabled": {
			aclsEnabled:    false,
			k8sNS:          "default",
			resourcePrefix: "prefix",
			gossipKey:      false,
		},
		"acls disabled, gossip": {
			aclsEnabled:    false,
			k8sNS:          "default",
			resourcePrefix: "prefix",
			gossipKey:      true,
		},
		"acls enabled, gossip": {
			aclsEnabled:    true,
			k8sNS:          "default",
			resourcePrefix: "prefix",
			gossipKey:      true,
		},
		"acls disabled, k8sNS=other": {
			aclsEnabled:    false,
			k8sNS:          "other",
			resourcePrefix: "prefix",
			gossipKey:      false,
		},
		"acls enabled, k8sNS=other, gossip": {
			aclsEnabled:    true,
			k8sNS:          "other",
			resourcePrefix: "prefix1",
			gossipKey:      true,
		},
		// NOTE: Not testing gossip with different k8sNS because gossip key is
		// mounted in as a file.
		"acls disabled, resourcePrefix=other": {
			aclsEnabled:    false,
			k8sNS:          "default",
			resourcePrefix: "other",
			gossipKey:      false,
		},
		"acls enabled, resourcePrefix=other": {
			aclsEnabled:    true,
			k8sNS:          "default",
			resourcePrefix: "other",
			gossipKey:      false,
		},
	}
	for name, c := range cases {
		tt.Run(name, func(t *testing.T) {

			// Set up Consul server with TLS.
			caFile, certFile, keyFile := test.GenerateServerCerts(t)
			a, err := testutil.NewTestServerConfigT(t, func(cfg *testutil.TestServerConfig) {
				cfg.CAFile = caFile
				cfg.CertFile = certFile
				cfg.KeyFile = keyFile
				if c.aclsEnabled {
					cfg.ACL.Enabled = true
					cfg.ACL.DefaultPolicy = "deny"
				}
			})
			require.NoError(t, err)
			defer a.Stop()

			// Construct Consul client.
			client, err := api.NewClient(&api.Config{
				Address: a.HTTPSAddr,
				Scheme:  "https",
				TLSConfig: api.TLSConfig{
					CAFile: caFile,
				},
			})
			require.NoError(t, err)

			// Bootstrap ACLs if enabled.
			var replicationToken string
			if c.aclsEnabled {
				var bootstrapResp *api.ACLToken
				timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
				// May need to retry bootstrapping until server has elected
				// leader.
				retry.RunWith(timer, t, func(r *retry.R) {
					bootstrapResp, _, err = client.ACL().Bootstrap()
					require.NoError(r, err)
				})
				bootstrapToken := bootstrapResp.SecretID
				require.NotEmpty(t, bootstrapToken)

				// Redefine the client with the bootstrap token set so
				// subsequent calls will succeed.
				client, err = api.NewClient(&api.Config{
					Address: a.HTTPSAddr,
					Scheme:  "https",
					TLSConfig: api.TLSConfig{
						CAFile: caFile,
					},
					Token: bootstrapToken,
				})
				require.NoError(t, err)

				// Create a token for the replication policy.
				_, _, err = client.ACL().PolicyCreate(&api.ACLPolicy{
					Name:  "acl-replication-token",
					Rules: replicationPolicy,
				}, nil)
				require.NoError(t, err)

				resp, _, err := client.ACL().TokenCreate(&api.ACLToken{
					Policies: []*api.ACLTokenPolicyLink{
						{
							Name: "acl-replication-token",
						},
					},
				}, nil)
				require.NoError(t, err)
				replicationToken = resp.SecretID
			}

			// Create mesh gateway.
			meshGWIP := "192.168.0.1"
			meshGWPort := 443
			err = client.Agent().ServiceRegister(&api.AgentServiceRegistration{
				Name: "mesh-gateway",
				TaggedAddresses: map[string]api.ServiceAddress{
					"wan": {
						Address: meshGWIP,
						Port:    meshGWPort,
					},
				},
			})
			require.NoError(t, err)

			// Create fake k8s.
			k8s := fake.NewSimpleClientset()

			// Create replication token secret if expected.
			if c.aclsEnabled {
				_, err := k8s.CoreV1().Secrets(c.k8sNS).Create(
					context.Background(),
					&v1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name: c.resourcePrefix + "-acl-replication-acl-token",
						},
						Data: map[string][]byte{
							common.ACLTokenSecretKey: []byte(replicationToken),
						},
					},
					metav1.CreateOptions{})
				require.NoError(t, err)
			}

			// Create gossip encryption key if expected.
			gossipEncryptionKey := "oGaLv60gQ0E+Uvn+Lokz9APjbu5fJaYx7kglOmg4jZc="
			var gossipKeyFile string
			if c.gossipKey {
				f, err := ioutil.TempFile("", "")
				require.NoError(t, err)
				err = ioutil.WriteFile(f.Name(), []byte(gossipEncryptionKey), 0644)
				require.NoError(t, err)
				gossipKeyFile = f.Name()
			}

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				k8sClient: k8s,
			}
			flags := []string{
				"-resource-prefix", c.resourcePrefix,
				"-k8s-namespace", c.k8sNS,
				"-mesh-gateway-service-name=mesh-gateway",
				"-ca-file", caFile,
				"-server-ca-cert-file", caFile,
				"-server-ca-key-file", keyFile,
				"-http-addr", fmt.Sprintf("https://%s", a.HTTPSAddr),
			}
			if c.aclsEnabled {
				flags = append(flags, "-export-replication-token")
			}
			if c.gossipKey {
				flags = append(flags, "-gossip-key-file", gossipKeyFile)
			}
			exitCode := cmd.Run(flags)
			require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

			// Check the secret is as expected.
			secret, err := k8s.CoreV1().Secrets(c.k8sNS).Get(context.Background(), c.resourcePrefix+"-federation", metav1.GetOptions{})
			require.NoError(t, err)

			// CA Cert
			require.Contains(t, secret.Data, "caCert")
			caFileBytes, err := ioutil.ReadFile(caFile)
			require.NoError(t, err)
			require.Equal(t, string(caFileBytes), string(secret.Data["caCert"]))

			// CA Key
			require.Contains(t, secret.Data, "caKey")
			keyFileBytes, err := ioutil.ReadFile(keyFile)
			require.NoError(t, err)
			require.Equal(t, string(keyFileBytes), string(secret.Data["caKey"]))

			// Server Config
			require.Contains(t, secret.Data, "serverConfigJSON")
			expCfg := fmt.Sprintf(`{"primary_datacenter":"dc1","primary_gateways":["%s:%d"]}`, meshGWIP, meshGWPort)
			require.Equal(t, expCfg, string(secret.Data["serverConfigJSON"]))

			// Replication Token
			if c.aclsEnabled {
				require.Contains(t, secret.Data, "replicationToken")
				require.Equal(t, replicationToken, string(secret.Data["replicationToken"]))
			} else {
				require.NotContains(t, secret.Data, "replicationToken")
			}

			// Gossip encryption key.
			if c.gossipKey {
				require.Contains(t, secret.Data, "gossipEncryptionKey")
				require.Equal(t, gossipEncryptionKey, string(secret.Data["gossipEncryptionKey"]))
			} else {
				require.NotContains(t, secret.Data, "gossipEncryptionKey")
			}
		})
	}
}

// Test when mesh gateway instances are delayed.
func TestRun_WaitsForMeshGatewayInstances(t *testing.T) {
	// Create fake k8s.
	k8s := fake.NewSimpleClientset()

	// Set up Consul server with TLS.
	caFile, certFile, keyFile := test.GenerateServerCerts(t)
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
	})
	require.NoError(t, err)
	defer a.Stop()

	// Create a mesh gateway instance after a delay.
	meshGWIP := "192.168.0.1"
	meshGWPort := 443
	go func() {
		time.Sleep(500 * time.Millisecond)
		client, err := api.NewClient(&api.Config{
			Address: a.HTTPSAddr,
			Scheme:  "https",
			TLSConfig: api.TLSConfig{
				CAFile: caFile,
			},
		})
		require.NoError(t, err)
		err = client.Agent().ServiceRegister(&api.AgentServiceRegistration{
			Name: "mesh-gateway",
			TaggedAddresses: map[string]api.ServiceAddress{
				"wan": {
					Address: meshGWIP,
					Port:    meshGWPort,
				},
			},
		})
		require.NoError(t, err)
	}()

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	k8sNS := "default"
	resourcePrefix := "prefix"
	exitCode := cmd.Run([]string{
		"-resource-prefix", resourcePrefix,
		"-k8s-namespace", k8sNS,
		"-mesh-gateway-service-name=mesh-gateway",
		"-ca-file", caFile,
		"-server-ca-cert-file", certFile,
		"-server-ca-key-file", keyFile,
		"-http-addr", fmt.Sprintf("https://%s", a.HTTPSAddr),
	})
	require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

	// Check the secret is as expected.
	secret, err := k8s.CoreV1().Secrets(k8sNS).Get(context.Background(), resourcePrefix+"-federation", metav1.GetOptions{})
	require.NoError(t, err)

	// Test server config.
	require.Contains(t, secret.Data, "serverConfigJSON")
	expCfg := fmt.Sprintf(`{"primary_datacenter":"dc1","primary_gateways":["%s:%d"]}`, meshGWIP, meshGWPort)
	require.Equal(t, expCfg, string(secret.Data["serverConfigJSON"]))
}

// Test when the mesh gateways don't have a tagged address of name "wan".
func TestRun_MeshGatewayNoWANAddr(t *testing.T) {
	t.Parallel()

	// Set up Consul server with TLS.
	caFile, certFile, keyFile := test.GenerateServerCerts(t)
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
	})
	require.NoError(t, err)
	defer a.Stop()
	client, err := api.NewClient(&api.Config{
		Address: a.HTTPSAddr,
		Scheme:  "https",
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
	})
	require.NoError(t, err)
	err = client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		Name: "mesh-gateway",
	})
	require.NoError(t, err)

	ui := cli.NewMockUi()
	k8s := fake.NewSimpleClientset()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	exitCode := cmd.Run([]string{
		"-resource-prefix=prefix",
		"-k8s-namespace=default",
		"-mesh-gateway-service-name=mesh-gateway",
		"-ca-file", caFile,
		"-server-ca-cert-file", caFile,
		"-server-ca-key-file", keyFile,
		"-http-addr", fmt.Sprintf("https://%s", a.HTTPSAddr),
	})
	require.Equal(t, 1, exitCode, ui.ErrorWriter.String())
}

// Test that we only return unique addrs for the mesh gateways.
func TestRun_MeshGatewayUniqueAddrs(tt *testing.T) {
	tt.Parallel()

	cases := []struct {
		addrs    []string
		expAddrs []string
	}{
		{
			addrs:    []string{"127.0.0.1:443"},
			expAddrs: []string{"127.0.0.1:443"},
		},
		{
			addrs:    []string{"127.0.0.1:443", "127.0.0.1:443"},
			expAddrs: []string{"127.0.0.1:443"},
		},
		{
			addrs:    []string{"127.0.0.1:443", "127.0.0.2:443", "127.0.0.1:443"},
			expAddrs: []string{"127.0.0.1:443", "127.0.0.2:443"},
		},
		{
			addrs:    []string{"127.0.0.1:443", "127.0.0.1:543", "127.0.0.1:443"},
			expAddrs: []string{"127.0.0.1:443", "127.0.0.1:543"},
		},
	}
	for _, c := range cases {
		tt.Run(strings.Join(c.addrs, ","), func(t *testing.T) {
			// Create fake k8s.
			k8s := fake.NewSimpleClientset()

			// Set up Consul server with TLS.
			caFile, certFile, keyFile := test.GenerateServerCerts(t)
			a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
				c.CAFile = caFile
				c.CertFile = certFile
				c.KeyFile = keyFile
			})
			require.NoError(t, err)
			defer a.Stop()

			// Create mesh gateway instances.
			client, err := api.NewClient(&api.Config{
				Address: a.HTTPSAddr,
				Scheme:  "https",
				TLSConfig: api.TLSConfig{
					CAFile: caFile,
				},
			})
			require.NoError(t, err)
			for i, addr := range c.addrs {
				port, err := strconv.Atoi(strings.Split(addr, ":")[1])
				require.NoError(t, err)
				err = client.Agent().ServiceRegister(&api.AgentServiceRegistration{
					Name: "mesh-gateway",
					ID:   fmt.Sprintf("mesh-gateway-%d", i),
					TaggedAddresses: map[string]api.ServiceAddress{
						"wan": {
							Address: strings.Split(addr, ":")[0],
							Port:    port,
						},
					},
				})
				require.NoError(t, err)
			}
			require.NoError(t, err)

			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				k8sClient: k8s,
			}
			k8sNS := "default"
			resourcePrefix := "prefix"
			exitCode := cmd.Run([]string{
				"-resource-prefix", resourcePrefix,
				"-k8s-namespace", k8sNS,
				"-mesh-gateway-service-name=mesh-gateway",
				"-ca-file", caFile,
				"-server-ca-cert-file", caFile,
				"-server-ca-key-file", keyFile,
				"-http-addr", fmt.Sprintf("https://%s", a.HTTPSAddr),
			})
			require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

			// Check the secret is as expected.
			secret, err := k8s.CoreV1().Secrets(k8sNS).Get(context.Background(), resourcePrefix+"-federation", metav1.GetOptions{})
			require.NoError(t, err)

			// Server Config
			require.Contains(t, secret.Data, "serverConfigJSON")
			type ServerCfg struct {
				PrimaryGateways []string `json:"primary_gateways"`
			}
			var cfg ServerCfg
			err = json.Unmarshal(secret.Data["serverConfigJSON"], &cfg)
			require.NoError(t, err)
			require.ElementsMatch(t, cfg.PrimaryGateways, c.expAddrs)
		})
	}
}

// Test when the replication secret isn't created immediately. This mimics
// what happens in a regular installation because the replication secret doesn't
// get created until ACL bootstrapping is complete which can take a while since
// it requires the servers to all be up and a leader elected.
func TestRun_ReplicationSecretDelay(t *testing.T) {
	t.Parallel()

	// Set up Consul server with TLS.
	caFile, certFile, keyFile := test.GenerateServerCerts(t)
	a, err := testutil.NewTestServerConfigT(t, func(cfg *testutil.TestServerConfig) {
		cfg.CAFile = caFile
		cfg.CertFile = certFile
		cfg.KeyFile = keyFile
		cfg.ACL.Enabled = true
		cfg.ACL.DefaultPolicy = "deny"
	})
	require.NoError(t, err)
	defer a.Stop()

	// Construct Consul client.
	client, err := api.NewClient(&api.Config{
		Address: a.HTTPSAddr,
		Scheme:  "https",
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
	})
	require.NoError(t, err)

	// Bootstrap ACLs. We can do this before the command is started because
	// the command retrieves the replication token from Kubernetes secret, i.e.
	// that's the only thing that needs to be delayed.
	var bootstrapResp *api.ACLToken
	timer := &retry.Timer{Timeout: 10 * time.Second, Wait: 500 * time.Millisecond}
	// May need to retry bootstrapping until server has elected
	// leader.
	retry.RunWith(timer, t, func(r *retry.R) {
		bootstrapResp, _, err = client.ACL().Bootstrap()
		require.NoError(r, err)
	})
	bootstrapToken := bootstrapResp.SecretID
	require.NotEmpty(t, bootstrapToken)

	// Redefine the client with the bootstrap token set so
	// subsequent calls will succeed.
	client, err = api.NewClient(&api.Config{
		Address: a.HTTPSAddr,
		Scheme:  "https",
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
		Token: bootstrapToken,
	})
	require.NoError(t, err)

	// Create a token for the replication policy.
	_, _, err = client.ACL().PolicyCreate(&api.ACLPolicy{
		Name:  "acl-replication-policy",
		Rules: replicationPolicy,
	}, nil)
	require.NoError(t, err)

	resp, _, err := client.ACL().TokenCreate(&api.ACLToken{
		Policies: []*api.ACLTokenPolicyLink{
			{
				Name: "acl-replication-policy",
			},
		},
	}, nil)
	require.NoError(t, err)
	replicationToken := resp.SecretID

	// Create mesh gateway.
	meshGWIP := "192.168.0.1"
	meshGWPort := 443
	err = client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		Name: "mesh-gateway",
		TaggedAddresses: map[string]api.ServiceAddress{
			"wan": {
				Address: meshGWIP,
				Port:    meshGWPort,
			},
		},
	})
	require.NoError(t, err)

	// Create fake k8s.
	k8s := fake.NewSimpleClientset()

	// Create replication token secret after a delay.
	go func() {
		time.Sleep(400 * time.Millisecond)
		_, err := k8s.CoreV1().Secrets("default").Create(
			context.Background(),
			&v1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: "prefix-" + common.ACLReplicationTokenName + "-acl-token",
				},
				Data: map[string][]byte{
					common.ACLTokenSecretKey: []byte(replicationToken),
				},
			},
			metav1.CreateOptions{})
		require.NoError(t, err)
	}()

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	flags := []string{
		"-resource-prefix=prefix",
		"-k8s-namespace=default",
		"-mesh-gateway-service-name=mesh-gateway",
		"-ca-file", caFile,
		"-server-ca-cert-file", caFile,
		"-server-ca-key-file", keyFile,
		"-http-addr", fmt.Sprintf("https://%s", a.HTTPSAddr),
		"-export-replication-token",
	}
	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

	// Check the secret is as expected.
	secret, err := k8s.CoreV1().Secrets("default").Get(context.Background(), "prefix-federation", metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, secret.Data, "replicationToken")
	require.Equal(t, replicationToken, string(secret.Data["replicationToken"]))
}

// Test that re-running the command updates the secret. In this test, we'll
// update the addresses of the mesh gateways.
func TestRun_UpdatesSecret(t *testing.T) {
	t.Parallel()

	// Create fake k8s.
	k8s := fake.NewSimpleClientset()

	// Set up Consul server with TLS.
	caFile, certFile, keyFile := test.GenerateServerCerts(t)
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
	})
	require.NoError(t, err)
	defer a.Stop()

	// Create a mesh gateway instance.
	client, err := api.NewClient(&api.Config{
		Address: a.HTTPSAddr,
		Scheme:  "https",
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
	})
	require.NoError(t, err)
	meshGWIP := "192.168.0.1"
	meshGWPort := 443
	err = client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		Name: "mesh-gateway",
		TaggedAddresses: map[string]api.ServiceAddress{
			"wan": {
				Address: meshGWIP,
				Port:    meshGWPort,
			},
		},
	})
	require.NoError(t, err)

	k8sNS := "default"
	resourcePrefix := "prefix"

	// First run.
	{
		ui := cli.NewMockUi()
		cmd := Command{
			UI:        ui,
			k8sClient: k8s,
		}
		exitCode := cmd.Run([]string{
			"-resource-prefix", resourcePrefix,
			"-k8s-namespace", k8sNS,
			"-mesh-gateway-service-name=mesh-gateway",
			"-ca-file", caFile,
			"-server-ca-cert-file", certFile,
			"-server-ca-key-file", keyFile,
			"-http-addr", fmt.Sprintf("https://%s", a.HTTPSAddr),
		})
		require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

		// Check the secret is as expected.
		secret, err := k8s.CoreV1().Secrets(k8sNS).Get(context.Background(), resourcePrefix+"-federation", metav1.GetOptions{})
		require.NoError(t, err)

		// Test server config.
		require.Contains(t, secret.Data, "serverConfigJSON")
		expCfg := fmt.Sprintf(`{"primary_datacenter":"dc1","primary_gateways":["%s:%d"]}`, meshGWIP, meshGWPort)
		require.Equal(t, expCfg, string(secret.Data["serverConfigJSON"]))
	}

	// Now re-run the command.
	{
		// Update the mesh gateway IP.
		newMeshGWIP := "127.0.0.1"
		err = client.Agent().ServiceRegister(&api.AgentServiceRegistration{
			Name: "mesh-gateway",
			TaggedAddresses: map[string]api.ServiceAddress{
				"wan": {
					Address: newMeshGWIP,
					Port:    meshGWPort,
				},
			},
		})
		require.NoError(t, err)

		ui := cli.NewMockUi()
		cmd := Command{
			UI:        ui,
			k8sClient: k8s,
		}
		exitCode := cmd.Run([]string{
			"-resource-prefix", resourcePrefix,
			"-k8s-namespace", k8sNS,
			"-mesh-gateway-service-name=mesh-gateway",
			"-ca-file", caFile,
			"-server-ca-cert-file", caFile,
			"-server-ca-key-file", keyFile,
			"-http-addr", fmt.Sprintf("https://%s", a.HTTPSAddr),
		})
		require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

		// Check the secret is as expected.
		secret, err := k8s.CoreV1().Secrets(k8sNS).Get(context.Background(), resourcePrefix+"-federation", metav1.GetOptions{})
		require.NoError(t, err)

		// Test server config. The mesh gateway IP should be updated.
		require.Contains(t, secret.Data, "serverConfigJSON")
		expCfg := fmt.Sprintf(`{"primary_datacenter":"dc1","primary_gateways":["%s:%d"]}`, newMeshGWIP, meshGWPort)
		require.Equal(t, expCfg, string(secret.Data["serverConfigJSON"]))
	}
}

// Test that if the Consul client isn't up yet we will retry until it is.
func TestRun_ConsulClientDelay(t *testing.T) {
	t.Parallel()

	// We need to reserve all 6 ports to avoid potential
	// port collisions with other tests.
	randomPorts := freeport.MustTake(6)
	caFile, certFile, keyFile := test.GenerateServerCerts(t)

	// Create fake k8s.
	k8s := fake.NewSimpleClientset()

	// Set up Consul server with TLS. Start after a 500ms delay.
	var a *testutil.TestServer
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(500 * time.Millisecond)
		var err error
		a, err = testutil.NewTestServerConfigT(t, func(cfg *testutil.TestServerConfig) {
			cfg.CAFile = caFile
			cfg.CertFile = certFile
			cfg.KeyFile = keyFile
			cfg.Ports = &testutil.TestPortConfig{
				DNS:     randomPorts[0],
				HTTP:    randomPorts[1],
				HTTPS:   randomPorts[2],
				SerfLan: randomPorts[3],
				SerfWan: randomPorts[4],
				Server:  randomPorts[5],
			}
		})
		require.NoError(t, err)

		// Construct Consul client.
		client, err := api.NewClient(&api.Config{
			Address: a.HTTPSAddr,
			Scheme:  "https",
			TLSConfig: api.TLSConfig{
				CAFile: caFile,
			},
		})
		require.NoError(t, err)

		// Create mesh gateway.
		meshGWIP := "192.168.0.1"
		meshGWPort := 443
		err = client.Agent().ServiceRegister(&api.AgentServiceRegistration{
			Name: "mesh-gateway",
			TaggedAddresses: map[string]api.ServiceAddress{
				"wan": {
					Address: meshGWIP,
					Port:    meshGWPort,
				},
			},
		})
		require.NoError(t, err)
	}()
	defer func() {
		if a != nil {
			a.Stop()
		}
	}()

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	flags := []string{
		"-resource-prefix=prefix",
		"-k8s-namespace=default",
		"-mesh-gateway-service-name=mesh-gateway",
		"-ca-file", caFile,
		"-server-ca-cert-file", caFile,
		"-server-ca-key-file", keyFile,
		"-http-addr", fmt.Sprintf("https://127.0.0.1:%d", randomPorts[2]),
	}
	exitCode := cmd.Run(flags)
	require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

	// Check the secret is as expected.
	wg.Wait()
	_, err := k8s.CoreV1().Secrets("default").Get(context.Background(), "prefix-federation", metav1.GetOptions{})
	require.NoError(t, err)
}

// Test that we use the -ca-file for our consul client and not the -server-ca-cert-file.
// If autoencrypt is enabled, the server CA won't work.
func TestRun_Autoencrypt(t *testing.T) {
	t.Parallel()

	// Create fake k8s.
	k8s := fake.NewSimpleClientset()

	// Set up Consul server with TLS.
	caFile, certFile, keyFile := test.GenerateServerCerts(t)
	a, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
	})
	require.NoError(t, err)
	defer a.Stop()

	// Create a mesh gateway instance.
	client, err := api.NewClient(&api.Config{
		Address: a.HTTPSAddr,
		Scheme:  "https",
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
	})
	require.NoError(t, err)
	meshGWIP := "192.168.0.1"
	meshGWPort := 443
	err = client.Agent().ServiceRegister(&api.AgentServiceRegistration{
		Name: "mesh-gateway",
		TaggedAddresses: map[string]api.ServiceAddress{
			"wan": {
				Address: meshGWIP,
				Port:    meshGWPort,
			},
		},
	})
	require.NoError(t, err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	k8sNS := "default"
	resourcePrefix := "prefix"
	exitCode := cmd.Run([]string{
		"-resource-prefix", resourcePrefix,
		"-k8s-namespace", k8sNS,
		"-mesh-gateway-service-name=mesh-gateway",
		"-ca-file", caFile,
		// Here we're passing in the key file which would fail the test if this
		// was being used as the CA (since it's not a CA).
		"-server-ca-cert-file", keyFile,
		"-server-ca-key-file", keyFile,
		"-http-addr", fmt.Sprintf("https://%s", a.HTTPSAddr),
	})
	require.Equal(t, 0, exitCode, ui.ErrorWriter.String())

	// Check the value of the server CA cert is the key file.
	secret, err := k8s.CoreV1().Secrets(k8sNS).Get(context.Background(), resourcePrefix+"-federation", metav1.GetOptions{})
	require.NoError(t, err)

	require.Contains(t, secret.Data, "caCert")
	keyFileBytes, err := ioutil.ReadFile(keyFile)
	require.NoError(t, err)
	require.Equal(t, string(keyFileBytes), string(secret.Data["caCert"]))
}

var replicationPolicy = `acl = "write"
operator = "write"
agent_prefix "" {
  policy = "read"
}
node_prefix "" {
  policy = "write"
}
service_prefix "" {
  policy = "read"
  intentions = "read"
}
`
