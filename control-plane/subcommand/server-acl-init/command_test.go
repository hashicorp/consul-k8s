package serveraclinit

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/freeport"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/go-discover"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/hashicorp/consul-k8s/control-plane/helper/cert"
	"github.com/hashicorp/consul-k8s/control-plane/helper/go-discover/mocks"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
)

var ns = "default"
var resourcePrefix = "release-name-consul"

func TestRun_FlagValidation(t *testing.T) {
	t.Parallel()

	cases := []struct {
		Flags  []string
		ExpErr string
	}{
		{
			Flags:  []string{},
			ExpErr: "-server-address must be set at least once",
		},
		{
			Flags:  []string{"-server-address=localhost"},
			ExpErr: "-resource-prefix must be set",
		},
		{
			Flags:  []string{"-acl-replication-token-file=/notexist", "-server-address=localhost", "-resource-prefix=prefix"},
			ExpErr: "Unable to read ACL replication token from file \"/notexist\": open /notexist: no such file or directory",
		},
		{
			Flags:  []string{"-bootstrap-token-file=/notexist", "-server-address=localhost", "-resource-prefix=prefix"},
			ExpErr: "Unable to read bootstrap token from file \"/notexist\": open /notexist: no such file or directory",
		},
		{
			Flags: []string{
				"-server-address=localhost",
				"-resource-prefix=prefix",
				"-sync-consul-node-name=Speci@l_Chars",
			},
			ExpErr: "-sync-consul-node-name=Speci@l_Chars is invalid: node name will not be discoverable " +
				"via DNS due to invalid characters. Valid characters include all alpha-numerics and dashes",
		},
		{
			Flags: []string{
				"-server-address=localhost",
				"-resource-prefix=prefix",
				"-sync-consul-node-name=5r9OPGfSRXUdGzNjBdAwmhCBrzHDNYs4XjZVR4wp7lSLIzqwS0ta51nBLIN0TMPV-too-long",
			},
			ExpErr: "-sync-consul-node-name=5r9OPGfSRXUdGzNjBdAwmhCBrzHDNYs4XjZVR4wp7lSLIzqwS0ta51nBLIN0TMPV-too-long is invalid: node name will not be discoverable " +
				"via DNS due to it being too long. Valid lengths are between 1 and 63 bytes",
		},
	}

	for _, c := range cases {
		t.Run(c.ExpErr, func(t *testing.T) {
			ui := cli.NewMockUi()
			cmd := Command{
				UI: ui,
			}
			responseCode := cmd.Run(c.Flags)
			require.Equal(t, 1, responseCode, ui.ErrorWriter.String())
			require.Contains(t, ui.ErrorWriter.String(), c.ExpErr)
		})
	}
}

