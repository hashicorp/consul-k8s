// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package consullogout

import (
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
)

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		flags  []string
		expErr string
	}{
		{
			flags:  []string{},
			expErr: "-consul-api-timeout must be set to a value greater than 0",
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

// TestRun_InvalidSinkFile validates that we correctly fail in case the token sink file
// does not exist.
func TestRun_InvalidSinkFile(t *testing.T) {
	t.Parallel()
	randFileName := fmt.Sprintf("/foo/%d/%d", rand.Int(), rand.Int())

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}
	code := cmd.Run([]string{
		"-token-file", randFileName,
		"-consul-api-timeout", "10s",
	})
	require.Equal(t, 1, code)
}

// Test_UnableToLogoutDueToInvalidToken checks the error path for when Consul is not
// aware of an ACL token. This is a big corner case but covers the rare occurrance that
// the preStop hook where `consul-logout` is run might be executed more than once by Kubelet.
// This also covers obscure cases where the acl-token file is corrupted somehow.
func Test_UnableToLogoutDueToInvalidToken(t *testing.T) {
	tokenFile := fmt.Sprintf("/tmp/%d1", rand.Int())
	t.Cleanup(func() {
		os.RemoveAll(tokenFile)
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
	require.NoError(t, err)

	bogusToken := "00000000-00-0-001110aacddbderf"
	err = os.WriteFile(tokenFile, []byte(bogusToken), 0444)
	require.NoError(t, err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	// Run the command.
	code := cmd.Run([]string{
		"-http-addr", fmt.Sprintf("%s://%s", cfg.Scheme, cfg.Address),
		"-token-file", tokenFile,
		"-consul-api-timeout", "10s",
	})
	require.Equal(t, 1, code, ui.ErrorWriter.String())
	require.Contains(t, "Unexpected response code: 403 (ACL not found)", ui.ErrorWriter.String())
}

// Test_RunUsingLogin creates an AuthMethod and issues an ACL Token via ACL().Login()
// which is the code path that is taken to provision the ACL tokens at runtime through
// subcommand/acl-init. It then runs `consul-logout` and ensures that the ACL token
// is properly destroyed.
func Test_RunUsingLogin(t *testing.T) {
	// This is the test file that we will write the token to so consul-logout can read it.
	tokenFile := fmt.Sprintf("/tmp/%d1", rand.Int())
	t.Cleanup(func() {
		os.RemoveAll(tokenFile)
	})

	// Start Consul server with ACLs enabled and default deny policy.
	masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
	server, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.DefaultPolicy = "deny"
		c.ACL.Tokens.InitialManagement = masterToken
	})
	require.NoError(t, err)
	defer server.Stop()
	server.WaitForLeader(t)
	cfg := api.DefaultConfig()
	cfg.Address = server.HTTPAddr
	cfg.Scheme = "http"
	cfg.Token = masterToken
	consulClient, err := consul.NewClient(cfg, 0)
	require.NoError(t, err)

	// We are not setting up the Component Auth Method here because testing logout
	// does not need to use the auth method and this auth method can still issue a login.
	test.SetupK8sAuthMethod(t, consulClient, "test-sa", "default")

	// Do the login.
	req := &api.ACLLoginParams{
		AuthMethod:  test.AuthMethod,
		BearerToken: test.ServiceAccountJWTToken,
		Meta:        map[string]string{},
	}
	token, _, err := consulClient.ACL().Login(req, &api.WriteOptions{})
	require.NoError(t, err)

	// Validate that the token was created.
	tok, _, err := consulClient.ACL().TokenRead(token.AccessorID, &api.QueryOptions{})
	require.NoError(t, err)

	// Write the token's SecretID to the tokenFile which mimics loading
	// the ACL token from subcommand/acl-init path.
	err = os.WriteFile(tokenFile, []byte(token.SecretID), 0444)
	require.NoError(t, err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI: ui,
	}

	// Run the command.
	code := cmd.Run([]string{
		"-http-addr", fmt.Sprintf("%s://%s", cfg.Scheme, cfg.Address),
		"-token-file", tokenFile,
		"-consul-api-timeout", "10s",
	})
	require.Equal(t, 0, code, ui.ErrorWriter.String())

	// Validate the ACL token was destroyed.
	noTok, _, err := consulClient.ACL().TokenReadSelf(&api.QueryOptions{Token: tok.SecretID})
	require.Error(t, err)
	require.Nil(t, noTok)
}
