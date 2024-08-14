// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package aclinit

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
)

const (
	componentAuthMethod = "consul-k8s-component-auth-method"
)

// Test that we write the secret data to a file.
func TestRun_TokenSinkFile(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	tmpDir, err := os.MkdirTemp("", "")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	// Set up k8s with the secret.
	token := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	k8sNS := "default"
	secretName := "secret-name"
	k8s := fake.NewSimpleClientset()
	_, err = k8s.CoreV1().Secrets(k8sNS).Create(
		context.Background(),
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:   secretName,
				Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
			},
			Data: map[string][]byte{
				"token": []byte(token),
			},
		},
		metav1.CreateOptions{})

	require.NoError(err)

	sinkFile := filepath.Join(tmpDir, "acl-token")
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	code := cmd.Run([]string{
		"-token-sink-file", sinkFile,
		"-secret-name", secretName,
	})
	require.Equal(0, code, ui.ErrorWriter.String())
	bytes, err := os.ReadFile(sinkFile)
	require.NoError(err)
	require.Equal(token, string(bytes), "exp: %s, got: %s", token, string(bytes))
}

// Test that if there's an error writing the sink file it's returned.
func TestRun_TokenSinkFileErr(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	// Set up k8s with the secret.
	token := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	k8sNS := "default"
	secretName := "secret-name"
	k8s := fake.NewSimpleClientset()
	_, err := k8s.CoreV1().Secrets(k8sNS).Create(
		context.Background(),
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:   secretName,
				Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
			},
			Data: map[string][]byte{
				"token": []byte(token),
			},
		},
		metav1.CreateOptions{})

	require.NoError(err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	code := cmd.Run([]string{
		"-token-sink-file", "/this/filepath/does/not/exist",
		"-secret-name", secretName,
	})

	require.Equal(1, code)
}

// Test that if the command is run twice it succeeds. This test is the result
// of a bug that we discovered where the command failed on subsequent runs because
// the token file only had read permissions (0400).
func TestRun_TokenSinkFileTwice(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	tmpDir, err := os.MkdirTemp("", "")
	require.NoError(err)
	defer os.RemoveAll(tmpDir)

	// Set up k8s with the secret.
	token := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	k8sNS := "default"
	secretName := "secret-name"
	k8s := fake.NewSimpleClientset()
	_, err = k8s.CoreV1().Secrets(k8sNS).Create(
		context.Background(),
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:   secretName,
				Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
			},
			Data: map[string][]byte{
				"token": []byte(token),
			},
		},
		metav1.CreateOptions{})

	sinkFile := filepath.Join(tmpDir, "acl-token")
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}

	require.NoError(err)

	// Run twice.
	for i := 0; i < 2; i++ {
		code := cmd.Run([]string{
			"-token-sink-file", sinkFile,
			"-secret-name", secretName,
		})
		require.Equal(0, code, ui.ErrorWriter.String())

		bytes, err := os.ReadFile(sinkFile)
		require.NoError(err)
		require.Equal(token, string(bytes), "exp: %s, got: %s", token, string(bytes))
	}
}

// TestRun_PerformsConsulLogin executes the consul login path and validates the token
// is written to disk.
func TestRun_PerformsConsulLogin(t *testing.T) {
	// This is the test file that we will write the token to so consul-logout can read it.
	tokenFile := common.WriteTempFile(t, "")
	bearerFile := common.WriteTempFile(t, test.ServiceAccountJWTToken)

	k8s := fake.NewSimpleClientset()

	// Start Consul server with ACLs enabled and default deny policy.
	masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"
	server := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.DefaultPolicy = "deny"
		c.ACL.Tokens.InitialManagement = masterToken
	})
	// Set up the Component Auth Method, this pre-loads Consul with bindingrule, roles and an acl:write policy so we
	// can issue an ACL.Login().
	client := server.APIClient
	test.SetupK8sComponentAuthMethod(t, client, "test-sa", "default")

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}

	code := cmd.Run([]string{
		"-token-sink-file", tokenFile,
		"-consul-login-bearer-token-file", bearerFile,
		"-auth-method-name", componentAuthMethod,
		"-consul-login-meta", "component-name=foo",
		"-addresses", strings.Split(server.TestServer.HTTPAddr, ":")[0],
		"-http-port", strings.Split(server.TestServer.HTTPAddr, ":")[1],
		"-grpc-port", strings.Split(server.TestServer.GRPCAddr, ":")[1],
	})
	require.Equal(t, 0, code, ui.ErrorWriter.String())
	// Validate the Token got written.
	tokenBytes, err := os.ReadFile(tokenFile)
	require.NoError(t, err)
	require.Equal(t, 36, len(tokenBytes))
}