// Test what happens if no extra flags were set (i.e. the defaults apply).
// We test with both the deprecated -release-name and the new -server-label-selector
// flags.
func TestRun_Defaults(t *testing.T) {
	t.Parallel()

	k8s, testSvr := completeSetup(t)
	defer testSvr.Stop()
	require := require.New(t)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	args := []string{
		"-timeout=1m",
		"-k8s-namespace=" + ns,
		"-server-address", strings.Split(testSvr.HTTPAddr, ":")[0],
		"-server-port", strings.Split(testSvr.HTTPAddr, ":")[1],
		"-resource-prefix=" + resourcePrefix,
	}
	responseCode := cmd.Run(args)
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the bootstrap kube secret is created.
	bootToken := getBootToken(t, k8s, resourcePrefix, ns)

	// Check that it has the right policies.
	consul, err := api.NewClient(&api.Config{
		Address: testSvr.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(err)
	tokenData, _, err := consul.ACL().TokenReadSelf(nil)
	require.NoError(err)
	require.Equal("global-management", tokenData.Policies[0].Name)

	// Check that the agent policy was created.
	agentPolicy := policyExists(t, "agent-token", consul)
	// Should be a global policy.
	require.Len(agentPolicy.Datacenters, 0)

	// We should also test that the server's token was updated, however I
	// couldn't find a way to test that with the test agent. Instead we test
	// that in another test when we're using an httptest server instead of
	// the test agent and we can assert that the /v1/agent/token/agent
	// endpoint was called.
}

// Test the different flags that should create tokens and save them as
// Kubernetes secrets.
func TestRun_TokensPrimaryDC(t *testing.T) {
	t.Parallel()

	cases := []struct {
		TestName    string
		TokenFlags  []string
		PolicyNames []string
		PolicyDCs   []string
		SecretNames []string
		LocalToken  bool
	}{
		{
			TestName:    "Client token",
			TokenFlags:  []string{"-create-client-token"},
			PolicyNames: []string{"client-token"},
			PolicyDCs:   []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-client-acl-token"},
			LocalToken:  true,
		},
		{
			TestName:    "Sync token",
			TokenFlags:  []string{"-create-sync-token"},
			PolicyNames: []string{"catalog-sync-token"},
			PolicyDCs:   []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-catalog-sync-acl-token"},
			LocalToken:  true,
		},
		{
			TestName:    "Enterprise license token",
			TokenFlags:  []string{"-create-enterprise-license-token"},
			PolicyNames: []string{"enterprise-license-token"},
			PolicyDCs:   []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-enterprise-license-acl-token"},
			LocalToken:  true,
		},
		{
			TestName:    "Snapshot agent token",
			TokenFlags:  []string{"-create-snapshot-agent-token"},
			PolicyNames: []string{"client-snapshot-agent-token"},
			PolicyDCs:   []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-client-snapshot-agent-acl-token"},
			LocalToken:  true,
		},
		{
			TestName:    "Mesh gateway token",
			TokenFlags:  []string{"-create-mesh-gateway-token"},
			PolicyNames: []string{"mesh-gateway-token"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-mesh-gateway-acl-token"},
			LocalToken:  false,
		},
		{
			TestName: "Ingress gateway tokens",
			TokenFlags: []string{"-ingress-gateway-name=ingress",
				"-ingress-gateway-name=gateway",
				"-ingress-gateway-name=another-gateway"},
			PolicyNames: []string{"ingress-ingress-gateway-token",
				"gateway-ingress-gateway-token",
				"another-gateway-ingress-gateway-token"},
			PolicyDCs: []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-ingress-ingress-gateway-acl-token",
				resourcePrefix + "-gateway-ingress-gateway-acl-token",
				resourcePrefix + "-another-gateway-ingress-gateway-acl-token"},
			LocalToken: true,
		},
		{
			TestName: "Terminating gateway tokens",
			TokenFlags: []string{"-terminating-gateway-name=terminating",
				"-terminating-gateway-name=gateway",
				"-terminating-gateway-name=another-gateway"},
			PolicyNames: []string{"terminating-terminating-gateway-token",
				"gateway-terminating-gateway-token",
				"another-gateway-terminating-gateway-token"},
			PolicyDCs: []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-terminating-terminating-gateway-acl-token",
				resourcePrefix + "-gateway-terminating-gateway-acl-token",
				resourcePrefix + "-another-gateway-terminating-gateway-acl-token"},
			LocalToken: true,
		},
		{
			TestName:    "ACL replication token",
			TokenFlags:  []string{"-create-acl-replication-token"},
			PolicyNames: []string{"acl-replication-token"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-acl-replication-acl-token"},
			LocalToken:  false,
		},
		{
			TestName:    "Controller token",
			TokenFlags:  []string{"-create-controller-token"},
			PolicyNames: []string{"controller-token"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-controller-acl-token"},
			LocalToken:  false,
		},
		{
			TestName:    "Endpoints Controller ACL token",
			TokenFlags:  []string{"-create-inject-token"},
			PolicyNames: []string{"connect-inject-token"},
			PolicyDCs:   []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-connect-inject-acl-token"},
			LocalToken:  true,
		},
	}
	for _, c := range cases {
		t.Run(c.TestName, func(t *testing.T) {
			k8s, testSvr := completeSetup(t)
			setUpK8sServiceAccount(t, k8s, ns)
			defer testSvr.Stop()
			require := require.New(t)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			cmdArgs := append([]string{
				"-timeout=1m",
				"-k8s-namespace=" + ns,
				"-server-address", strings.Split(testSvr.HTTPAddr, ":")[0],
				"-server-port", strings.Split(testSvr.HTTPAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
			}, c.TokenFlags...)

			responseCode := cmd.Run(cmdArgs)
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Check that the expected policy was created.
			bootToken := getBootToken(t, k8s, resourcePrefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testSvr.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(err)

			for i := range c.PolicyNames {
				policy := policyExists(t, c.PolicyNames[i], consul)
				require.Equal(c.PolicyDCs, policy.Datacenters)

				// Test that the token was created as a Kubernetes Secret.
				tokenSecret, err := k8s.CoreV1().Secrets(ns).Get(context.Background(), c.SecretNames[i], metav1.GetOptions{})
				require.NoError(err)
				require.NotNil(tokenSecret)
				token, ok := tokenSecret.Data["token"]
				require.True(ok)

				// Test that the token has the expected policies in Consul.
				tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: string(token)})
				require.NoError(err)
				require.Equal(c.PolicyNames[i], tokenData.Policies[0].Name)
				require.Equal(c.LocalToken, tokenData.Local)
			}

			// Test that if the same command is run again, it doesn't error.
			t.Run(c.TestName+"-retried", func(t *testing.T) {
				ui := cli.NewMockUi()
				cmd := Command{
					UI:        ui,
					clientset: k8s,
				}
				cmd.init()
				responseCode := cmd.Run(cmdArgs)
				require.Equal(0, responseCode, ui.ErrorWriter.String())
			})
		})
	}
}

// Test creating each token type when replication is enabled.
func TestRun_TokensReplicatedDC(t *testing.T) {
	t.Parallel()

	cases := []struct {
		TestName    string
		TokenFlags  []string
		PolicyNames []string
		PolicyDCs   []string
		SecretNames []string
		LocalToken  bool
	}{
		{
			TestName:    "Client token",
			TokenFlags:  []string{"-create-client-token"},
			PolicyNames: []string{"client-token-dc2"},
			PolicyDCs:   []string{"dc2"},
			SecretNames: []string{resourcePrefix + "-client-acl-token"},
			LocalToken:  true,
		},
		{
			TestName:    "Sync token",
			TokenFlags:  []string{"-create-sync-token"},
			PolicyNames: []string{"catalog-sync-token-dc2"},
			PolicyDCs:   []string{"dc2"},
			SecretNames: []string{resourcePrefix + "-catalog-sync-acl-token"},
			LocalToken:  true,
		},
		{
			TestName:    "Enterprise license token",
			TokenFlags:  []string{"-create-enterprise-license-token"},
			PolicyNames: []string{"enterprise-license-token-dc2"},
			PolicyDCs:   []string{"dc2"},
			SecretNames: []string{resourcePrefix + "-enterprise-license-acl-token"},
			LocalToken:  true,
		},
		{
			TestName:    "Snapshot agent token",
			TokenFlags:  []string{"-create-snapshot-agent-token"},
			PolicyNames: []string{"client-snapshot-agent-token-dc2"},
			PolicyDCs:   []string{"dc2"},
			SecretNames: []string{resourcePrefix + "-client-snapshot-agent-acl-token"},
			LocalToken:  true,
		},
		{
			TestName:    "Mesh gateway token",
			TokenFlags:  []string{"-create-mesh-gateway-token"},
			PolicyNames: []string{"mesh-gateway-token-dc2"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-mesh-gateway-acl-token"},
			LocalToken:  false,
		},
		{
			TestName: "Ingress gateway tokens",
			TokenFlags: []string{"-ingress-gateway-name=ingress",
				"-ingress-gateway-name=gateway",
				"-ingress-gateway-name=another-gateway"},
			PolicyNames: []string{"ingress-ingress-gateway-token-dc2",
				"gateway-ingress-gateway-token-dc2",
				"another-gateway-ingress-gateway-token-dc2"},
			PolicyDCs: []string{"dc2"},
			SecretNames: []string{resourcePrefix + "-ingress-ingress-gateway-acl-token",
				resourcePrefix + "-gateway-ingress-gateway-acl-token",
				resourcePrefix + "-another-gateway-ingress-gateway-acl-token"},
			LocalToken: true,
		},
		{
			TestName: "Terminating gateway tokens",
			TokenFlags: []string{"-terminating-gateway-name=terminating",
				"-terminating-gateway-name=gateway",
				"-terminating-gateway-name=another-gateway"},
			PolicyNames: []string{"terminating-terminating-gateway-token-dc2",
				"gateway-terminating-gateway-token-dc2",
				"another-gateway-terminating-gateway-token-dc2"},
			PolicyDCs: []string{"dc2"},
			SecretNames: []string{resourcePrefix + "-terminating-terminating-gateway-acl-token",
				resourcePrefix + "-gateway-terminating-gateway-acl-token",
				resourcePrefix + "-another-gateway-terminating-gateway-acl-token"},
			LocalToken: true,
		},
		{
			TestName:    "Endpoints controller ACL token",
			TokenFlags:  []string{"-create-inject-token"},
			PolicyNames: []string{"connect-inject-token-dc2"},
			PolicyDCs:   []string{"dc2"},
			SecretNames: []string{resourcePrefix + "-connect-inject-acl-token"},
			LocalToken:  true,
		},
		{
			TestName:    "Controller token",
			TokenFlags:  []string{"-create-controller-token"},
			PolicyNames: []string{"controller-token-dc2"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-controller-acl-token"},
			LocalToken:  false,
		},
	}
	for _, c := range cases {
		t.Run(c.TestName, func(t *testing.T) {
			bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
			tokenFile := common.WriteTempFile(t, bootToken)

			k8s, consul, secondaryAddr, cleanup := mockReplicatedSetup(t, bootToken)
			setUpK8sServiceAccount(t, k8s, ns)
			defer cleanup()

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			cmdArgs := append([]string{
				"-federation",
				"-timeout=1m",
				"-k8s-namespace=" + ns,
				"-acl-replication-token-file", tokenFile,
				"-server-address", strings.Split(secondaryAddr, ":")[0],
				"-server-port", strings.Split(secondaryAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
			}, c.TokenFlags...)

			responseCode := cmd.Run(cmdArgs)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			// Check that the expected policy was created.
			retry.Run(t, func(r *retry.R) {
				for i := range c.PolicyNames {
					policy := policyExists(r, c.PolicyNames[i], consul)
					require.Equal(r, c.PolicyDCs, policy.Datacenters)

					// Test that the token was created as a Kubernetes Secret.
					tokenSecret, err := k8s.CoreV1().Secrets(ns).Get(context.Background(), c.SecretNames[i], metav1.GetOptions{})
					require.NoError(r, err)
					require.NotNil(r, tokenSecret)
					token, ok := tokenSecret.Data["token"]
					require.True(r, ok)

					// Test that the token has the expected policies in Consul.
					tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: string(token)})
					require.NoError(r, err)
					require.Equal(r, c.PolicyNames[i], tokenData.Policies[0].Name)
					require.Equal(r, c.LocalToken, tokenData.Local)
				}
			})
		})
	}
}

// Test creating each token type when the bootstrap token is provided.
func TestRun_TokensWithProvidedBootstrapToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		TestName    string
		TokenFlags  []string
		PolicyNames []string
		SecretNames []string
	}{
		{
			TestName:    "Client token",
			TokenFlags:  []string{"-create-client-token"},
			PolicyNames: []string{"client-token"},
			SecretNames: []string{resourcePrefix + "-client-acl-token"},
		},
		{
			TestName:    "Endpoints controller ACL token",
			TokenFlags:  []string{"-create-inject-token"},
			PolicyNames: []string{"connect-inject-token"},
			SecretNames: []string{resourcePrefix + "-connect-inject-acl-token"},
		},
		{
			TestName:    "Sync token",
			TokenFlags:  []string{"-create-sync-token"},
			PolicyNames: []string{"catalog-sync-token"},
			SecretNames: []string{resourcePrefix + "-catalog-sync-acl-token"},
		},
		{
			TestName:    "Enterprise license token",
			TokenFlags:  []string{"-create-enterprise-license-token"},
			PolicyNames: []string{"enterprise-license-token"},
			SecretNames: []string{resourcePrefix + "-enterprise-license-acl-token"},
		},
		{
			TestName:    "Snapshot agent token",
			TokenFlags:  []string{"-create-snapshot-agent-token"},
			PolicyNames: []string{"client-snapshot-agent-token"},
			SecretNames: []string{resourcePrefix + "-client-snapshot-agent-acl-token"},
		},
		{
			TestName:    "Mesh gateway token",
			TokenFlags:  []string{"-create-mesh-gateway-token"},
			PolicyNames: []string{"mesh-gateway-token"},
			SecretNames: []string{resourcePrefix + "-mesh-gateway-acl-token"},
		},
		{
			TestName: "Ingress gateway tokens",
			TokenFlags: []string{"-ingress-gateway-name=ingress",
				"-ingress-gateway-name=gateway",
				"-ingress-gateway-name=another-gateway"},
			PolicyNames: []string{"ingress-ingress-gateway-token",
				"gateway-ingress-gateway-token",
				"another-gateway-ingress-gateway-token"},
			SecretNames: []string{resourcePrefix + "-ingress-ingress-gateway-acl-token",
				resourcePrefix + "-gateway-ingress-gateway-acl-token",
				resourcePrefix + "-another-gateway-ingress-gateway-acl-token"},
		},
		{
			TestName: "Terminating gateway tokens",
			TokenFlags: []string{"-terminating-gateway-name=terminating",
				"-terminating-gateway-name=gateway",
				"-terminating-gateway-name=another-gateway"},
			PolicyNames: []string{"terminating-terminating-gateway-token",
				"gateway-terminating-gateway-token",
				"another-gateway-terminating-gateway-token"},
			SecretNames: []string{resourcePrefix + "-terminating-terminating-gateway-acl-token",
				resourcePrefix + "-gateway-terminating-gateway-acl-token",
				resourcePrefix + "-another-gateway-terminating-gateway-acl-token"},
		},
		{
			TestName:    "ACL replication token",
			TokenFlags:  []string{"-create-acl-replication-token"},
			PolicyNames: []string{"acl-replication-token"},
			SecretNames: []string{resourcePrefix + "-acl-replication-acl-token"},
		},
		{
			TestName:    "Controller token",
			TokenFlags:  []string{"-create-controller-token"},
			PolicyNames: []string{"controller-token"},
			SecretNames: []string{resourcePrefix + "-controller-acl-token"},
		},
	}
	for _, c := range cases {
		t.Run(c.TestName, func(t *testing.T) {
			bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
			tokenFile := common.WriteTempFile(t, bootToken)

			k8s, testAgent := completeBootstrappedSetup(t, bootToken)
			setUpK8sServiceAccount(t, k8s, ns)
			defer testAgent.Stop()

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmdArgs := append([]string{
				"-timeout=1m",
				"-k8s-namespace", ns,
				"-bootstrap-token-file", tokenFile,
				"-server-address", strings.Split(testAgent.HTTPAddr, ":")[0],
				"-server-port", strings.Split(testAgent.HTTPAddr, ":")[1],
				"-resource-prefix", resourcePrefix,
			}, c.TokenFlags...)

			responseCode := cmd.Run(cmdArgs)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			consul, err := api.NewClient(&api.Config{
				Address: testAgent.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(t, err)

			// Check that the expected policy was created.
			retry.Run(t, func(r *retry.R) {
				for i := range c.PolicyNames {
					policyExists(r, c.PolicyNames[i], consul)

					// Test that the token was created as a Kubernetes Secret.
					tokenSecret, err := k8s.CoreV1().Secrets(ns).Get(context.Background(), c.SecretNames[i], metav1.GetOptions{})
					require.NoError(r, err)
					require.NotNil(r, tokenSecret)
					token, ok := tokenSecret.Data["token"]
					require.True(r, ok)

					// Test that the token has the expected policies in Consul.
					tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: string(token)})
					require.NoError(r, err)
					require.Equal(r, c.PolicyNames[i], tokenData.Policies[0].Name)
				}
			})
		})
	}
}

// Test the conditions under which we should create the anonymous token
// policy.
func TestRun_AnonymousTokenPolicy(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		Flags              []string
		SecondaryDC        bool
		ExpAnonymousPolicy bool
	}{
		"dns, primary dc": {
			Flags:              []string{"-allow-dns"},
			SecondaryDC:        false,
			ExpAnonymousPolicy: true,
		},
		"dns, secondary dc": {
			Flags:              []string{"-allow-dns"},
			SecondaryDC:        true,
			ExpAnonymousPolicy: false,
		},
		"auth method, primary dc, no replication (deprecated)": {
			Flags:              []string{"-create-inject-auth-method"},
			SecondaryDC:        false,
			ExpAnonymousPolicy: false,
		},
		"auth method, primary dc, with federation": {
			Flags:              []string{"-create-inject-auth-method", "-federation"},
			SecondaryDC:        false,
			ExpAnonymousPolicy: true,
		},
		"auth method, secondary dc, with federation": {
			Flags:              []string{"-create-inject-auth-method", "-federation"},
			SecondaryDC:        true,
			ExpAnonymousPolicy: false,
		},
		"auth method, secondary dc (deprecated)": {
			Flags:              []string{"-create-inject-auth-method"},
			SecondaryDC:        true,
			ExpAnonymousPolicy: false,
		},
		"auth method, primary dc, no replication": {
			Flags:              []string{"-create-inject-token"},
			SecondaryDC:        false,
			ExpAnonymousPolicy: false,
		},
		"auth method, secondary dc": {
			Flags:              []string{"-create-inject-token"},
			SecondaryDC:        true,
			ExpAnonymousPolicy: false,
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			flags := c.Flags
			var k8s *fake.Clientset
			var consulHTTPAddr string
			var consul *api.Client

			if c.SecondaryDC {
				var cleanup func()
				bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
				k8s, consul, consulHTTPAddr, cleanup = mockReplicatedSetup(t, bootToken)
				defer cleanup()

				tmp, err := ioutil.TempFile("", "")
				require.NoError(t, err)
				_, err = tmp.WriteString(bootToken)
				require.NoError(t, err)
				flags = append(flags, "-acl-replication-token-file", tmp.Name())
			} else {
				var testSvr *testutil.TestServer
				k8s, testSvr = completeSetup(t)
				defer testSvr.Stop()
				consulHTTPAddr = testSvr.HTTPAddr
			}
			setUpK8sServiceAccount(t, k8s, ns)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			cmdArgs := append([]string{
				"-timeout=1m",
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-server-address", strings.Split(consulHTTPAddr, ":")[0],
				"-server-port", strings.Split(consulHTTPAddr, ":")[1],
			}, flags...)
			responseCode := cmd.Run(cmdArgs)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			if !c.SecondaryDC {
				bootToken := getBootToken(t, k8s, resourcePrefix, ns)
				var err error
				consul, err = api.NewClient(&api.Config{
					Address: consulHTTPAddr,
					Token:   bootToken,
				})
				require.NoError(t, err)
			}

			anonPolicyName := "anonymous-token-policy"
			if c.ExpAnonymousPolicy {
				// Check that the anonymous token policy was created.
				policy := policyExists(t, anonPolicyName, consul)
				// Should be a global policy.
				require.Len(t, policy.Datacenters, 0)

				// Check that the anonymous token has the policy.
				tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: "anonymous"})
				require.NoError(t, err)
				require.Equal(t, anonPolicyName, tokenData.Policies[0].Name)
			} else {
				policies, _, err := consul.ACL().PolicyList(nil)
				require.NoError(t, err)
				for _, p := range policies {
					if p.Name == anonPolicyName {
						t.Error("anon policy was created")
					}
				}
			}

			// Test that if the same command is re-run it doesn't error.
			t.Run("retried", func(t *testing.T) {
				ui := cli.NewMockUi()
				cmd := Command{
					UI:        ui,
					clientset: k8s,
				}
				cmd.init()
				responseCode := cmd.Run(cmdArgs)
				require.Equal(t, 0, responseCode, ui.ErrorWriter.String())
			})
		})
	}
}

