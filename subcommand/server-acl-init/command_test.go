package serveraclinit

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hashicorp/consul/agent"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/hashicorp/consul/tlsutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	appv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var ns = "default"
var releaseName = "release-name"
var resourcePrefix = "release-name-consul"

func TestRun_FlagValidation(t *testing.T) {
	cases := []struct {
		Flags  []string
		ExpErr string
	}{
		{
			Flags:  []string{},
			ExpErr: "-release-name or -server-label-selector must be set",
		},
		{
			Flags:  []string{"-release-name=name", "-server-label-selector=hi"},
			ExpErr: "-release-name and -server-label-selector cannot both be set",
		},
		{
			Flags:  []string{"-server-label-selector=hi"},
			ExpErr: "if -server-label-selector is set -resource-prefix must also be set",
		},
		{
			Flags:  []string{"-acl-replication-token-file=/notexist", "-server-label-selector=hi", "-resource-prefix=prefix"},
			ExpErr: "Unable to read ACL replication token from file \"/notexist\": open /notexist: no such file or directory",
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
	for _, flags := range [][]string{
		{"-release-name=" + releaseName},
		{
			"-server-label-selector=component=server,app=consul,release=" + releaseName,
			"-resource-prefix=" + resourcePrefix,
		},
	} {
		t.Run(flags[0], func(t *testing.T) {
			k8s, testSvr := completeSetup(t, resourcePrefix)
			defer testSvr.Stop()
			require := require.New(t)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			args := append([]string{
				"-k8s-namespace=" + ns,
				"-expected-replicas=1",
			}, flags...)
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
		})
	}
}

// Test the different flags that should create tokens and save them as
// Kubernetes secrets. We test using the -release-name flag vs using the
// -resource-prefix flag.
func TestRun_TokensPrimaryDC(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		TokenFlag          string
		ResourcePrefixFlag string
		ReleaseNameFlag    string
		PolicyName         string
		PolicyDCs          []string
		SecretName         string
		LocalToken         bool
	}{
		"client token -release-name": {
			TokenFlag:          "-create-client-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			PolicyName:         "client-token",
			PolicyDCs:          []string{"dc1"},
			SecretName:         "release-name-consul-client-acl-token",
			LocalToken:         true,
		},
		"client token -resource-prefix": {
			TokenFlag:          "-create-client-token",
			ResourcePrefixFlag: "my-prefix",
			PolicyName:         "client-token",
			PolicyDCs:          []string{"dc1"},
			SecretName:         "my-prefix-client-acl-token",
			LocalToken:         true,
		},
		"catalog-sync token -release-name": {
			TokenFlag:          "-create-sync-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			PolicyName:         "catalog-sync-token",
			PolicyDCs:          []string{"dc1"},
			SecretName:         "release-name-consul-catalog-sync-acl-token",
			LocalToken:         true,
		},
		"catalog-sync token -resource-prefix": {
			TokenFlag:          "-create-sync-token",
			ResourcePrefixFlag: "my-prefix",
			PolicyName:         "catalog-sync-token",
			PolicyDCs:          []string{"dc1"},
			SecretName:         "my-prefix-catalog-sync-acl-token",
			LocalToken:         true,
		},
		"connect-inject-namespace token -release-name": {
			TokenFlag:          "-create-inject-namespace-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			PolicyName:         "connect-inject-token",
			PolicyDCs:          []string{"dc1"},
			SecretName:         "release-name-consul-connect-inject-acl-token",
			LocalToken:         true,
		},
		"connect-inject-namespace token -resource-prefix": {
			TokenFlag:          "-create-inject-namespace-token",
			ResourcePrefixFlag: "my-prefix",
			PolicyName:         "connect-inject-token",
			PolicyDCs:          []string{"dc1"},
			SecretName:         "my-prefix-connect-inject-acl-token",
			LocalToken:         true,
		},
		"enterprise-license token -release-name": {
			TokenFlag:          "-create-enterprise-license-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			PolicyName:         "enterprise-license-token",
			PolicyDCs:          []string{"dc1"},
			SecretName:         "release-name-consul-enterprise-license-acl-token",
			LocalToken:         true,
		},
		"enterprise-license token -resource-prefix": {
			TokenFlag:          "-create-enterprise-license-token",
			ResourcePrefixFlag: "my-prefix",
			PolicyName:         "enterprise-license-token",
			PolicyDCs:          []string{"dc1"},
			SecretName:         "my-prefix-enterprise-license-acl-token",
			LocalToken:         true,
		},
		"client-snapshot-agent token -release-name": {
			TokenFlag:          "-create-snapshot-agent-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			PolicyName:         "client-snapshot-agent-token",
			PolicyDCs:          []string{"dc1"},
			SecretName:         "release-name-consul-client-snapshot-agent-acl-token",
			LocalToken:         true,
		},
		"client-snapshot-agent token -resource-prefix": {
			TokenFlag:          "-create-snapshot-agent-token",
			ResourcePrefixFlag: "my-prefix",
			ReleaseNameFlag:    "release-name",
			PolicyName:         "client-snapshot-agent-token",
			PolicyDCs:          []string{"dc1"},
			SecretName:         "my-prefix-client-snapshot-agent-acl-token",
			LocalToken:         true,
		},
		"mesh-gateway token -release-name": {
			TokenFlag:          "-create-mesh-gateway-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			PolicyName:         "mesh-gateway-token",
			PolicyDCs:          nil,
			SecretName:         "release-name-consul-mesh-gateway-acl-token",
			LocalToken:         false,
		},
		"mesh-gateway token -resource-prefix": {
			TokenFlag:          "-create-mesh-gateway-token",
			ResourcePrefixFlag: "my-prefix",
			ReleaseNameFlag:    "release-name",
			PolicyName:         "mesh-gateway-token",
			PolicyDCs:          nil,
			SecretName:         "my-prefix-mesh-gateway-acl-token",
			LocalToken:         false,
		},
		"acl-replication token -release-name": {
			TokenFlag:          "-create-acl-replication-token",
			ResourcePrefixFlag: "",
			ReleaseNameFlag:    "release-name",
			PolicyName:         "acl-replication-token",
			PolicyDCs:          nil,
			SecretName:         "release-name-consul-acl-replication-acl-token",
			LocalToken:         false,
		},
		"acl-replication token -resource-prefix": {
			TokenFlag:          "-create-acl-replication-token",
			ResourcePrefixFlag: "my-prefix",
			ReleaseNameFlag:    "release-name",
			PolicyName:         "acl-replication-token",
			PolicyDCs:          nil,
			SecretName:         "my-prefix-acl-replication-acl-token",
			LocalToken:         false,
		},
	}
	for testName, c := range cases {
		t.Run(testName, func(t *testing.T) {
			prefix := c.ResourcePrefixFlag
			if c.ResourcePrefixFlag == "" {
				prefix = releaseName + "-consul"
			}
			k8s, testSvr := completeSetup(t, prefix)
			defer testSvr.Stop()
			require := require.New(t)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			cmdArgs := []string{
				"-k8s-namespace=" + ns,
				"-expected-replicas=1",
				c.TokenFlag,
			}
			if c.ResourcePrefixFlag != "" {
				// If using the -resource-prefix flag, we expect the -server-label-selector
				// flag to also be set.
				labelSelector := fmt.Sprintf("release=%s,component=server,app=consul", releaseName)
				cmdArgs = append(cmdArgs, "-resource-prefix="+c.ResourcePrefixFlag, "-server-label-selector="+labelSelector)
			} else {
				cmdArgs = append(cmdArgs, "-release-name="+c.ReleaseNameFlag)
			}
			responseCode := cmd.Run(cmdArgs)
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Check that the expected policy was created.
			bootToken := getBootToken(t, k8s, prefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testSvr.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(err)
			policy := policyExists(t, c.PolicyName, consul)
			require.Equal(c.PolicyDCs, policy.Datacenters)

			// Test that the token was created as a Kubernetes Secret.
			tokenSecret, err := k8s.CoreV1().Secrets(ns).Get(c.SecretName, metav1.GetOptions{})
			require.NoError(err)
			require.NotNil(tokenSecret)
			token, ok := tokenSecret.Data["token"]
			require.True(ok)

			// Test that the token has the expected policies in Consul.
			tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: string(token)})
			require.NoError(err)
			require.Equal(c.PolicyName, tokenData.Policies[0].Name)
			require.Equal(c.LocalToken, tokenData.Local)

			// Test that if the same command is run again, it doesn't error.
			t.Run(testName+"-retried", func(t *testing.T) {
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
		TokenFlag  string
		PolicyName string
		PolicyDCs  []string
		SecretName string
		LocalToken bool
	}{
		{
			TokenFlag:  "-create-client-token",
			PolicyName: "client-token-dc2",
			PolicyDCs:  []string{"dc2"},
			SecretName: "release-name-consul-client-acl-token",
			LocalToken: true,
		},
		{
			TokenFlag:  "-create-sync-token",
			PolicyName: "catalog-sync-token-dc2",
			PolicyDCs:  []string{"dc2"},
			SecretName: "release-name-consul-catalog-sync-acl-token",
			LocalToken: true,
		},
		{
			TokenFlag:  "-create-inject-namespace-token",
			PolicyName: "connect-inject-token-dc2",
			PolicyDCs:  []string{"dc2"},
			SecretName: "release-name-consul-connect-inject-acl-token",
			LocalToken: true,
		},
		{
			TokenFlag:  "-create-enterprise-license-token",
			PolicyName: "enterprise-license-token-dc2",
			PolicyDCs:  []string{"dc2"},
			SecretName: "release-name-consul-enterprise-license-acl-token",
			LocalToken: true,
		},
		{
			TokenFlag:  "-create-snapshot-agent-token",
			PolicyName: "client-snapshot-agent-token-dc2",
			PolicyDCs:  []string{"dc2"},
			SecretName: "release-name-consul-client-snapshot-agent-acl-token",
			LocalToken: true,
		},
		{
			TokenFlag:  "-create-mesh-gateway-token",
			PolicyName: "mesh-gateway-token-dc2",
			PolicyDCs:  nil,
			SecretName: "release-name-consul-mesh-gateway-acl-token",
			LocalToken: false,
		},
	}
	for _, c := range cases {
		t.Run(c.TokenFlag, func(t *testing.T) {
			bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
			tokenFile, fileCleanup := writeTempFile(t, bootToken)
			defer fileCleanup()

			k8s, consul, cleanup := mockReplicatedSetup(t, resourcePrefix, bootToken)
			defer cleanup()

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			cmdArgs := []string{
				"-k8s-namespace=" + ns,
				"-expected-replicas=1",
				"-acl-replication-token-file", tokenFile,
				"-server-label-selector=component=server,app=consul,release=" + releaseName,
				"-resource-prefix=" + resourcePrefix,
				c.TokenFlag,
			}
			responseCode := cmd.Run(cmdArgs)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			// Check that the expected policy was created.
			retry.Run(t, func(r *retry.R) {
				policy := policyExists(r, c.PolicyName, consul)
				require.Equal(r, c.PolicyDCs, policy.Datacenters)
			})

			retry.Run(t, func(r *retry.R) {
				// Test that the token was created as a Kubernetes Secret.
				tokenSecret, err := k8s.CoreV1().Secrets(ns).Get(c.SecretName, metav1.GetOptions{})
				require.NoError(r, err)
				require.NotNil(r, tokenSecret)
				token, ok := tokenSecret.Data["token"]
				require.True(r, ok)

				// Test that the token has the expected policies in Consul.
				tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: string(token)})
				require.NoError(r, err)
				require.Equal(r, c.PolicyName, tokenData.Policies[0].Name)
				require.Equal(r, c.LocalToken, tokenData.Local)
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
		"auth method, primary dc, no replication": {
			Flags:              []string{"-create-inject-auth-method"},
			SecondaryDC:        false,
			ExpAnonymousPolicy: false,
		},
		"auth method, primary dc, with replication": {
			Flags:              []string{"-create-inject-auth-method", "-create-acl-replication-token"},
			SecondaryDC:        false,
			ExpAnonymousPolicy: true,
		},
		"auth method, secondary dc": {
			Flags:              []string{"-create-inject-auth-method"},
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
				k8s, consul, cleanup = mockReplicatedSetup(t, resourcePrefix,
					bootToken)
				defer cleanup()

				tmp, err := ioutil.TempFile("", "")
				require.NoError(t, err)
				_, err = tmp.WriteString(bootToken)
				require.NoError(t, err)
				flags = append(flags, "-acl-replication-token-file", tmp.Name())
			} else {
				var testSvr *testutil.TestServer
				k8s, testSvr = completeSetup(t, resourcePrefix)
				defer testSvr.Stop()
				consulHTTPAddr = testSvr.HTTPAddr
			}
			setUpK8sServiceAccount(t, k8s)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			cmdArgs := append([]string{
				"-server-label-selector=component=server,app=consul,release=" + releaseName,
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-expected-replicas=1",
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
				require.Equal(t, "anonymous-token-policy", tokenData.Policies[0].Name)
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
		AuthMethodFlag string
	}{
		"-create-inject-token flag": {
			AuthMethodFlag: "-create-inject-token",
		},
		"-create-inject-auth-method flag": {
			AuthMethodFlag: "-create-inject-auth-method",
		},
	}
	for testName, c := range cases {
		t.Run(testName, func(tt *testing.T) {

			k8s, testSvr := completeSetup(tt, resourcePrefix)
			defer testSvr.Stop()
			caCert, jwtToken := setUpK8sServiceAccount(tt, k8s)
			require := require.New(tt)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			bindingRuleSelector := "serviceaccount.name!=default"
			cmdArgs := []string{
				"-server-label-selector=component=server,app=consul,release=" + releaseName,
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-expected-replicas=1",
				"-acl-binding-rule-selector=" + bindingRuleSelector,
			}
			cmdArgs = append(cmdArgs, c.AuthMethodFlag)
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
			require.Equal(authMethod.Config["Host"], "https://1.2.3.4:443")
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

// Test that ACL binding rules are updated if the rule selector changes.
func TestRun_BindingRuleUpdates(t *testing.T) {
	t.Parallel()
	k8s, testSvr := completeSetup(t, resourcePrefix)
	setUpK8sServiceAccount(t, k8s)
	defer testSvr.Stop()
	require := require.New(t)

	consul, err := api.NewClient(&api.Config{
		Address: testSvr.HTTPAddr,
	})
	require.NoError(err)

	ui := cli.NewMockUi()
	commonArgs := []string{
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
		"-create-inject-auth-method",
	}
	firstRunArgs := append(commonArgs,
		"-acl-binding-rule-selector=serviceaccount.name!=default",
	)
	// Our second run, we change the binding rule selector.
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
		authMethodName := releaseName + "-consul-k8s-auth-method"
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
		authMethodName := releaseName + "-consul-k8s-auth-method"
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
}

// Test that if the server pods aren't available at first that bootstrap
// still succeeds.
func TestRun_DelayedServerPods(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})

		switch r.URL.Path {
		case "/v1/agent/self":
			fmt.Fprintln(w, `{"Config": {"Datacenter": "dc1"}}`)
		default:
			// Send an empty JSON response with code 200 to all calls.
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()

	// Start the command before the Pod exist.
	// Run in a goroutine so we can create the Pods asynchronously
	done := make(chan bool)
	var responseCode int
	go func() {
		responseCode = cmd.Run([]string{
			"-server-label-selector=component=server,app=consul,release=" + releaseName,
			"-resource-prefix=" + resourcePrefix,
			"-k8s-namespace=" + ns,
			"-expected-replicas=1",
		})
		close(done)
	}()

	// Asynchronously create the server Pod after a delay.
	go func() {
		// Create the Pods after a delay between 100 and 500ms.
		// It's randomized to ensure we're not relying on specific timing.
		delay := 100 + rand.Intn(400)
		time.Sleep(time.Duration(delay) * time.Millisecond)

		pods := k8s.CoreV1().Pods(ns)
		_, err = pods.Create(&v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourcePrefix + "-server-0",
				Labels: map[string]string{
					"component": "server",
					"app":       "consul",
					"release":   releaseName,
				},
			},
			Status: v1.PodStatus{
				PodIP: serverURL.Hostname(),
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name: "consul",
						Ports: []v1.ContainerPort{
							{
								Name:          "http",
								ContainerPort: int32(port),
							},
						},
					},
				},
			},
		})
		require.NoError(err)
		_, err = k8s.AppsV1().StatefulSets(ns).Create(&appv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourcePrefix + "-server",
				Labels: map[string]string{
					"component": "server",
					"app":       "consul",
					"release":   releaseName,
				},
			},
			Status: appv1.StatefulSetStatus{
				UpdateRevision:  "current",
				CurrentRevision: "current",
			},
		})
		require.NoError(err)
	}()

	// Wait for the command to exit.
	select {
	case <-done:
		require.Equal(0, responseCode, ui.ErrorWriter.String())
	case <-time.After(2 * time.Second):
		require.FailNow("command did not exit after 2s")
	}

	// Test that the bootstrap kube secret is created.
	getBootToken(t, k8s, resourcePrefix, ns)

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

// Test that if a deployment of the statefulset is in progress we wait.
func TestRun_InProgressDeployment(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	type APICall struct {
		Method string
		Path   string
	}
	var consulAPICalls []APICall
	consulServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Record all the API calls made.
		consulAPICalls = append(consulAPICalls, APICall{
			Method: r.Method,
			Path:   r.URL.Path,
		})
		switch r.URL.Path {
		case "/v1/agent/self":
			fmt.Fprintln(w, `{"Config": {"Datacenter": "dc1"}}`)
		default:
			// Send an empty JSON response with code 200 to all calls.
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(err)

	// The pods and statefulset are created but as an in-progress deployment
	pods := k8s.CoreV1().Pods(ns)
	_, err = pods.Create(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server-0",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: v1.PodStatus{
			PodIP: serverURL.Hostname(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "consul",
					Ports: []v1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	})
	require.NoError(err)
	_, err = k8s.AppsV1().StatefulSets(ns).Create(&appv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: appv1.StatefulSetStatus{
			UpdateRevision:  "updated",
			CurrentRevision: "current",
		},
	})
	require.NoError(err)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()

	// Start the command before the Pod exist.
	// Run in a goroutine so we can create the Pods asynchronously
	done := make(chan bool)
	var responseCode int
	go func() {
		responseCode = cmd.Run([]string{
			"-server-label-selector=component=server,app=consul,release=" + releaseName,
			"-resource-prefix=" + resourcePrefix,
			"-k8s-namespace=" + ns,
			"-expected-replicas=1",
		})
		close(done)
	}()

	// Asynchronously update the deployment status after a delay.
	go func() {
		// Update after a delay between 100 and 500ms.
		// It's randomized to ensure we're not relying on specific timing.
		delay := 100 + rand.Intn(400)
		time.Sleep(time.Duration(delay) * time.Millisecond)
		_, err = k8s.AppsV1().StatefulSets(ns).Update(&appv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name: resourcePrefix + "-server",
				Labels: map[string]string{
					"component": "server",
					"app":       "consul",
					"release":   releaseName,
				},
			},
			Status: appv1.StatefulSetStatus{
				UpdateRevision:  "updated",
				CurrentRevision: "updated",
			},
		})
		require.NoError(err)
	}()

	// Wait for the command to exit.
	select {
	case <-done:
		require.Equal(0, responseCode, ui.ErrorWriter.String())
	case <-time.After(2 * time.Second):
		require.FailNow("command did not exit after 2s")
	}

	// Test that the bootstrap kube secret is created.
	getBootToken(t, k8s, resourcePrefix, ns)

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
			fmt.Fprintln(w, `{"Config": {"Datacenter": "dc1"}}`)
		default:
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()

	// Create the Server Pods.
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(err)
	pods := k8s.CoreV1().Pods(ns)
	_, err = pods.Create(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server-0",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: v1.PodStatus{
			PodIP: serverURL.Hostname(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "consul",
					Ports: []v1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	})
	require.NoError(err)
	// Create Consul server Statefulset.
	_, err = k8s.AppsV1().StatefulSets(ns).Create(&appv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: appv1.StatefulSetStatus{
			UpdateRevision:  "current",
			CurrentRevision: "current",
		},
	})

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()

	done := make(chan bool)
	var responseCode int
	go func() {
		responseCode = cmd.Run([]string{
			"-server-label-selector=component=server,app=consul,release=" + releaseName,
			"-resource-prefix=" + resourcePrefix,
			"-k8s-namespace=" + ns,
			"-expected-replicas=1",
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
			fmt.Fprintln(w, `{"Config": {"Datacenter": "dc1"}}`)
		default:
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()

	// Create the Server Pods.
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(err)
	pods := k8s.CoreV1().Pods(ns)
	_, err = pods.Create(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server-0",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: v1.PodStatus{
			PodIP: serverURL.Hostname(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "consul",
					Ports: []v1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	})
	require.NoError(err)
	// Create the server statefulset.
	_, err = k8s.AppsV1().StatefulSets(ns).Create(&appv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: appv1.StatefulSetStatus{
			UpdateRevision:  "current",
			CurrentRevision: "current",
		},
	})
	require.NoError(err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	responseCode := cmd.Run([]string{
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
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

// Test if there is an old bootstrap Secret we assume the servers were
// bootstrapped already and continue on to the next step.
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
			fmt.Fprintln(w, `{"Config": {"Datacenter": "dc1"}}`)
		default:
			// Send an empty JSON response with code 200 to all calls.
			fmt.Fprintln(w, "{}")
		}
	}))
	defer consulServer.Close()

	// Create the Server Pods.
	serverURL, err := url.Parse(consulServer.URL)
	require.NoError(err)
	port, err := strconv.Atoi(serverURL.Port())
	require.NoError(err)
	pods := k8s.CoreV1().Pods(ns)
	_, err = pods.Create(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server-0",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: v1.PodStatus{
			PodIP: serverURL.Hostname(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "consul",
					Ports: []v1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	})
	require.NoError(err)
	// Create the server statefulset.
	_, err = k8s.AppsV1().StatefulSets(ns).Create(&appv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-server",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: appv1.StatefulSetStatus{
			UpdateRevision:  "current",
			CurrentRevision: "current",
		},
	})
	require.NoError(err)

	// Create the bootstrap secret.
	_, err = k8s.CoreV1().Secrets(ns).Create(&v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-bootstrap-acl-token",
		},
		Data: map[string][]byte{
			"token": []byte("old-token"),
		},
	})
	require.NoError(err)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	responseCode := cmd.Run([]string{
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the Secret is the same.
	secret, err := k8s.CoreV1().Secrets(ns).Get(resourcePrefix+"-bootstrap-acl-token", metav1.GetOptions{})
	require.NoError(err)
	require.Contains(secret.Data, "token")
	require.Equal("old-token", string(secret.Data["token"]))

	// Test that the expected API calls were made.
	require.Equal([]APICall{
		// We only expect the calls for creating client tokens
		// and updating the server policy.
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
			"/v1/acl/policy",
		},
		{
			"PUT",
			"/v1/acl/token",
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
	cmd.init()
	responseCode := cmd.Run([]string{
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
		"-timeout=500ms",
	})
	require.Equal(1, responseCode, ui.ErrorWriter.String())
}

// Test that the bootstrapping process can make calls to Consul API over HTTPS
// when the consul agent is configured with HTTPS only (HTTP disabled).
func TestRun_HTTPS(t *testing.T) {
	t.Parallel()
	require := require.New(t)
	k8s := fake.NewSimpleClientset()

	caFile, certFile, keyFile, cleanup := generateServerCerts(t)
	defer cleanup()

	agentConfig := fmt.Sprintf(`
		primary_datacenter = "dc1"
		acl {
			enabled = true
		}
		ca_file = "%s"
		cert_file = "%s"
		key_file = "%s"`, caFile, certFile, keyFile)

	// NOTE: We can't use testutil.TestServer for this test because the HTTP
	// port can't be disabled (causes a seg fault).
	a := &agent.TestAgent{
		Name:   t.Name(),
		HCL:    agentConfig,
		UseTLS: true, // this also disables HTTP port
	}

	a.Start()
	defer a.Shutdown()

	createTestK8SResources(t, k8s, a.HTTPAddr(), resourcePrefix, "https", ns)

	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	responseCode := cmd.Run([]string{
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-use-https",
		"-consul-tls-server-name", "server.dc1.consul",
		"-consul-ca-cert", caFile,
		"-expected-replicas=1",
	})
	require.Equal(0, responseCode, ui.ErrorWriter.String())

	// Test that the bootstrap token is created to make sure the bootstrapping succeeded.
	// The presence of the bootstrap token tells us that the API calls to Consul have been successful.
	tokenSecret, err := k8s.CoreV1().Secrets(ns).Get(resourcePrefix+"-bootstrap-acl-token", metav1.GetOptions{})
	require.NoError(err)
	require.NotNil(tokenSecret)
	_, ok := tokenSecret.Data["token"]
	require.True(ok)
}

// Test that the ACL replication token created from the primary DC can be used
// for replication in the secondary DC.
func TestRun_ACLReplicationTokenValid(t *testing.T) {
	t.Parallel()

	secondaryK8s, secondaryConsulClient, aclReplicationToken, clean := completeReplicatedSetup(t, resourcePrefix)
	defer clean()

	// completeReplicatedSetup ran the command in our primary dc so now we
	// need to run the command in our secondary dc.
	tokenFile, cleanup := writeTempFile(t, aclReplicationToken)
	defer cleanup()
	secondaryUI := cli.NewMockUi()
	secondaryCmd := Command{
		UI:        secondaryUI,
		clientset: secondaryK8s,
	}
	secondaryCmd.init()
	secondaryCmdArgs := []string{
		"-k8s-namespace=" + ns,
		"-expected-replicas=1",
		"-server-label-selector=component=server,app=consul,release=" + releaseName,
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
			tokenFile, fileCleanup := writeTempFile(t, bootToken)
			defer fileCleanup()
			k8s, consul, cleanup := mockReplicatedSetup(t, resourcePrefix, bootToken)
			setUpK8sServiceAccount(t, k8s)
			defer cleanup()

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			cmdArgs := append([]string{
				"-k8s-namespace=" + ns,
				"-expected-replicas=1",
				"-acl-replication-token-file", tokenFile,
				"-server-label-selector=component=server,app=consul,release=" + releaseName,
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

// Set up test consul agent and kubernetes cluster.
func completeSetup(t *testing.T, prefix string) (*fake.Clientset, *testutil.TestServer) {
	k8s := fake.NewSimpleClientset()

	svr, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
	})
	require.NoError(t, err)

	createTestK8SResources(t, k8s, svr.HTTPAddr, prefix, "http", ns)

	return k8s, svr
}

// completeReplicatedSetup sets up two Consul servers with ACL replication
// using the server-acl-init command to start the replication.
// Returns the Kubernetes client for the secondary DC,
// a Consul API client initialized for the secondary DC, the replication token
// generated and a cleanup function that should be called at the end of the
// test that cleans up resources.
func completeReplicatedSetup(t *testing.T, prefix string) (*fake.Clientset, *api.Client, string, func()) {
	return replicatedSetup(t, prefix, "")
}

// mockReplicatedSetup sets up two Consul servers with ACL replication.
// It's a mock setup because we don't run the server-acl-init
// command to set up replication but do it in config using the bootstrap
// token. See completeReplicatedSetup for a complete setup using the command.
// Returns the Kubernetes client for the secondary DC,
// a Consul API client initialized for the secondary DC and a
// cleanup function that should be called at the end of the test that cleans
// up resources.
func mockReplicatedSetup(t *testing.T, prefix string, bootToken string) (*fake.Clientset, *api.Client, func()) {
	k8sClient, consulClient, _, cleanup := replicatedSetup(t, prefix, bootToken)
	return k8sClient, consulClient, cleanup
}

// replicatedSetup is a helper function for completeReplicatedSetup and
// mockReplicatedSetup. If bootToken is empty, it will run the server-acl-init
// command to set up replication. Otherwise it will do it through config.
func replicatedSetup(t *testing.T, prefix string, bootToken string) (*fake.Clientset, *api.Client, string, func()) {
	primarySvr, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		if bootToken != "" {
			c.ACL.Tokens.Master = bootToken
		}
	})
	require.NoError(t, err)

	var aclReplicationToken string
	if bootToken == "" {
		primaryK8s := fake.NewSimpleClientset()
		createTestK8SResources(t, primaryK8s, primarySvr.HTTPAddr, resourcePrefix, "http", ns)
		require.NoError(t, err)

		// Run the command to bootstrap ACLs
		primaryUI := cli.NewMockUi()
		primaryCmd := Command{
			UI:        primaryUI,
			clientset: primaryK8s,
		}
		primaryCmd.init()
		primaryCmdArgs := []string{
			"-k8s-namespace=" + ns,
			"-expected-replicas=1",
			"-server-label-selector=component=server,app=consul,release=" + releaseName,
			"-resource-prefix=" + resourcePrefix,
			"-create-acl-replication-token",
		}
		responseCode := primaryCmd.Run(primaryCmdArgs)
		require.Equal(t, 0, responseCode, primaryUI.ErrorWriter.String())

		// Retrieve the replication ACL token from the kubernetes secret.
		tokenSecret, err := primaryK8s.CoreV1().Secrets(ns).Get("release-name-consul-acl-replication-acl-token", metav1.GetOptions{})
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
	consul, err := api.NewClient(&api.Config{
		Address: secondarySvr.HTTPAddr,
		Token:   clientToken,
	})
	require.NoError(t, err)

	// WAN join the primary and secondary.
	err = consul.Agent().Join(primarySvr.WANAddr, true)
	require.NoError(t, err)

	// Finally, set up our kube cluster. It will use the secondary dc.
	k8s := fake.NewSimpleClientset()
	createTestK8SResources(t, k8s, secondarySvr.HTTPAddr, prefix, "http", ns)

	return k8s, consul, aclReplicationToken, func() {
		primarySvr.Stop()
		secondarySvr.Stop()
	}
}

// Create test k8s resources (server pods and server stateful set)
func createTestK8SResources(t *testing.T, k8s *fake.Clientset, consulHTTPAddr, prefix, scheme, k8sNamespace string) {
	require := require.New(t)
	consulURL, err := url.Parse("http://" + consulHTTPAddr)
	require.NoError(err)
	port, err := strconv.Atoi(consulURL.Port())
	require.NoError(err)

	// Create Consul server Pod.
	_, err = k8s.CoreV1().Pods(k8sNamespace).Create(&v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: prefix + "-server-0",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: v1.PodStatus{
			PodIP: consulURL.Hostname(),
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name: "consul",
					Ports: []v1.ContainerPort{
						{
							Name:          scheme,
							ContainerPort: int32(port),
						},
					},
				},
			},
		},
	})
	require.NoError(err)

	// Create Consul server Statefulset.
	_, err = k8s.AppsV1().StatefulSets(k8sNamespace).Create(&appv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: prefix + "-server",
			Labels: map[string]string{
				"component": "server",
				"app":       "consul",
				"release":   releaseName,
			},
		},
		Status: appv1.StatefulSetStatus{
			UpdateRevision:  "current",
			CurrentRevision: "current",
		},
	})
	require.NoError(err)
}