// TestRun_WithAclAuthMethodDefinedWritesConfigJsonWithTokenMatchingSinkFile
// executes the consul login path and validates the token is written to
// acl-config.json and matches the token written to sink file.
func TestRun_WithAclAuthMethodDefined_WritesConfigJson_WithTokenMatchingSinkFile(t *testing.T) {
	tokenFile := common.WriteTempFile(t, "")
	bearerFile := common.WriteTempFile(t, test.ServiceAccountJWTToken)
	tmpDir, err := os.MkdirTemp("", "")
	require.NoError(t, err)
	t.Cleanup(func() {
		os.RemoveAll(tokenFile)
		os.RemoveAll(tmpDir)
	})

	k8s := fake.NewSimpleClientset()

	// Start Consul server with ACLs enabled and default deny policy.
	masterToken := "b78d37c7-0ca7-5f4d-99ee-6d9975ce4586"

	server := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.DefaultPolicy = "deny"
		c.ACL.Tokens.InitialManagement = masterToken
	})
	consulClient := server.APIClient

	// Set up the Component Auth Method, this pre-loads Consul with bindingrule,
	// roles and an acl:write policy so we can issue an ACL.Login().
	test.SetupK8sComponentAuthMethod(t, consulClient, "test-sa", "default")

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}

	code := cmd.Run([]string{
		"-init-type", "client",
		"-acl-dir", tmpDir,
		"-token-sink-file", tokenFile,
		"-consul-login-bearer-token-file", bearerFile,
		"-auth-method-name", componentAuthMethod,
		"-consul-login-meta", "component-name=foo",
		"-addresses", strings.Split(server.TestServer.HTTPAddr, ":")[0],
		"-http-port", strings.Split(server.TestServer.HTTPAddr, ":")[1],
		"-grpc-port", strings.Split(server.TestServer.GRPCAddr, ":")[1],
	})
	require.Equal(t, 0, code, ui.ErrorWriter.String())
	// Validate the ACL Config file got written.
	aclConfigBytes, err := os.ReadFile(fmt.Sprintf("%s/acl-config.json", tmpDir))
	require.NoError(t, err)
	// Validate the Token Sink File got written.
	sinkFileToken, err := os.ReadFile(tokenFile)
	require.NoError(t, err)
	// Validate the Token Sink File Matches the ACL Cconfig Token by injecting
	// the token secret into the template used by the ACL config file.
	var buf bytes.Buffer
	tpl := template.Must(template.New("root").Parse(strings.TrimSpace(clientACLConfigTpl)))
	err = tpl.Execute(&buf, string(sinkFileToken))
	require.NoError(t, err)
	expectedAclConfig := buf.String()

	require.Equal(t, expectedAclConfig, string(aclConfigBytes))
}

// TestRun_WithAclAuthMethodDefinedWritesConfigJsonWithTokenMatchingSinkFile
// executes the k8s secret path and validates the token is written to
// acl-config.json and matches the token written to sink file.
func TestRun_WithoutAclAuthMethodDefined_WritesConfigJsonWithTokenMatchingSinkFile(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	tmpDir, err := os.MkdirTemp("", "")
	require.NoError(err)

	t.Cleanup(func() {
		os.RemoveAll(tmpDir)
	})

	// Set up k8s with the secret.
	token := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	k8sNS := "default"
	secretName := "secret-name"
	k8s := fake.NewSimpleClientset()
	_, err = k8s.CoreV1().Secrets(k8sNS).Create(
		context.Background(),
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:   secretName,
				Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
			},
			Data: map[string][]byte{
				"token": []byte(token),
			},
		},
		metav1.CreateOptions{})

	require.NoError(err)

	sinkFile := filepath.Join(tmpDir, "acl-token")
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		k8sClient: k8s,
	}
	code := cmd.Run([]string{
		"-token-sink-file", sinkFile,
		"-secret-name", secretName,
		"-init-type", "client",
		"-acl-dir", tmpDir,
	})
	// Validate the ACL Config file got written.
	aclConfigBytes, err := os.ReadFile(fmt.Sprintf("%s/acl-config.json", tmpDir))
	require.NoError(err)
	// Validate the Token Sink File got written.
	require.Equal(0, code, ui.ErrorWriter.String())
	sinkFileToken, err := os.ReadFile(sinkFile)
	require.NoError(err)
	// Validate the Token Sink File Matches the ACL Cconfig Token by injecting
	// the token secret into the template used by the ACL config file.
	var buf bytes.Buffer
	tpl := template.Must(template.New("root").Parse(strings.TrimSpace(clientACLConfigTpl)))
	err = tpl.Execute(&buf, string(sinkFileToken))
	require.NoError(err)
	expectedAclConfig := buf.String()

	require.Equal(expectedAclConfig, string(aclConfigBytes))
}