func TestRun_ConnectInjectAuthMethod(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		flags        []string
		expectedHost string
	}{
		"-create-inject-token flag": {
			flags:        []string{"-create-inject-token"},
			expectedHost: "https://kubernetes.default.svc",
		},
		"-create-inject-auth-method flag": {
			flags:        []string{"-create-inject-auth-method"},
			expectedHost: "https://kubernetes.default.svc",
		},
		"-inject-auth-method-host flag (deprecated)": {
			flags: []string{
				"-create-inject-auth-method",
				"-inject-auth-method-host=https://my-kube.com",
			},
			expectedHost: "https://my-kube.com",
		},
		"-inject-auth-method-host flag": {
			flags: []string{
				"-create-inject-token",
				"-inject-auth-method-host=https://my-kube.com",
			},
			expectedHost: "https://my-kube.com",
		},
	}
	for testName, c := range cases {
		t.Run(testName, func(t *testing.T) {

			k8s, testSvr := completeSetup(t)
			defer testSvr.Stop()
			caCert, jwtToken := setUpK8sServiceAccount(t, k8s, ns)
			require := require.New(t)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			bindingRuleSelector := "serviceaccount.name!=default"
			cmdArgs := []string{
				"-timeout=1m",
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-server-address", strings.Split(testSvr.HTTPAddr, ":")[0],
				"-server-port", strings.Split(testSvr.HTTPAddr, ":")[1],
				"-acl-binding-rule-selector=" + bindingRuleSelector,
			}
			cmdArgs = append(cmdArgs, c.flags...)
			responseCode := cmd.Run(cmdArgs)
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Check that the auth method was created.
			bootToken := getBootToken(t, k8s, resourcePrefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testSvr.HTTPAddr,
			})
			require.NoError(err)
			authMethodName := resourcePrefix + "-k8s-auth-method"
			authMethod, _, err := consul.ACL().AuthMethodRead(authMethodName,
				&api.QueryOptions{Token: bootToken})
			require.NoError(err)
			require.Contains(authMethod.Config, "Host")
			require.Equal(authMethod.Config["Host"], c.expectedHost)
			require.Contains(authMethod.Config, "CACert")
			require.Equal(authMethod.Config["CACert"], caCert)
			require.Contains(authMethod.Config, "ServiceAccountJWT")
			require.Equal(authMethod.Config["ServiceAccountJWT"], jwtToken)

			// Check that the binding rule was created.
			rules, _, err := consul.ACL().BindingRuleList(authMethodName, &api.QueryOptions{Token: bootToken})
			require.NoError(err)
			require.Len(rules, 1)
			require.Equal("service", string(rules[0].BindType))
			require.Equal("${serviceaccount.name}", rules[0].BindName)
			require.Equal(bindingRuleSelector, rules[0].Selector)

			// Test that if the same command is re-run it doesn't error.
			t.Run("retried", func(t *testing.T) {
				ui := cli.NewMockUi()
				cmd := Command{
					UI:        ui,
					clientset: k8s,
				}
				cmd.init()
				responseCode := cmd.Run(cmdArgs)
				require.Equal(0, responseCode, ui.ErrorWriter.String())
			})
		})
	}
}