// getBootToken gets the bootstrap token from the Kubernetes secret. It will
// cause a test failure if the Secret doesn't exist or is malformed.
func getBootToken(t *testing.T, k8s *fake.Clientset, prefix string, k8sNamespace string) string {
	bootstrapSecret, err := k8s.CoreV1().Secrets(k8sNamespace).Get(fmt.Sprintf("%s-bootstrap-acl-token", prefix), metav1.GetOptions{})
	require.NoError(t, err)
	require.NotNil(t, bootstrapSecret)
	bootToken, ok := bootstrapSecret.Data["token"]
	require.True(t, ok)
	return string(bootToken)
}

// generateServerCerts generates Consul CA
// and a server certificate and saves them to temp files.
// It returns file names in this order:
// CA certificate, server certificate, and server key.
// Note that it's the responsibility of the caller to
// remove the temporary files created by this function.
func generateServerCerts(t *testing.T) (string, string, string, func()) {
	require := require.New(t)

	caFile, err := ioutil.TempFile("", "ca")
	require.NoError(err)

	certFile, err := ioutil.TempFile("", "cert")
	require.NoError(err)

	certKeyFile, err := ioutil.TempFile("", "key")
	require.NoError(err)

	// Generate CA
	sn, err := tlsutil.GenerateSerialNumber()
	require.NoError(err)

	s, _, err := tlsutil.GeneratePrivateKey()
	require.NoError(err)

	constraints := []string{"consul", "localhost"}
	ca, err := tlsutil.GenerateCA(s, sn, 1, constraints)
	require.NoError(err)

	// Generate Server Cert
	name := fmt.Sprintf("server.%s.%s", "dc1", "consul")
	DNSNames := []string{name, "localhost"}
	IPAddresses := []net.IP{net.ParseIP("127.0.0.1")}
	extKeyUsage := []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}

	sn, err = tlsutil.GenerateSerialNumber()
	require.NoError(err)

	pub, priv, err := tlsutil.GenerateCert(s, ca, sn, name, 1, DNSNames, IPAddresses, extKeyUsage)
	require.NoError(err)

	// Write certs and key to files
	_, err = caFile.WriteString(ca)
	require.NoError(err)
	_, err = certFile.WriteString(pub)
	require.NoError(err)
	_, err = certKeyFile.WriteString(priv)
	require.NoError(err)

	cleanupFunc := func() {
		os.Remove(caFile.Name())
		os.Remove(certFile.Name())
		os.Remove(certKeyFile.Name())
	}
	return caFile.Name(), certFile.Name(), certKeyFile.Name(), cleanupFunc
}

