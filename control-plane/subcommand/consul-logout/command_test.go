package consullogout

import (
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
)

// TestRun_InvalidSinkFile validates that we correctly fail in case the token sink file
// does not exist.
func TestRun_InvalidSinkFile(t *testing.T) {
	t.Parallel()
	randFileName := fmt.Sprintf("/foo/%d/%d", rand.Int(), rand.Int())

	ui := cli.NewMockUi()
	cmd := Command{
		UI:            ui,
		tokenSinkFile: randFileName,
	}
	code := cmd.Run([]string{})
	require.Equal(t, 1, code)
}

// Test_UnableToLogoutDueToInvalidToken checks the error path for when Consul is not
// aware of an ACL token. This is a big corner case but covers the rare occurrance that
// the preStop hook where `consul-logout` is run might be executed more than once by Kubelet.
// This also covers obscure cases where the acl-token file is corrupted somehow.
func Test_UnableToLogoutDueToInvalidToken(t *testing.T) {
	tokenFile := fmt.Sprintf("/tmp/%d1", rand.Int())
	t.Cleanup(func() {
		os.Remove(tokenFile)
	})

	var caFile, certFile, keyFile string
	// Start Consul server with ACLs enabled and default deny policy.
	masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
	server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.DefaultPolicy = "deny"
		c.ACL.Tokens.InitialManagement = masterToken
		caFile, certFile, keyFile = test.GenerateServerCerts(t)
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
	})
	require.NoError(t, err)
	defer server.Stop()
	server.WaitForLeader(t)
	cfg := &api.Config{
		Address: server.HTTPSAddr,
		Scheme:  "https",
		Token:   masterToken,
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
	}
	consulClient, err := api.NewClient(cfg)
	require.NoError(t, err)

	test.SetupK8sAuthMethod(t, consulClient, "test-sa", "default", common.ComponentAuthMethod)

	bogusToken := "00000000-00-0-001110aacddbderf"
	err = os.WriteFile(tokenFile, []byte(bogusToken), 0444)
	require.NoError(t, err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:            ui,
		tokenSinkFile: tokenFile,
		consulClient:  consulClient,
	}

	// Run the command.
	code := cmd.Run([]string{})
	require.Equal(t, 1, code, ui.ErrorWriter.String())
	require.Contains(t, "Unexpected response code: 403 (ACL not found)", ui.ErrorWriter.String())
}

// Test_RunUsingLogin creates an AuthMethod and issues an ACL Token via ACL().Login()
// which is the code path that is taken to provision the ACL tokens at runtime through
// subcommand/acl-init. It then runs `consul-logout` and ensures that the ACL token
// is properly destroyed.
func Test_RunUsingLogin(t *testing.T) {
	var caFile, certFile, keyFile string
	// This is the test file that we will write the token to so consul-logout can read it.
	tokenFile := fmt.Sprintf("/tmp/%d1", rand.Int())
	t.Cleanup(func() {
		os.Remove(tokenFile)
	})

	// Start Consul server with ACLs enabled and default deny policy.
	masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
	server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.DefaultPolicy = "deny"
		c.ACL.Tokens.InitialManagement = masterToken
		caFile, certFile, keyFile = test.GenerateServerCerts(t)
		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
	})
	require.NoError(t, err)
	defer server.Stop()
	server.WaitForLeader(t)
	cfg := &api.Config{
		Address: server.HTTPSAddr,
		Scheme:  "https",
		Token:   masterToken,
		TLSConfig: api.TLSConfig{
			CAFile: caFile,
		},
	}
	consulClient, err := consul.NewClient(cfg)
	require.NoError(t, err)

	test.SetupK8sAuthMethod(t, consulClient, "test-sa", "default", common.ComponentAuthMethod)

	// Do the login.
	req := &api.ACLLoginParams{
		AuthMethod:  common.ComponentAuthMethod,
		BearerToken: test.ServiceAccountJWTToken,
		Meta:        map[string]string{},
	}
	token, _, err := consulClient.ACL().Login(req, &api.WriteOptions{})
	require.NoError(t, err)

	// Validate that the token was created.
	_, _, err = consulClient.ACL().TokenRead(token.AccessorID, &api.QueryOptions{})
	require.NoError(t, err)

	// Write the token's SecretID to the tokenFile which mimics loading
	// the ACL token from subcommand/acl-init path.
	err = os.WriteFile(tokenFile, []byte(token.SecretID), 0444)
	require.NoError(t, err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:            ui,
		tokenSinkFile: tokenFile,
		consulClient:  consulClient,
	}

	// Run the command.
	code := cmd.Run([]string{})
	require.Equal(t, 0, code, ui.ErrorWriter.String())

	// Validate the ACL token was destroyed.
	tokenList, _, err := consulClient.ACL().TokenList(nil)
	require.NoError(t, err)
	for _, tok := range tokenList {
		require.NotEqual(t, tok.SecretID, token.SecretID)
	}
}