// Test that when we provide a different k8s auth method parameters,
// the auth method is updated.
func TestRun_ConnectInjectAuthMethodUpdates(t *testing.T) {
	t.Parallel()

	// Test with deprecated -create-inject-auth-method flag.
	cases := []string{"-create-inject-auth-method", "-create-inject-token"}
	for _, flag := range cases {
		t.Run(flag, func(t *testing.T) {

			k8s, testSvr := completeSetup(t)
			defer testSvr.Stop()
			caCert, jwtToken := setUpK8sServiceAccount(t, k8s, ns)
			require := require.New(t)

			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}

			bindingRuleSelector := "serviceaccount.name!=default"

			// First, create an auth method using the defaults
			responseCode := cmd.Run([]string{
				"-timeout=1m",
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-server-address", strings.Split(testSvr.HTTPAddr, ":")[0],
				"-server-port", strings.Split(testSvr.HTTPAddr, ":")[1],
				flag,
				"-acl-binding-rule-selector=" + bindingRuleSelector,
			})
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Check that the auth method was created.
			bootToken := getBootToken(t, k8s, resourcePrefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testSvr.HTTPAddr,
			})
			require.NoError(err)
			authMethodName := resourcePrefix + "-k8s-auth-method"
			authMethod, _, err := consul.ACL().AuthMethodRead(authMethodName,
				&api.QueryOptions{Token: bootToken})
			require.NoError(err)
			require.NotNil(authMethod)
			require.Contains(authMethod.Config, "Host")
			require.Equal(authMethod.Config["Host"], defaultKubernetesHost)
			require.Contains(authMethod.Config, "CACert")
			require.Equal(authMethod.Config["CACert"], caCert)
			require.Contains(authMethod.Config, "ServiceAccountJWT")
			require.Equal(authMethod.Config["ServiceAccountJWT"], jwtToken)

			// Generate a new CA certificate
			_, _, caCertPem, _, err := cert.GenerateCA("kubernetes")
			require.NoError(err)

			// Overwrite the default kubernetes api, service account token and CA cert
			kubernetesHost := "https://kubernetes.example.com"
			// This token is the base64 encoded example token from jwt.io
			serviceAccountToken = "ZXlKaGJHY2lPaUpJVXpJMU5pSXNJblI1Y0NJNklrcFhWQ0o5LmV5SnpkV0lpT2lJeE1qTTBOVFkzT0Rrd0lpd2libUZ0WlNJNklrcHZhRzRnUkc5bElpd2lhV0YwSWpveE5URTJNak01TURJeWZRLlNmbEt4d1JKU01lS0tGMlFUNGZ3cE1lSmYzNlBPazZ5SlZfYWRRc3N3NWM="
			serviceAccountCACert = base64.StdEncoding.EncodeToString([]byte(caCertPem))

			// Create a new service account
			updatedCACert, updatedJWTToken := setUpK8sServiceAccount(t, k8s, ns)

			// Run command again
			responseCode = cmd.Run([]string{
				"-timeout=1m",
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-server-address", strings.Split(testSvr.HTTPAddr, ":")[0],
				"-server-port", strings.Split(testSvr.HTTPAddr, ":")[1],
				"-acl-binding-rule-selector=" + bindingRuleSelector,
				flag,
				"-inject-auth-method-host=" + kubernetesHost,
			})
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Check that the auth method has been updated
			authMethod, _, err = consul.ACL().AuthMethodRead(authMethodName,
				&api.QueryOptions{Token: bootToken})
			require.NoError(err)
			require.NotNil(authMethod)
			require.Contains(authMethod.Config, "Host")
			require.Equal(authMethod.Config["Host"], kubernetesHost)
			require.Contains(authMethod.Config, "CACert")
			require.Equal(authMethod.Config["CACert"], updatedCACert)
			require.Contains(authMethod.Config, "ServiceAccountJWT")
			require.Equal(authMethod.Config["ServiceAccountJWT"], updatedJWTToken)
		})
	}
}

// Test that ACL binding rules are updated if the rule selector changes.
func TestRun_BindingRuleUpdates(tt *testing.T) {
	tt.Parallel()

	// Test with deprecated -create-inject-auth-method flag.
	cases := []string{"-create-inject-auth-method", "-create-inject-token"}
	for _, flag := range cases {
		tt.Run(flag, func(t *testing.T) {
			k8s, testSvr := completeSetup(t)
			setUpK8sServiceAccount(t, k8s, ns)
			defer testSvr.Stop()
			require := require.New(t)

			consul, err := api.NewClient(&api.Config{
				Address: testSvr.HTTPAddr,
			})
			require.NoError(err)

			ui := cli.NewMockUi()
			commonArgs := []string{
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-server-address", strings.Split(testSvr.HTTPAddr, ":")[0],
				"-server-port", strings.Split(testSvr.HTTPAddr, ":")[1],
				flag,
			}
			firstRunArgs := append(commonArgs,
				"-acl-binding-rule-selector=serviceaccount.name!=default",
			)
			// On the second run, we change the binding rule selector.
			secondRunArgs := append(commonArgs,
				"-acl-binding-rule-selector=serviceaccount.name!=changed",
			)

			// Run the command first to populate the binding rule.
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			responseCode := cmd.Run(firstRunArgs)
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Validate the binding rule.
			{
				queryOpts := &api.QueryOptions{Token: getBootToken(t, k8s, resourcePrefix, ns)}
				authMethodName := resourcePrefix + "-k8s-auth-method"
				rules, _, err := consul.ACL().BindingRuleList(authMethodName, queryOpts)
				require.NoError(err)
				require.Len(rules, 1)
				actRule, _, err := consul.ACL().BindingRuleRead(rules[0].ID, queryOpts)
				require.NoError(err)
				require.NotNil(actRule)
				require.Equal("Kubernetes binding rule", actRule.Description)
				require.Equal(api.BindingRuleBindTypeService, actRule.BindType)
				require.Equal("${serviceaccount.name}", actRule.BindName)
				require.Equal("serviceaccount.name!=default", actRule.Selector)
			}

			// Re-run the command with namespace flags. The policies should be updated.
			// NOTE: We're redefining the command so that the old flag values are
			// reset.
			cmd = Command{
				UI:        ui,
				clientset: k8s,
			}
			responseCode = cmd.Run(secondRunArgs)
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Check the binding rule is changed expected.
			{
				queryOpts := &api.QueryOptions{Token: getBootToken(t, k8s, resourcePrefix, ns)}
				authMethodName := resourcePrefix + "-k8s-auth-method"
				rules, _, err := consul.ACL().BindingRuleList(authMethodName, queryOpts)
				require.NoError(err)
				require.Len(rules, 1)
				actRule, _, err := consul.ACL().BindingRuleRead(rules[0].ID, queryOpts)
				require.NoError(err)
				require.NotNil(actRule)
				require.Equal("Kubernetes binding rule", actRule.Description)
				require.Equal(api.BindingRuleBindTypeService, actRule.BindType)
				require.Equal("${serviceaccount.name}", actRule.BindName)
				require.Equal("serviceaccount.name!=changed", actRule.Selector)
			}
		})
	}
}