// setUpK8sServiceAccount creates a Service Account for the connect injector.
// This Service Account would normally automatically be created by Kubernetes
// when the injector deployment is created. It returns the Service Account
// CA Cert and JWT token.
func setUpK8sServiceAccount(t *testing.T, k8s *fake.Clientset) (string, string) {
	// Create Kubernetes Service.
	_, err := k8s.CoreV1().Services(ns).Create(&v1.Service{
		Spec: v1.ServiceSpec{
			ClusterIP: "1.2.3.4",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "kubernetes",
		},
	})
	require.NoError(t, err)

	// Create ServiceAccount for the injector that the helm chart creates.
	_, err = k8s.CoreV1().ServiceAccounts(ns).Create(&v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-connect-injector-authmethod-svc-account",
		},
		Secrets: []v1.ObjectReference{
			{
				Name: resourcePrefix + "-connect-injector-authmethod-svc-account",
			},
		},
	})
	require.NoError(t, err)

	// Create the ServiceAccount Secret.
	caCertBytes, err := base64.StdEncoding.DecodeString(serviceAccountCACert)
	require.NoError(t, err)
	tokenBytes, err := base64.StdEncoding.DecodeString(serviceAccountToken)
	require.NoError(t, err)
	_, err = k8s.CoreV1().Secrets(ns).Create(&v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourcePrefix + "-connect-injector-authmethod-svc-account",
		},
		Data: map[string][]byte{
			"ca.crt": caCertBytes,
			"token":  tokenBytes,
		},
	})
	require.NoError(t, err)
	return string(caCertBytes), string(tokenBytes)
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