// Test that the catalog sync policy is updated if the Consul node name changes.
func TestRun_SyncPolicyUpdates(t *testing.T) {
	t.Parallel()
	k8s, testSvr := completeSetup(t)
	defer testSvr.Stop()
	require := require.New(t)

	ui := cli.NewMockUi()
	commonArgs := []string{
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-server-address", strings.Split(testSvr.HTTPAddr, ":")[0],
		"-server-port", strings.Split(testSvr.HTTPAddr, ":")[1],
		"-create-sync-token",
	}
	firstRunArgs := append(commonArgs,
		"-sync-consul-node-name=k8s-sync",
	)
	// On the second run, we change the sync node name.
	secondRunArgs := append(commonArgs,
		"-sync-consul-node-name=new-node-name",
	)

	// Run the command first to populate the sync policy.
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	responseCode := cmd.Run(firstRunArgs)
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Create consul client
	bootToken := getBootToken(t, k8s, resourcePrefix, ns)
	consul, err := api.NewClient(&api.Config{
		Address: testSvr.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(err)

	// Get and check the sync policy details
	firstPolicies, _, err := consul.ACL().PolicyList(nil)
	require.NoError(err)

	for _, p := range firstPolicies {
		if p.Name == "catalog-sync-token" {
			policy, _, err := consul.ACL().PolicyRead(p.ID, nil)
			require.NoError(err)

			// Check the node name in the policy
			require.Contains(policy.Rules, "k8s-sync")
		}
	}

	// Re-run the command with a new Consul node name. The sync policy should be updated.
	// NOTE: We're redefining the command so that the old flag values are reset.
	cmd = Command{
		UI:        ui,
		clientset: k8s,
	}
	responseCode = cmd.Run(secondRunArgs)
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Get and check the sync policy details
	secondPolicies, _, err := consul.ACL().PolicyList(nil)
	require.NoError(err)

	for _, p := range secondPolicies {
		if p.Name == "catalog-sync-token" {
			policy, _, err := consul.ACL().PolicyRead(p.ID, nil)
			require.NoError(err)

			// Check the node name in the policy
			require.Contains(policy.Rules, "new-node-name")
		}
	}
}

// Test that we give an error if an ACL policy we were going to create
// already exists but it has a different description than what consul-k8s
// expected. In this case, it's likely that a user manually created an ACL
// policy with the same name and so we want to error.
// This test will test with the catalog sync policy but any policy
// that we try to update will work for testing.
func TestRun_ErrorsOnDuplicateACLPolicy(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	// Create Consul with ACLs already bootstrapped so that we can
	// then seed it with our manually created policy.
	bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	tokenFile := common.WriteTempFile(t, bootToken)
	k8s, testAgent := completeBootstrappedSetup(t, bootToken)
	setUpK8sServiceAccount(t, k8s, ns)
	defer testAgent.Stop()

	consul, err := api.NewClient(&api.Config{
		Address: testAgent.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(err)

	// Create the policy manually.
	description := "not the expected description"
	policy, _, err := consul.ACL().PolicyCreate(&api.ACLPolicy{
		Name:        "catalog-sync-token",
		Description: description,
	}, nil)
	require.NoError(err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmdArgs := []string{
		"-timeout=1s",
		"-k8s-namespace", ns,
		"-bootstrap-token-file", tokenFile,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-server-address", strings.Split(testAgent.HTTPAddr, ":")[0],
		"-server-port", strings.Split(testAgent.HTTPAddr, ":")[1],
		"-create-sync-token",
	}
	responseCode := cmd.Run(cmdArgs)

	// We expect the command to time out.
	require.Equal(1, responseCode)
	// NOTE: Since the error is logged through the logger instead of the UI
	// there's no good way to test that we logged the expected error however
	// we also test this directly in create_or_update_test.go.

	// Check that the policy wasn't modified.
	rereadPolicy, _, err := consul.ACL().PolicyRead(policy.ID, nil)
	require.NoError(err)
	require.Equal(description, rereadPolicy.Description)
}

// Test that if the servers aren't available at first that bootstrap
// still succeeds.
func TestRun_DelayedServers(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	randomPorts := freeport.MustTake(6)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}

	// Start the command before the server is up.
	// Run in a goroutine so we can start the server asynchronously
	done := make(chan bool)
	var responseCode int
	go func() {
		responseCode = cmd.Run([]string{
			"-timeout=1m",
			"-resource-prefix=" + resourcePrefix,
			"-k8s-namespace=" + ns,
			"-server-address=127.0.0.1",
			"-server-port=" + strconv.Itoa(randomPorts[1]),
		})
		close(done)
	}()

	// Asynchronously start the test server after a delay.
	testServerReady := make(chan bool)
	var srv *testutil.TestServer
	go func() {
		// Start the servers after a delay between 100 and 500ms.
		// It's randomized to ensure we're not relying on specific timing.
		delay := 100 + rand.Intn(400)
		time.Sleep(time.Duration(delay) * time.Millisecond)

		var err error
		srv, err = testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
			c.ACL.Enabled = true

			c.Ports = &testutil.TestPortConfig{
				DNS:     randomPorts[0],
				HTTP:    randomPorts[1],
				HTTPS:   randomPorts[2],
				SerfLan: randomPorts[3],
				SerfWan: randomPorts[4],
				Server:  randomPorts[5],
			}
		})
		require.NoError(err)
		close(testServerReady)
	}()

	// Wait for server to come up
	select {
	case <-testServerReady:
		defer srv.Stop()
	case <-time.After(5 * time.Second):
		require.FailNow("test server took longer than 5s to come up")
	}

	// Wait for the command to exit.
	select {
	case <-done:
		require.Equal(0, responseCode, ui.ErrorWriter.String())
	case <-time.After(5 * time.Second):
		require.FailNow("command did not exit after 5s")
	}

	// Test that the bootstrap kube secret is created.
	bootToken := getBootToken(t, k8s, resourcePrefix, ns)

	// Check that it has the right policies.
	consul, err := api.NewClient(&api.Config{
		Address: srv.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(err)
	tokenData, _, err := consul.ACL().TokenReadSelf(nil)
	require.NoError(err)
	require.Equal("global-management", tokenData.Policies[0].Name)

	// Check that the agent policy was created.
	policyExists(t, "agent-token", consul)
}

// Test that if there's no leader, we retry until one is elected.
func TestRun_NoLeader(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall

	// Start the Consul server.
	numACLBootCalls := 0
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})

		switch r.URL.Path {
		case "/v1/acl/bootstrap":
			// On the first two calls, return the error that results from no leader
			// being elected.
			if numACLBootCalls < 2 {
				w.WriteHeader(500)
				fmt.Fprintln(w, "The ACL system is currently in legacy mode.")
			} else {
				fmt.Fprintln(w, "{}")
			}
			numACLBootCalls++
		case "/v1/agent/self":
			fmt.Fprintln(w, `{"Config": {"Datacenter": "dc1", "PrimaryDatacenter": "dc1"}}`)
		case "/v1/acl/tokens":
			fmt.Fprintln(w, `[]`)
		default:
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()

	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}

	done := make(chan bool)
	var responseCode int
	go func() {
		responseCode = cmd.Run([]string{
			"-timeout=1m",
			"-resource-prefix=" + resourcePrefix,
			"-k8s-namespace=" + ns,
			"-server-address=" + serverURL.Hostname(),
			"-server-port=" + serverURL.Port(),
		})
		close(done)
	}()

	select {
	case <-done:
		require.Equal(0, responseCode, ui.ErrorWriter.String())
	case <-time.After(5 * time.Second):
		require.FailNow("command did not complete within 5s")
	}

	// Test that the bootstrap kube secret is created.
	getBootToken(t, k8s, resourcePrefix, ns)

	// Test that the expected API calls were made.
	require.Equal([]APICall{
		// Bootstrap will have been called 3 times.
		{
			"PUT",
			"/v1/acl/bootstrap",
		},
		{
			"PUT",
			"/v1/acl/bootstrap",
		},
		{
			"PUT",
			"/v1/acl/bootstrap",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"GET",
			"/v1/acl/tokens",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
		{
			"PUT",
			"/v1/agent/token/agent",
		},
		{
			"GET",
			"/v1/agent/self",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
	}, consulAPICalls)
}

func TestConsulDatacenterList(t *testing.T) {
	cases := map[string]struct {
		agentSelfResponse map[string]map[string]interface{}
		expDC             string
		expPrimaryDC      string
		expErr            string
	}{
		"empty map": {
			agentSelfResponse: make(map[string]map[string]interface{}),
			expErr:            "/agent/self response did not contain Config.Datacenter key: map[]",
		},
		"Config.Datacenter not string": {
			agentSelfResponse: map[string]map[string]interface{}{
				"Config": {
					"Datacenter": 10,
				},
			},
			expErr: "1 error(s) decoding:\n\n* 'Config.Datacenter' expected type 'string', got unconvertible type 'float64', value: '10'",
		},
		"Config.PrimaryDatacenter and DebugConfig.PrimaryDatacenter empty": {
			agentSelfResponse: map[string]map[string]interface{}{
				"Config": {
					"Datacenter": "dc",
				},
			},
			expErr: "both Config.PrimaryDatacenter and DebugConfig.PrimaryDatacenter are empty: map[Config:map[Datacenter:dc]]",
		},
		"Config.PrimaryDatacenter set": {
			agentSelfResponse: map[string]map[string]interface{}{
				"Config": {
					"Datacenter":        "dc",
					"PrimaryDatacenter": "primary",
				},
			},
			expDC:        "dc",
			expPrimaryDC: "primary",
		},
		"DebugConfig.PrimaryDatacenter set": {
			agentSelfResponse: map[string]map[string]interface{}{
				"Config": {
					"Datacenter": "dc",
				},
				"DebugConfig": {
					"PrimaryDatacenter": "primary",
				},
			},
			expDC:        "dc",
			expPrimaryDC: "primary",
		},
		"both Config.PrimaryDatacenter and DebugConfig.PrimaryDatacenter set": {
			agentSelfResponse: map[string]map[string]interface{}{
				"Config": {
					"Datacenter":        "dc",
					"PrimaryDatacenter": "primary",
				},
				"DebugConfig": {
					"PrimaryDatacenter": "primary",
				},
			},
			expDC:        "dc",
			expPrimaryDC: "primary",
		},
	}

	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			jsonResponse, err := json.Marshal(c.agentSelfResponse)
			require.NoError(t, err)

			consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/agent/self":
					fmt.Fprintln(w, string(jsonResponse))
				default:
					t.Fatalf("unexpected request to %s", r.URL.Path)
				}
			}))
			defer consulServer.Close()

			consulClient, err := api.NewClient(&api.Config{Address: consulServer.URL})
			require.NoError(t, err)

			command := Command{
				log: hclog.New(hclog.DefaultOptions),
				ctx: context.Background(),
			}
			actDC, actPrimaryDC, err := command.consulDatacenterList(consulClient)
			if c.expErr != "" {
				require.EqualError(t, err, c.expErr)
			} else {
				require.Equal(t, c.expDC, actDC)
				require.Equal(t, c.expPrimaryDC, actPrimaryDC)
			}
		})
	}
}

// Test that if creating client tokens fails at first, we retry.
func TestRun_ClientTokensRetry(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall

	// Start the Consul server.
	numPolicyCalls := 0
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})

		switch r.URL.Path {
		// The second call to create a policy will fail. This is the client
		// token call.
		case "/v1/acl/policy":
			if numPolicyCalls == 1 {
				w.WriteHeader(500)
				fmt.Fprintln(w, "The ACL system is currently in legacy mode.")
			} else {
				fmt.Fprintln(w, "{}")
			}
			numPolicyCalls++
		case "/v1/agent/self":
			fmt.Fprintln(w, `{"Config": {"Datacenter": "dc1", "PrimaryDatacenter": "dc1"}}`)
		case "/v1/acl/tokens":
			fmt.Fprintln(w, `[]`)
		default:
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()

	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	responseCode := cmd.Run([]string{
		"-timeout=1m",
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-server-address=" + serverURL.Hostname(),
		"-server-port=" + serverURL.Port(),
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the expected API calls were made.
	require.Equal([]APICall{
		{
			"PUT",
			"/v1/acl/bootstrap",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"GET",
			"/v1/acl/tokens",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
		{
			"PUT",
			"/v1/agent/token/agent",
		},
		{
			"GET",
			"/v1/agent/self",
		},
		// This call should happen twice since the first will fail.
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
	}, consulAPICalls)
}

// Test if there is an old bootstrap Secret we still try to create and set
// server tokens.
func TestRun_AlreadyBootstrapped(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall

	// Start the Consul server.
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})
		switch r.URL.Path {
		case "/v1/agent/self":
			fmt.Fprintln(w, `{"Config": {"Datacenter": "dc1", "PrimaryDatacenter": "dc1"}}`)
		case "/v1/acl/tokens":
			fmt.Fprintln(w, `[]`)
		default:
			// Send an empty JSON response with code 200 to all calls.
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()

	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)

	// Create the bootstrap secret.
	_, err = k8s.CoreV1().Secrets(ns).Create(
		context.Background(),
		&v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourcePrefix + "-bootstrap-acl-token",
			},
			Data: map[string][]byte{
				"token": []byte("old-token"),
			},
		},
		metav1.CreateOptions{})
	require.NoError(err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}

	responseCode := cmd.Run([]string{
		"-timeout=500ms",
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-server-address=" + serverURL.Hostname(),
		"-server-port=" + serverURL.Port(),
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the Secret is the same.
	secret, err := k8s.CoreV1().Secrets(ns).Get(context.Background(), resourcePrefix+"-bootstrap-acl-token", metav1.GetOptions{})
	require.NoError(err)
	require.Contains(secret.Data, "token")
	require.Equal("old-token", string(secret.Data["token"]))

	// Test that the expected API calls were made.
	require.Equal([]APICall{
		// We expect calls for updating the server policy, setting server tokens,
		// and updating client policy.
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"GET",
			"/v1/acl/tokens",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
		{
			"PUT",
			"/v1/agent/token/agent",
		},
		{
			"GET",
			"/v1/agent/self",
		},
		{
			"PUT",
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
		},
	}, consulAPICalls)
}

// Test if there is an old bootstrap Secret and the server token exists
// that we don't try and recreate the token.
func TestRun_AlreadyBootstrapped_ServerTokenExists(t *testing.T) {
	t.Parallel()
	require := require.New(t)

	// First set everything up with ACLs bootstrapped.
	bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	k8s, testAgent := completeBootstrappedSetup(t, bootToken)
	setUpK8sServiceAccount(t, k8s, ns)
	defer testAgent.Stop()
	k8s.CoreV1().Secrets(ns).Create(context.Background(), &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-bootstrap-acl-token",
		},
		Data: map[string][]byte{
			"token": []byte(bootToken),
		},
	}, metav1.CreateOptions{})

	consulClient, err := api.NewClient(&api.Config{
		Address: testAgent.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(err)
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}

	// Create the server policy and token _before_ we run the command.
	agentPolicyRules, err := cmd.agentRules()
	require.NoError(err)
	policy, _, err := consulClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name:        "agent-token",
		Description: "Agent Token Policy",
		Rules:       agentPolicyRules,
	}, nil)
	require.NoError(err)
	_, _, err = consulClient.ACL().TokenCreate(&api.ACLToken{
		Description: fmt.Sprintf("Server Token for %s", strings.Split(testAgent.HTTPAddr, ":")[0]),
		Policies: []*api.ACLTokenPolicyLink{
			{
				Name: policy.Name,
			},
		},
	}, nil)
	require.NoError(err)

	// Run the command.
	cmdArgs := []string{
		"-timeout=1m",
		"-k8s-namespace", ns,
		"-server-address", strings.Split(testAgent.HTTPAddr, ":")[0],
		"-server-port", strings.Split(testAgent.HTTPAddr, ":")[1],
		"-resource-prefix", resourcePrefix,
	}

	responseCode := cmd.Run(cmdArgs)
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Check that only one server token exists, i.e. it didn't create an
	// extra token.
	tokens, _, err := consulClient.ACL().TokenList(nil)
	require.NoError(err)
	count := 0
	for _, token := range tokens {
		if len(token.Policies) == 1 && token.Policies[0].Name == policy.Name {
			count++
		}
	}
	require.Equal(1, count)
}

// Test if there is a provided bootstrap we skip bootstrapping of the servers
// and continue on to the next step.
func TestRun_SkipBootstrapping_WhenBootstrapTokenIsProvided(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	tokenFile := common.WriteTempFile(t, bootToken)

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall

	// Start the Consul server.
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})
		switch r.URL.Path {
		case "/v1/agent/self":
			fmt.Fprintln(w, `{"Config": {"Datacenter": "dc1", "PrimaryDatacenter": "dc1"}}`)
		default:
			// Send an empty JSON response with code 200 to all calls.
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()

	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}

	responseCode := cmd.Run([]string{
		"-timeout=500ms",
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-server-address=" + serverURL.Hostname(),
		"-server-port=" + serverURL.Port(),
		"-bootstrap-token-file=" + tokenFile,
		"-create-client-token=false", // disable client token, so there are less calls
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the expected API calls were made.
	// We expect not to see the call to /v1/acl/bootstrap.
	require.Equal([]APICall{
		// We only expect the calls to get the datacenter
		{
			"GET",
			"/v1/agent/self",
		},
	}, consulAPICalls)
}

// Test that we exit after timeout.
func TestRun_Timeout(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}

	responseCode := cmd.Run([]string{
		"-timeout=500ms",
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-server-address=foo",
	})
	require.Equal(1, responseCode, ui.ErrorWriter.String())
}

// Test that the bootstrapping process can make calls to Consul API over HTTPS
// when the consul agent is configured with HTTPS.
func TestRun_HTTPS(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	caFile, certFile, keyFile := test.GenerateServerCerts(t)

	srv, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true

		c.CAFile = caFile
		c.CertFile = certFile
		c.KeyFile = keyFile
	})
	require.NoError(err)
	defer srv.Stop()

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}

	responseCode := cmd.Run([]string{
		"-timeout=1m",
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-use-https",
		"-consul-tls-server-name", "server.dc1.consul",
		"-consul-ca-cert", caFile,
		"-server-address=" + strings.Split(srv.HTTPSAddr, ":")[0],
		"-server-port=" + strings.Split(srv.HTTPSAddr, ":")[1],
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the bootstrap token is created to make sure the bootstrapping succeeded.
	// The presence of the bootstrap token tells us that the API calls to Consul have been successful.
	tokenSecret, err := k8s.CoreV1().Secrets(ns).Get(context.Background(), resourcePrefix+"-bootstrap-acl-token", metav1.GetOptions{})
	require.NoError(err)
	require.NotNil(tokenSecret)
	_, ok := tokenSecret.Data["token"]
	require.True(ok)
}

// Test that the ACL replication token created from the primary DC can be used
// for replication in the secondary DC.
func TestRun_ACLReplicationTokenValid(t *testing.T) {
	t.Parallel()

	secondaryK8s, secondaryConsulClient, secondaryAddr, aclReplicationToken, clean := completeReplicatedSetup(t)
	defer clean()

	// completeReplicatedSetup ran the command in our primary dc so now we
	// need to run the command in our secondary dc.
	tokenFile := common.WriteTempFile(t, aclReplicationToken)
	secondaryUI := cli.NewMockUi()
	secondaryCmd := Command{
		UI:        secondaryUI,
		clientset: secondaryK8s,
	}
	secondaryCmd.init()
	secondaryCmdArgs := []string{
		"-federation",
		"-timeout=1m",
		"-k8s-namespace=" + ns,
		"-server-address", strings.Split(secondaryAddr, ":")[0],
		"-server-port", strings.Split(secondaryAddr, ":")[1],
		"-resource-prefix=" + resourcePrefix,
		"-acl-replication-token-file", tokenFile,
		"-create-client-token",
		"-create-mesh-gateway-token",
	}
	responseCode := secondaryCmd.Run(secondaryCmdArgs)
	require.Equal(t, 0, responseCode, secondaryUI.ErrorWriter.String())

	// Test that replication was successful.
	retry.Run(t, func(r *retry.R) {
		replicationStatus, _, err := secondaryConsulClient.ACL().Replication(nil)
		require.NoError(t, err)
		require.True(t, replicationStatus.Enabled)
		require.Greater(t, replicationStatus.ReplicatedIndex, uint64(0))
	})

	// Test that the client policy was created.
	retry.Run(t, func(r *retry.R) {
		p := policyExists(r, "client-token-dc2", secondaryConsulClient)
		require.Equal(r, []string{"dc2"}, p.Datacenters)
	})

	// Test that the mesh-gateway policy was created. This is a global policy
	// so replication has to have worked for it to exist.
	retry.Run(t, func(r *retry.R) {
		p := policyExists(r, "mesh-gateway-token-dc2", secondaryConsulClient)
		require.Len(r, p.Datacenters, 0)
	})
}

// Test that if acl replication is enabled, we don't create an anonymous token policy.
func TestRun_AnonPolicy_IgnoredWithReplication(t *testing.T) {
	// The anonymous policy is configured when one of these flags is set.
	cases := []string{"-allow-dns", "-create-inject-auth-method"}
	for _, flag := range cases {
		t.Run(flag, func(t *testing.T) {
			bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
			tokenFile := common.WriteTempFile(t, bootToken)
			k8s, consul, serverAddr, cleanup := mockReplicatedSetup(t, bootToken)
			setUpK8sServiceAccount(t, k8s, ns)
			defer cleanup()

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			cmdArgs := append([]string{
				"-timeout=1m",
				"-k8s-namespace=" + ns,
				"-acl-replication-token-file", tokenFile,
				"-server-address", strings.Split(serverAddr, ":")[0],
				"-server-port", strings.Split(serverAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
			}, flag)
			responseCode := cmd.Run(cmdArgs)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			// The anonymous token policy should not have been created.
			policies, _, err := consul.ACL().PolicyList(nil)
			require.NoError(t, err)
			for _, p := range policies {
				if p.Name == "anonymous-token-policy" {
					require.Fail(t, "anonymous-token-policy exists")
				}
			}
		})
	}
}

// Test that when the -server-address contains a cloud-auto join string,
// we are still able to bootstrap ACLs.
func TestRun_CloudAutoJoin(t *testing.T) {
	t.Parallel()

	k8s, testSvr := completeSetup(t)
	defer testSvr.Stop()
	require := require.New(t)

	// create a mock provider
	// that always returns the server address
	// provided through the cloud-auto join string
	provider := new(mocks.MockProvider)
	// create stubs for our MockProvider so that it returns
	// the address of the test agent
	provider.On("Addrs", mock.Anything, mock.Anything).Return([]string{"127.0.0.1"}, nil)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
		providers: map[string]discover.Provider{"mock": provider},
	}
	args := []string{
		"-timeout=1m",
		"-k8s-namespace=" + ns,
		"-resource-prefix=" + resourcePrefix,
		"-server-address", "provider=mock",
		"-server-port", strings.Split(testSvr.HTTPAddr, ":")[1],
	}
	responseCode := cmd.Run(args)
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// check that the provider has been called
	provider.AssertNumberOfCalls(t, "Addrs", 1)

	// Test that the bootstrap kube secret is created.
	bootToken := getBootToken(t, k8s, resourcePrefix, ns)

	// Check that it has the right policies.
	consul, err := api.NewClient(&api.Config{
		Address: testSvr.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(err)
	tokenData, _, err := consul.ACL().TokenReadSelf(nil)
	require.NoError(err)
	require.Equal("global-management", tokenData.Policies[0].Name)

	// Check that the agent policy was created.
	agentPolicy := policyExists(t, "agent-token", consul)
	// Should be a global policy.
	require.Len(agentPolicy.Datacenters, 0)
}

func TestRun_GatewayErrors(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		flags []string
	}{
		"ingress empty name": {
			flags: []string{"-ingress-gateway-name="},
		},
		"ingress namespace": {
			flags: []string{"-ingress-gateway-name=name.namespace"},
		},
		"ingress dot": {
			flags: []string{"-ingress-gateway-name=name."},
		},
		"terminating empty name": {
			flags: []string{"-terminating-gateway-name="},
		},
		"terminating namespace": {
			flags: []string{"-terminating-gateway-name=name.namespace"},
		},
		"terminating dot": {
			flags: []string{"-terminating-gateway-name=name."},
		},
	}
	for testName, c := range cases {
		t.Run(testName, func(tt *testing.T) {

			k8s, testSvr := completeSetup(tt)
			defer testSvr.Stop()
			require := require.New(tt)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmdArgs := []string{
				"-timeout=500ms",
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-server-address", strings.Split(testSvr.HTTPAddr, ":")[0],
				"-server-port", strings.Split(testSvr.HTTPAddr, ":")[1],
			}
			cmdArgs = append(cmdArgs, c.flags...)
			responseCode := cmd.Run(cmdArgs)
			require.Equal(1, responseCode, ui.ErrorWriter.String())
		})
	}
}

// Set up test consul agent and kubernetes cluster.
func completeSetup(t *testing.T) (*fake.Clientset, *testutil.TestServer) {
	k8s := fake.NewSimpleClientset()

	svr, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
	})
	require.NoError(t, err)
	svr.WaitForLeader(t)

	return k8s, svr
}