func writeTempFile(t *testing.T, contents string) (string, func()) {
	t.Helper()
	file, err := ioutil.TempFile("", "")
	require.NoError(t, err)
	_, err = file.WriteString(contents)
	require.NoError(t, err)
	return file.Name(), func() {
		os.Remove(file.Name())
	}
}

var serviceAccountCACert = "LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSURDekNDQWZPZ0F3SUJBZ0lRS3pzN05qbDlIczZYYzhFWG91MjVoekFOQmdrcWhraUc5dzBCQVFzRkFEQXYKTVMwd0t3WURWUVFERXlRMU9XVTJaR00wTVMweU1EaG1MVFF3T1RVdFlUSTRPUzB4Wm1NM01EQmhZekZqWXpndwpIaGNOTVRrd05qQTNNVEF4TnpNeFdoY05NalF3TmpBMU1URXhOek14V2pBdk1TMHdLd1lEVlFRREV5UTFPV1UyClpHTTBNUzB5TURobUxUUXdPVFV0WVRJNE9TMHhabU0zTURCaFl6RmpZemd3Z2dFaU1BMEdDU3FHU0liM0RRRUIKQVFVQUE0SUJEd0F3Z2dFS0FvSUJBUURaakh6d3FvZnpUcEdwYzBNZElDUzdldXZmdWpVS0UzUEMvYXBmREFnQgo0anpFRktBNzgvOStLVUd3L2MvMFNIZVNRaE4rYThnd2xIUm5BejFOSmNmT0lYeTRkd2VVdU9rQWlGeEg4cGh0CkVDd2tlTk83ejhEb1Y4Y2VtaW5DUkhHamFSbW9NeHBaN2cycFpBSk5aZVB4aTN5MWFOa0ZBWGU5Z1NVU2RqUloKUlhZa2E3d2gyQU85azJkbEdGQVlCK3Qzdld3SjZ0d2pHMFR0S1FyaFlNOU9kMS9vTjBFMDFMekJjWnV4a04xawo4Z2ZJSHk3Yk9GQ0JNMldURURXLzBhQXZjQVByTzhETHFESis2TWpjM3I3K3psemw4YVFzcGIwUzA4cFZ6a2k1CkR6Ly84M2t5dTBwaEp1aWo1ZUI4OFY3VWZQWHhYRi9FdFY2ZnZyTDdNTjRmQWdNQkFBR2pJekFoTUE0R0ExVWQKRHdFQi93UUVBd0lDQkRBUEJnTlZIUk1CQWY4RUJUQURBUUgvTUEwR0NTcUdTSWIzRFFFQkN3VUFBNElCQVFCdgpRc2FHNnFsY2FSa3RKMHpHaHh4SjUyTm5SVjJHY0lZUGVOM1p2MlZYZTNNTDNWZDZHMzJQVjdsSU9oangzS21BCi91TWg2TmhxQnpzZWtrVHowUHVDM3dKeU0yT0dvblZRaXNGbHF4OXNGUTNmVTJtSUdYQ2Ezd0M4ZS9xUDhCSFMKdzcvVmVBN2x6bWozVFFSRS9XMFUwWkdlb0F4bjliNkp0VDBpTXVjWXZQMGhYS1RQQldsbnpJaWphbVU1MHIyWQo3aWEwNjVVZzJ4VU41RkxYL3Z4T0EzeTRyanBraldvVlFjdTFwOFRaclZvTTNkc0dGV3AxMGZETVJpQUhUdk9ICloyM2pHdWs2cm45RFVIQzJ4UGozd0NUbWQ4U0dFSm9WMzFub0pWNWRWZVE5MHd1c1h6M3ZURzdmaWNLbnZIRlMKeHRyNVBTd0gxRHVzWWZWYUdIMk8KLS0tLS1FTkQgQ0VSVElGSUNBVEUtLS0tLQo="
var serviceAccountToken = "ZXlKaGJHY2lPaUpTVXpJMU5pSXNJbXRwWkNJNklpSjkuZXlKcGMzTWlPaUpyZFdKbGNtNWxkR1Z6TDNObGNuWnBZMlZoWTJOdmRXNTBJaXdpYTNWaVpYSnVaWFJsY3k1cGJ5OXpaWEoyYVdObFlXTmpiM1Z1ZEM5dVlXMWxjM0JoWTJVaU9pSmtaV1poZFd4MElpd2lhM1ZpWlhKdVpYUmxjeTVwYnk5elpYSjJhV05sWVdOamIzVnVkQzl6WldOeVpYUXVibUZ0WlNJNkltdG9ZV3RwTFdGeVlXTm9ibWxrTFdOdmJuTjFiQzFqYjI1dVpXTjBMV2x1YW1WamRHOXlMV0YxZEdodFpYUm9iMlF0YzNaakxXRmpZMjlvYm1SaWRpSXNJbXQxWW1WeWJtVjBaWE11YVc4dmMyVnlkbWxqWldGalkyOTFiblF2YzJWeWRtbGpaUzFoWTJOdmRXNTBMbTVoYldVaU9pSnJhR0ZyYVMxaGNtRmphRzVwWkMxamIyNXpkV3d0WTI5dWJtVmpkQzFwYm1wbFkzUnZjaTFoZFhSb2JXVjBhRzlrTFhOMll5MWhZMk52ZFc1MElpd2lhM1ZpWlhKdVpYUmxjeTVwYnk5elpYSjJhV05sWVdOamIzVnVkQzl6WlhKMmFXTmxMV0ZqWTI5MWJuUXVkV2xrSWpvaU4yVTVOV1V4TWprdFpUUTNNeTB4TVdVNUxUaG1ZV0V0TkRJd01UQmhPREF3TVRJeUlpd2ljM1ZpSWpvaWMzbHpkR1Z0T25ObGNuWnBZMlZoWTJOdmRXNTBPbVJsWm1GMWJIUTZhMmhoYTJrdFlYSmhZMmh1YVdRdFkyOXVjM1ZzTFdOdmJtNWxZM1F0YVc1cVpXTjBiM0l0WVhWMGFHMWxkR2h2WkMxemRtTXRZV05qYjNWdWRDSjkuWWk2M01NdHpoNU1CV0tLZDNhN2R6Q0pqVElURTE1aWtGeV9UbnBka19Bd2R3QTlKNEFNU0dFZUhONXZXdEN1dUZqb19sTUpxQkJQSGtLMkFxYm5vRlVqOW01Q29wV3lxSUNKUWx2RU9QNGZVUS1SYzBXMVBfSmpVMXJaRVJIRzM5YjVUTUxnS1BRZ3V5aGFpWkVKNkNqVnRtOXdVVGFncmdpdXFZVjJpVXFMdUY2U1lObTZTckt0a1BTLWxxSU8tdTdDMDZ3Vms1bTV1cXdJVlFOcFpTSUNfNUxzNWFMbXlaVTNuSHZILVY3RTNIbUJoVnlaQUI3NmpnS0IwVHlWWDFJT3NrdDlQREZhck50VTNzdVp5Q2p2cUMtVUpBNnNZZXlTZTRkQk5Lc0tsU1o2WXV4VVVtbjFSZ3YzMllNZEltbnNXZzhraGYtekp2cWdXazdCNUVB"