// Set up test consul agent and kubernetes cluster.
// The consul agent is bootstrapped with the master token.
func completeBootstrappedSetup(t *testing.T, masterToken string) (*fake.Clientset, *testutil.TestServer) {
	k8s := fake.NewSimpleClientset()

	svr, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.Tokens.Master = masterToken
	})
	require.NoError(t, err)
	svr.WaitForActiveCARoot(t)

	return k8s, svr
}

// completeReplicatedSetup sets up two Consul servers with ACL replication
// using the server-acl-init command to start the replication.
// Returns the Kubernetes client for the secondary DC,
// a Consul API client initialized for the secondary DC,
// the address of the secondary Consul server,
// the replication token generated and a cleanup function
// that should be called at the end of the test that cleans up resources.
func completeReplicatedSetup(t *testing.T) (*fake.Clientset, *api.Client, string, string, func()) {
	return replicatedSetup(t, "")
}

// mockReplicatedSetup sets up two Consul servers with ACL replication.
// It's a mock setup because we don't run the server-acl-init
// command to set up replication but do it in config using the bootstrap
// token. See completeReplicatedSetup for a complete setup using the command.
// Returns the Kubernetes client for the secondary DC,
// a Consul API client initialized for the secondary DC,
// the address of the secondary Consul server, and a
// cleanup function that should be called at the end of the test that cleans
// up resources.
func mockReplicatedSetup(t *testing.T, bootToken string) (*fake.Clientset, *api.Client, string, func()) {
	k8sClient, consulClient, serverAddr, _, cleanup := replicatedSetup(t, bootToken)
	return k8sClient, consulClient, serverAddr, cleanup
}

// replicatedSetup is a helper function for completeReplicatedSetup and
// mockReplicatedSetup. If bootToken is empty, it will run the server-acl-init
// command to set up replication. Otherwise it will do it through config.
// Returns the Kubernetes client for the secondary DC,
// a Consul API client initialized for the secondary DC,
// the address of the secondary Consul server, ACL replication token, and a
// cleanup function that should be called at the end of the test that cleans
// up resources.
func replicatedSetup(t *testing.T, bootToken string) (*fake.Clientset, *api.Client, string, string, func()) {
	primarySvr, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		if bootToken != "" {
			c.ACL.Tokens.Master = bootToken
		}
	})
	require.NoError(t, err)
	primarySvr.WaitForLeader(t)

	var aclReplicationToken string
	if bootToken == "" {
		primaryK8s := fake.NewSimpleClientset()
		require.NoError(t, err)

		// Run the command to bootstrap ACLs
		primaryUI := cli.NewMockUi()
		primaryCmd := Command{
			UI:        primaryUI,
			clientset: primaryK8s,
		}
		primaryCmd.init()
		primaryCmdArgs := []string{
			"-federation",
			"-k8s-namespace=" + ns,
			"-server-address", strings.Split(primarySvr.HTTPAddr, ":")[0],
			"-server-port", strings.Split(primarySvr.HTTPAddr, ":")[1],
			"-resource-prefix=" + resourcePrefix,
			"-create-acl-replication-token",
		}
		responseCode := primaryCmd.Run(primaryCmdArgs)
		require.Equal(t, 0, responseCode, primaryUI.ErrorWriter.String())

		// Retrieve the replication ACL token from the kubernetes secret.
		tokenSecret, err := primaryK8s.CoreV1().Secrets(ns).Get(context.Background(), "release-name-consul-acl-replication-acl-token", metav1.GetOptions{})
		require.NoError(t, err)
		require.NotNil(t, tokenSecret)
		aclReplicationTokenBytes, ok := tokenSecret.Data["token"]
		require.True(t, ok)
		aclReplicationToken = string(aclReplicationTokenBytes)
	}

	// Set up the secondary server that will federate with the primary.
	secondarySvr, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.Datacenter = "dc2"
		c.ACL.Enabled = true
		c.ACL.TokenReplication = true
		c.PrimaryDatacenter = "dc1"
		if bootToken == "" {
			c.ACL.Tokens.Agent = aclReplicationToken
			c.ACL.Tokens.Replication = aclReplicationToken
		} else {
			c.ACL.Tokens.Agent = bootToken
			c.ACL.Tokens.Replication = bootToken
		}
	})
	require.NoError(t, err)

	// Our consul client will use the secondary dc.
	clientToken := bootToken
	if bootToken == "" {
		clientToken = aclReplicationToken
	}

	// Create a consul client pointing to the primary server.
	// Note: We need to use the primary server to make the WAN join API call
	// because the secondary will not be able to verify this token
	// until ACL replication has started, and ACL replication cannot
	// be started because we haven't told the secondary where the primary
	// server is yet.
	consul, err := api.NewClient(&api.Config{
		Address: primarySvr.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(t, err)

	// WAN join primary to the secondary
	err = consul.Agent().Join(secondarySvr.WANAddr, true)
	require.NoError(t, err)

	secondarySvr.WaitForLeader(t)

	// Overwrite consul client, pointing it to the secondary DC
	consul, err = api.NewClient(&api.Config{
		Address: secondarySvr.HTTPAddr,
		Token:   clientToken,
	})
	require.NoError(t, err)

	// Finally, set up our kube cluster. It will use the secondary dc.
	k8s := fake.NewSimpleClientset()

	return k8s, consul, secondarySvr.HTTPAddr, aclReplicationToken, func() {
		primarySvr.Stop()
		secondarySvr.Stop()
	}
}

// getBootToken gets the bootstrap token from the Kubernetes secret. It will
// cause a test failure if the Secret doesn't exist or is malformed.
func getBootToken(t *testing.T, k8s *fake.Clientset, prefix string, k8sNamespace string) string {
	bootstrapSecret, err := k8s.CoreV1().Secrets(k8sNamespace).Get(context.Background(), fmt.Sprintf("%s-bootstrap-acl-token", prefix), metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, bootstrapSecret)
	bootToken, ok := bootstrapSecret.Data["token"]
	require.True(t, ok)
	return string(bootToken)
}

// setUpK8sServiceAccount creates a Service Account for the connect injector.
// This Service Account would normally automatically be created by Kubernetes
// when the injector deployment is created. It returns the Service Account
// CA Cert and JWT token.
func setUpK8sServiceAccount(t *testing.T, k8s *fake.Clientset, namespace string) (string, string) {
	// Create ServiceAccount for the kubernetes auth method if it doesn't exist,
	// otherwise, do nothing.
	serviceAccountName := resourcePrefix + "-connect-injector-authmethod-svc-account"
	sa, _ := k8s.CoreV1().ServiceAccounts(namespace).Get(context.Background(), serviceAccountName, metav1.GetOptions{})
	if sa == nil {
		// Create a service account that references two secrets.
		// The second secret is mimicking the behavior on Openshift,
		// where two secrets are injected: one with SA token and one with docker config.
		_, err := k8s.CoreV1().ServiceAccounts(namespace).Create(
			context.Background(),
			&v1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Name: serviceAccountName,
				},
				Secrets: []v1.ObjectReference{
					{
						Name: resourcePrefix + "-some-other-secret",
					},
					{
						Name: resourcePrefix + "-connect-injector-authmethod-svc-account",
					},
				},
			},
			metav1.CreateOptions{})
		require.NoError(t, err)
	}

	// Create the ServiceAccount Secret.
	caCertBytes, err := base64.StdEncoding.DecodeString(serviceAccountCACert)
	require.NoError(t, err)
	tokenBytes, err := base64.StdEncoding.DecodeString(serviceAccountToken)
	require.NoError(t, err)

	// Create a Kubernetes secret if it doesn't exist, otherwise update it
	secretName := resourcePrefix + "-connect-injector-authmethod-svc-account"
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: secretName,
		},
		Data: map[string][]byte{
			"ca.crt": caCertBytes,
			"token":  tokenBytes,
		},
		Type: v1.SecretTypeServiceAccountToken,
	}
	createOrUpdateSecret(t, k8s, secret, namespace)

	// Create the second secret of a different type
	otherSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-some-other-secret",
		},
		Data: map[string][]byte{},
		Type: v1.SecretTypeDockercfg,
	}
	createOrUpdateSecret(t, k8s, otherSecret, namespace)

	return string(caCertBytes), string(tokenBytes)
}

func createOrUpdateSecret(t *testing.T, k8s *fake.Clientset, secret *v1.Secret, namespace string) {
	existingSecret, _ := k8s.CoreV1().Secrets(namespace).Get(context.Background(), secret.Name, metav1.GetOptions{})
	var err error
	if existingSecret == nil {
		_, err = k8s.CoreV1().Secrets(namespace).Create(context.Background(), secret, metav1.CreateOptions{})
		require.NoError(t, err)
	} else {
		_, err = k8s.CoreV1().Secrets(namespace).Update(context.Background(), secret, metav1.UpdateOptions{})
		require.NoError(t, err)
	}
}

// policyExists asserts that policy with name exists. Returns the policy
// if it does, otherwise fails the test.
func policyExists(t require.TestingT, name string, client *api.Client) *api.ACLPolicyListEntry {
	policies, _, err := client.ACL().PolicyList(nil)
	require.NoError(t, err)
	var policy *api.ACLPolicyListEntry
	for _, p := range policies {
		if p.Name == name {
			policy = p
			break
		}
	}
	require.NotNil(t, policy, "policy %s not found", name)
	return policy
}

var serviceAccountCACert = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURDekNDQWZPZ0F3SUJBZ0lRS3pzN05qbDlIczZYYzhFWG91MjVoekFOQmdrcWhraUc5dzBCQVFzRkFEQXYKTVMwd0t3WURWUVFERXlRMU9XVTJaR00wTVMweU1EaG1MVFF3T1RVdFlUSTRPUzB4Wm1NM01EQmhZekZqWXpndwpIaGNOTVRrd05qQTNNVEF4TnpNeFdoY05NalF3TmpBMU1URXhOek14V2pBdk1TMHdLd1lEVlFRREV5UTFPV1UyClpHTTBNUzB5TURobUxUUXdPVFV0WVRJNE9TMHhabU0zTURCaFl6RmpZemd3Z2dFaU1BMEdDU3FHU0liM0RRRUIKQVFVQUE0SUJEd0F3Z2dFS0FvSUJBUURaakh6d3FvZnpUcEdwYzBNZElDUzdldXZmdWpVS0UzUEMvYXBmREFnQgo0anpFRktBNzgvOStLVUd3L2MvMFNIZVNRaE4rYThnd2xIUm5BejFOSmNmT0lYeTRkd2VVdU9rQWlGeEg4cGh0CkVDd2tlTk83ejhEb1Y4Y2VtaW5DUkhHamFSbW9NeHBaN2cycFpBSk5aZVB4aTN5MWFOa0ZBWGU5Z1NVU2RqUloKUlhZa2E3d2gyQU85azJkbEdGQVlCK3Qzdld3SjZ0d2pHMFR0S1FyaFlNOU9kMS9vTjBFMDFMekJjWnV4a04xawo4Z2ZJSHk3Yk9GQ0JNMldURURXLzBhQXZjQVByTzhETHFESis2TWpjM3I3K3psemw4YVFzcGIwUzA4cFZ6a2k1CkR6Ly84M2t5dTBwaEp1aWo1ZUI4OFY3VWZQWHhYRi9FdFY2ZnZyTDdNTjRmQWdNQkFBR2pJekFoTUE0R0ExVWQKRHdFQi93UUVBd0lDQkRBUEJnTlZIUk1CQWY4RUJUQURBUUgvTUEwR0NTcUdTSWIzRFFFQkN3VUFBNElCQVFCdgpRc2FHNnFsY2FSa3RKMHpHaHh4SjUyTm5SVjJHY0lZUGVOM1p2MlZYZTNNTDNWZDZHMzJQVjdsSU9oangzS21BCi91TWg2TmhxQnpzZWtrVHowUHVDM3dKeU0yT0dvblZRaXNGbHF4OXNGUTNmVTJtSUdYQ2Ezd0M4ZS9xUDhCSFMKdzcvVmVBN2x6bWozVFFSRS9XMFUwWkdlb0F4bjliNkp0VDBpTXVjWXZQMGhYS1RQQldsbnpJaWphbVU1MHIyWQo3aWEwNjVVZzJ4VU41RkxYL3Z4T0EzeTRyanBraldvVlFjdTFwOFRaclZvTTNkc0dGV3AxMGZETVJpQUhUdk9ICloyM2pHdWs2cm45RFVIQzJ4UGozd0NUbWQ4U0dFSm9WMzFub0pWNWRWZVE5MHd1c1h6M3ZURzdmaWNLbnZIRlMKeHRyNVBTd0gxRHVzWWZWYUdIMk8KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo="
var serviceAccountToken = "ZXlKaGJHY2lPaUpTVXpJMU5pSXNJbXRwWkNJNklpSjkuZXlKcGMzTWlPaUpyZFdKbGNtNWxkR1Z6TDNObGNuWnBZMlZoWTJOdmRXNTBJaXdpYTNWaVpYSnVaWFJsY3k1cGJ5OXpaWEoyYVdObFlXTmpiM1Z1ZEM5dVlXMWxjM0JoWTJVaU9pSmtaV1poZFd4MElpd2lhM1ZpWlhKdVpYUmxjeTVwYnk5elpYSjJhV05sWVdOamIzVnVkQzl6WldOeVpYUXVibUZ0WlNJNkltdG9ZV3RwTFdGeVlXTm9ibWxrTFdOdmJuTjFiQzFqYjI1dVpXTjBMV2x1YW1WamRHOXlMV0YxZEdodFpYUm9iMlF0YzNaakxXRmpZMjlvYm1SaWRpSXNJbXQxWW1WeWJtVjBaWE11YVc4dmMyVnlkbWxqWldGalkyOTFiblF2YzJWeWRtbGpaUzFoWTJOdmRXNTBMbTVoYldVaU9pSnJhR0ZyYVMxaGNtRmphRzVwWkMxamIyNXpkV3d0WTI5dWJtVmpkQzFwYm1wbFkzUnZjaTFoZFhSb2JXVjBhRzlrTFhOMll5MWhZMk52ZFc1MElpd2lhM1ZpWlhKdVpYUmxjeTVwYnk5elpYSjJhV05sWVdOamIzVnVkQzl6WlhKMmFXTmxMV0ZqWTI5MWJuUXVkV2xrSWpvaU4yVTVOV1V4TWprdFpUUTNNeTB4TVdVNUxUaG1ZV0V0TkRJd01UQmhPREF3TVRJeUlpd2ljM1ZpSWpvaWMzbHpkR1Z0T25ObGNuWnBZMlZoWTJOdmRXNTBPbVJsWm1GMWJIUTZhMmhoYTJrdFlYSmhZMmh1YVdRdFkyOXVjM1ZzTFdOdmJtNWxZM1F0YVc1cVpXTjBiM0l0WVhWMGFHMWxkR2h2WkMxemRtTXRZV05qYjNWdWRDSjkuWWk2M01NdHpoNU1CV0tLZDNhN2R6Q0pqVElURTE1aWtGeV9UbnBka19Bd2R3QTlKNEFNU0dFZUhONXZXdEN1dUZqb19sTUpxQkJQSGtLMkFxYm5vRlVqOW01Q29wV3lxSUNKUWx2RU9QNGZVUS1SYzBXMVBfSmpVMXJaRVJIRzM5YjVUTUxnS1BRZ3V5aGFpWkVKNkNqVnRtOXdVVGFncmdpdXFZVjJpVXFMdUY2U1lObTZTckt0a1BTLWxxSU8tdTdDMDZ3Vms1bTV1cXdJVlFOcFpTSUNfNUxzNWFMbXlaVTNuSHZILVY3RTNIbUJoVnlaQUI3NmpnS0IwVHlWWDFJT3NrdDlQREZhck50VTNzdVp5Q2p2cUMtVUpBNnNZZXlTZTRkQk5Lc0tsU1o2WXV4VVVtbjFSZ3YzMllNZEltbnNXZzhraGYtekp2cWdXazdCNUVB"
