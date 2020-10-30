// +build enterprise

package serveraclinit

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

// Test the auth method and acl binding rule created when namespaces are enabled
// and there's a single consul destination namespace.
func TestRun_ConnectInject_SingleDestinationNamespace(t *testing.T) {
	t.Parallel()

	consulDestNamespaces := []string{"default", "destination"}
	for _, consulDestNamespace := range consulDestNamespaces {
		t.Run(consulDestNamespace, func(tt *testing.T) {
			k8s, testAgent := completeEnterpriseSetup(tt)
			defer testAgent.Stop()
			setUpK8sServiceAccount(tt, k8s, ns)
			require := require.New(tt)

			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			args := []string{
				"-server-address=" + strings.Split(testAgent.HTTPAddr, ":")[0],
				"-server-port=" + strings.Split(testAgent.HTTPAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-create-inject-token",
				"-enable-namespaces",
				"-consul-inject-destination-namespace", consulDestNamespace,
				"-acl-binding-rule-selector=serviceaccount.name!=default",
			}

			responseCode := cmd.Run(args)
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			bootToken := getBootToken(t, k8s, resourcePrefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testAgent.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(err)

			// Ensure there's only one auth method.
			namespaceQuery := &api.QueryOptions{
				Namespace: consulDestNamespace,
			}
			methods, _, err := consul.ACL().AuthMethodList(namespaceQuery)
			require.NoError(err)
			require.Len(methods, 1)

			// Check the ACL auth method is created in the expected namespace.
			authMethodName := resourcePrefix + "-k8s-auth-method"
			actMethod, _, err := consul.ACL().AuthMethodRead(authMethodName, namespaceQuery)
			require.NoError(err)
			require.NotNil(actMethod)
			require.Equal("kubernetes", actMethod.Type)
			require.Equal("Kubernetes Auth Method", actMethod.Description)
			require.NotContains(actMethod.Config, "MapNamespaces")
			require.NotContains(actMethod.Config, "ConsulNamespacePrefix")

			// Check the binding rule is as expected.
			rules, _, err := consul.ACL().BindingRuleList(authMethodName, namespaceQuery)
			require.NoError(err)
			require.Len(rules, 1)
			actRule, _, err := consul.ACL().BindingRuleRead(rules[0].ID, namespaceQuery)
			require.NoError(err)
			require.NotNil(actRule)
			require.Equal("Kubernetes binding rule", actRule.Description)
			require.Equal(api.BindingRuleBindTypeService, actRule.BindType)
			require.Equal("${serviceaccount.name}", actRule.BindName)
			require.Equal("serviceaccount.name!=default", actRule.Selector)

			// Check that the default namespace got an attached ACL policy
			defNamespace, _, err := consul.Namespaces().Read("default", &api.QueryOptions{})
			require.NoError(err)
			require.NotNil(defNamespace)
			require.NotNil(defNamespace.ACLs)
			require.Len(defNamespace.ACLs.PolicyDefaults, 1)
			require.Equal("cross-namespace-policy", defNamespace.ACLs.PolicyDefaults[0].Name)

			if consulDestNamespace != "default" {
				// Check that only one namespace was created besides the
				// already existing `default` namespace
				namespaces, _, err := consul.Namespaces().List(&api.QueryOptions{})
				require.NoError(err)
				require.Len(namespaces, 2)

				// Check the created namespace properties
				actNamespace, _, err := consul.Namespaces().Read(consulDestNamespace, &api.QueryOptions{})
				require.NoError(err)
				require.NotNil(actNamespace)
				require.Equal(consulDestNamespace, actNamespace.Name)
				require.Equal("Auto-generated by consul-k8s", actNamespace.Description)
				require.NotNil(actNamespace.ACLs)
				require.Len(actNamespace.ACLs.PolicyDefaults, 1)
				require.Equal("cross-namespace-policy", actNamespace.ACLs.PolicyDefaults[0].Name)
				require.Contains(actNamespace.Meta, "external-source")
				require.Equal("kubernetes", actNamespace.Meta["external-source"])
			}
		})
	}
}

// Test the auth method and acl binding rule created when namespaces are enabled
// and we're mirroring namespaces.
func TestRun_ConnectInject_NamespaceMirroring(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		MirroringPrefix string
		ExtraFlags      []string
	}{
		"no prefix": {
			MirroringPrefix: "",
			ExtraFlags:      nil,
		},
		"with prefix": {
			MirroringPrefix: "prefix-",
			ExtraFlags:      nil,
		},
		"with destination namespace flag": {
			MirroringPrefix: "",
			// Mirroring takes precedence over this flag so it should have no
			// effect.
			ExtraFlags: []string{"-consul-inject-destination-namespace=dest"},
		},
	}

	for name, c := range cases {
		tt.Run(name, func(t *testing.T) {
			k8s, testAgent := completeEnterpriseSetup(t)
			defer testAgent.Stop()
			setUpK8sServiceAccount(t, k8s, ns)
			require := require.New(t)

			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			args := []string{
				"-server-address=" + strings.Split(testAgent.HTTPAddr, ":")[0],
				"-server-port=" + strings.Split(testAgent.HTTPAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-create-inject-token",
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix", c.MirroringPrefix,
				"-acl-binding-rule-selector=serviceaccount.name!=default",
			}
			args = append(args, c.ExtraFlags...)
			responseCode := cmd.Run(args)
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			bootToken := getBootToken(t, k8s, resourcePrefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testAgent.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(err)

			// Check the ACL auth method is as expected.
			authMethodName := resourcePrefix + "-k8s-auth-method"
			method, _, err := consul.ACL().AuthMethodRead(authMethodName, nil)
			require.NoError(err)
			require.NotNil(method, authMethodName+" not found")
			require.Equal("kubernetes", method.Type)
			require.Equal("Kubernetes Auth Method", method.Description)
			require.Contains(method.Config, "MapNamespaces")
			require.Contains(method.Config, "ConsulNamespacePrefix")
			require.Equal(true, method.Config["MapNamespaces"])
			require.Equal(c.MirroringPrefix, method.Config["ConsulNamespacePrefix"])

			// Check the binding rule is as expected.
			rules, _, err := consul.ACL().BindingRuleList(authMethodName, nil)
			require.NoError(err)
			require.Len(rules, 1)
			actRule, _, err := consul.ACL().BindingRuleRead(rules[0].ID, nil)
			require.NoError(err)
			require.NotNil(actRule)
			require.Equal("Kubernetes binding rule", actRule.Description)
			require.Equal(api.BindingRuleBindTypeService, actRule.BindType)
			require.Equal("${serviceaccount.name}", actRule.BindName)
			require.Equal("serviceaccount.name!=default", actRule.Selector)
		})
	}
}

// Test that ACL policies get updated if namespaces config changes.
func TestRun_ACLPolicyUpdates(tt *testing.T) {
	tt.Parallel()

	k8sNamespaceFlags := []string{"default", "other"}
	for _, k8sNamespaceFlag := range k8sNamespaceFlags {
		tt.Run(k8sNamespaceFlag, func(t *testing.T) {
			k8s, testAgent := completeEnterpriseSetup(t)
			setUpK8sServiceAccount(t, k8s, k8sNamespaceFlag)
			defer testAgent.Stop()
			require := require.New(t)

			ui := cli.NewMockUi()
			firstRunArgs := []string{
				"-server-address=" + strings.Split(testAgent.HTTPAddr, ":")[0],
				"-server-port=" + strings.Split(testAgent.HTTPAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace", k8sNamespaceFlag,
				"-create-client-token",
				"-allow-dns",
				"-create-mesh-gateway-token",
				"-create-sync-token",
				"-create-inject-token",
				"-create-snapshot-agent-token",
				"-create-enterprise-license-token",
				"-ingress-gateway-name=gw",
				"-ingress-gateway-name=anothergw",
				"-terminating-gateway-name=gw",
				"-terminating-gateway-name=anothergw",
			}
			// Our second run, we're going to update from namespaces disabled to
			// namespaces enabled with a single destination ns.
			secondRunArgs := append(firstRunArgs,
				"-enable-namespaces",
				"-consul-sync-destination-namespace=sync",
				"-consul-inject-destination-namespace=dest")

			// Run the command first to populate the policies.
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			responseCode := cmd.Run(firstRunArgs)
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			bootToken := getBootToken(t, k8s, resourcePrefix, k8sNamespaceFlag)
			consul, err := api.NewClient(&api.Config{
				Address: testAgent.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(err)

			// Check that the expected policies were created.
			firstRunExpectedPolicies := []string{
				"anonymous-token-policy",
				"client-token",
				"catalog-sync-token",
				"mesh-gateway-token",
				"client-snapshot-agent-token",
				"enterprise-license-token",
				"gw-ingress-gateway-token",
				"anothergw-ingress-gateway-token",
				"gw-terminating-gateway-token",
				"anothergw-terminating-gateway-token",
			}
			policies, _, err := consul.ACL().PolicyList(nil)
			require.NoError(err)

			// Check that we have the right number of policies. The actual
			// policies will have two more than expected because of the
			// global management and namespace management polices that
			// are automatically created, the latter in consul-ent v1.7+.
			require.Equal(len(firstRunExpectedPolicies), len(policies)-2)

			// Collect the actual policies into a map to make it easier to assert
			// on their existence and contents.
			actualPolicies := make(map[string]string)
			for _, p := range policies {
				policy, _, err := consul.ACL().PolicyRead(p.ID, nil)
				require.NoError(err)
				actualPolicies[p.Name] = policy.Rules
			}
			for _, expected := range firstRunExpectedPolicies {
				actRules, ok := actualPolicies[expected]
				require.True(ok, "Did not find policy %s", expected)
				// We assert that the policy doesn't have any namespace config
				// in it because later that's what we're using to test that it
				// got updated.
				require.NotContains(actRules, "namespace")
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

			// Check that the policies have all been updated.
			secondRunExpectedPolicies := []string{
				"anonymous-token-policy",
				"client-token",
				"catalog-sync-token",
				"connect-inject-token",
				"mesh-gateway-token",
				"client-snapshot-agent-token",
				"enterprise-license-token",
				"cross-namespace-policy",
				"gw-ingress-gateway-token",
				"anothergw-ingress-gateway-token",
				"gw-terminating-gateway-token",
				"anothergw-terminating-gateway-token",
			}
			policies, _, err = consul.ACL().PolicyList(nil)
			require.NoError(err)

			// Check that we have the right number of policies. The actual
			// policies will have two more than expected because of the
			// global management and namespace management polices that
			// are automatically created, the latter in consul-ent v1.7+.
			require.Equal(len(secondRunExpectedPolicies), len(policies)-2)

			// Collect the actual policies into a map to make it easier to assert
			// on their existence and contents.
			actualPolicies = make(map[string]string)
			for _, p := range policies {
				policy, _, err := consul.ACL().PolicyRead(p.ID, nil)
				require.NoError(err)
				actualPolicies[p.Name] = policy.Rules
			}
			for _, expected := range secondRunExpectedPolicies {
				actRules, ok := actualPolicies[expected]
				require.True(ok, "Did not find policy %s", expected)

				switch expected {
				case "connect-inject-token":
					// The connect inject token doesn't have namespace config,
					// but does change to operator:write from an empty string.
					require.Contains(actRules, "operator = \"write\"")
				case "client-snapshot-agent-token", "enterprise-license-token":
					// The snapshot agent and enterprise license tokens shouldn't change.
					require.NotContains(actRules, "namespace")
				default:
					// Assert that the policies have the word namespace in them. This
					// tests that they were updated. The actual contents are tested
					// in rules_test.go.
					require.Contains(actRules, "namespace")
				}
			}
		})
	}
}

// Test that re-running the commands results in auth method and binding rules
// being updated.
func TestRun_ConnectInject_Updates(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		// Args for first run of command.
		FirstRunArgs []string
		// Args for second run of command.
		SecondRunArgs []string
		// Expected namespace for the auth method.
		AuthMethodExpectedNS string
		// If true, we expect MapNamespaces to be set on the auth method
		// config.
		AuthMethodExpectMapNamespacesConfig bool
		// If AuthMethodExpectMapNamespacesConfig is true, we will assert
		// that the ConsulNamespacePrefix field on the auth method config
		// is set to this.
		AuthMethodExpectedNamespacePrefixConfig string
		// Expected namespace for the binding rule.
		BindingRuleExpectedNS string
	}{
		"no ns => mirroring ns, no prefix": {
			FirstRunArgs: nil,
			SecondRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
			},
			AuthMethodExpectedNS:                    "default",
			AuthMethodExpectMapNamespacesConfig:     true,
			AuthMethodExpectedNamespacePrefixConfig: "",
			BindingRuleExpectedNS:                   "default",
		},
		"no ns => mirroring ns, prefix": {
			FirstRunArgs: nil,
			SecondRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix=prefix-",
			},
			AuthMethodExpectedNS:                    "default",
			AuthMethodExpectMapNamespacesConfig:     true,
			AuthMethodExpectedNamespacePrefixConfig: "prefix-",
			BindingRuleExpectedNS:                   "default",
		},
		"no ns => single dest ns": {
			FirstRunArgs: nil,
			SecondRunArgs: []string{
				"-enable-namespaces",
				"-consul-inject-destination-namespace=dest",
			},
			AuthMethodExpectedNS:                    "dest",
			AuthMethodExpectMapNamespacesConfig:     false,
			AuthMethodExpectedNamespacePrefixConfig: "",
			BindingRuleExpectedNS:                   "dest",
		},
		"mirroring ns => single dest ns": {
			FirstRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix=prefix-",
			},
			SecondRunArgs: []string{
				"-enable-namespaces",
				"-consul-inject-destination-namespace=dest",
			},
			AuthMethodExpectedNS:                    "dest",
			AuthMethodExpectMapNamespacesConfig:     false,
			AuthMethodExpectedNamespacePrefixConfig: "",
			BindingRuleExpectedNS:                   "dest",
		},
		"single dest ns => mirroring ns": {
			FirstRunArgs: []string{
				"-enable-namespaces",
				"-consul-inject-destination-namespace=dest",
			},
			SecondRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix=prefix-",
			},
			AuthMethodExpectedNS:                    "default",
			AuthMethodExpectMapNamespacesConfig:     true,
			AuthMethodExpectedNamespacePrefixConfig: "prefix-",
			BindingRuleExpectedNS:                   "default",
		},
		"mirroring ns (no prefix) => mirroring ns (no prefix)": {
			FirstRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix=",
			},
			SecondRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix=",
			},
			AuthMethodExpectedNS:                    "default",
			AuthMethodExpectMapNamespacesConfig:     true,
			AuthMethodExpectedNamespacePrefixConfig: "",
			BindingRuleExpectedNS:                   "default",
		},
		"mirroring ns => mirroring ns (same prefix)": {
			FirstRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix=prefix-",
			},
			SecondRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix=prefix-",
			},
			AuthMethodExpectedNS:                    "default",
			AuthMethodExpectMapNamespacesConfig:     true,
			AuthMethodExpectedNamespacePrefixConfig: "prefix-",
			BindingRuleExpectedNS:                   "default",
		},
		"mirroring ns (no prefix) => mirroring ns (prefix)": {
			FirstRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix=",
			},
			SecondRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix=prefix-",
			},
			AuthMethodExpectedNS:                    "default",
			AuthMethodExpectMapNamespacesConfig:     true,
			AuthMethodExpectedNamespacePrefixConfig: "prefix-",
			BindingRuleExpectedNS:                   "default",
		},
		"mirroring ns (prefix) => mirroring ns (no prefix)": {
			FirstRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix=prefix-",
			},
			SecondRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix=",
			},
			AuthMethodExpectedNS:                    "default",
			AuthMethodExpectMapNamespacesConfig:     true,
			AuthMethodExpectedNamespacePrefixConfig: "",
			BindingRuleExpectedNS:                   "default",
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			require := require.New(tt)
			k8s, testAgent := completeEnterpriseSetup(tt)
			defer testAgent.Stop()
			setUpK8sServiceAccount(tt, k8s, ns)

			ui := cli.NewMockUi()
			defaultArgs := []string{
				"-server-address=" + strings.Split(testAgent.HTTPAddr, ":")[0],
				"-server-port=" + strings.Split(testAgent.HTTPAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-create-inject-token",
			}

			// First run. NOTE: we don't assert anything here since we've
			// tested these results in other tests. What we care about here
			// is the result after the second run.
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			responseCode := cmd.Run(append(defaultArgs, c.FirstRunArgs...))
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Second run.
			// NOTE: We're redefining the command so that the old flag values are
			// reset.
			cmd = Command{
				UI:        ui,
				clientset: k8s,
			}
			responseCode = cmd.Run(append(defaultArgs, c.SecondRunArgs...))
			require.Equal(0, responseCode, ui.ErrorWriter.String())

			// Now check that everything is as expected.
			bootToken := getBootToken(tt, k8s, resourcePrefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testAgent.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(err)

			// Check the ACL auth method is as expected.
			authMethodName := resourcePrefix + "-k8s-auth-method"
			method, _, err := consul.ACL().AuthMethodRead(authMethodName, &api.QueryOptions{
				Namespace: c.AuthMethodExpectedNS,
			})
			require.NoError(err)
			require.NotNil(method, authMethodName+" not found")
			if c.AuthMethodExpectMapNamespacesConfig {
				require.Contains(method.Config, "MapNamespaces")
				require.Contains(method.Config, "ConsulNamespacePrefix")
				require.Equal(true, method.Config["MapNamespaces"])
				require.Equal(c.AuthMethodExpectedNamespacePrefixConfig, method.Config["ConsulNamespacePrefix"])
			} else {
				require.NotContains(method.Config, "MapNamespaces")
				require.NotContains(method.Config, "ConsulNamespacePrefix")
			}

			// Check the binding rule is as expected.
			rules, _, err := consul.ACL().BindingRuleList(authMethodName, &api.QueryOptions{
				Namespace: c.BindingRuleExpectedNS,
			})
			require.NoError(err)
			require.Len(rules, 1)
		})
	}
}

// Test the tokens and policies that are created when namespaces is enabled.
func TestRun_TokensWithNamespacesEnabled(tt *testing.T) {
	tt.Parallel()

	cases := map[string]struct {
		TokenFlags  []string
		PolicyNames []string
		PolicyDCs   []string
		SecretNames []string
		LocalToken  bool
	}{
		"client token": {
			TokenFlags:  []string{"-create-client-token"},
			PolicyNames: []string{"client-token"},
			PolicyDCs:   []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-client-acl-token"},
			LocalToken:  true,
		},
		"catalog-sync token": {
			TokenFlags:  []string{"-create-sync-token"},
			PolicyNames: []string{"catalog-sync-token"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-catalog-sync-acl-token"},
			LocalToken:  false,
		},
		"connect-inject-token": {
			TokenFlags:  []string{"-create-inject-token", "-enable-namespaces"},
			PolicyNames: []string{"connect-inject-token"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-connect-inject-acl-token"},
			LocalToken:  false,
		},
		"enterprise-license token": {
			TokenFlags:  []string{"-create-enterprise-license-token"},
			PolicyNames: []string{"enterprise-license-token"},
			PolicyDCs:   []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-enterprise-license-acl-token"},
			LocalToken:  true,
		},
		"client-snapshot-agent token": {
			TokenFlags:  []string{"-create-snapshot-agent-token"},
			PolicyNames: []string{"client-snapshot-agent-token"},
			PolicyDCs:   []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-client-snapshot-agent-acl-token"},
			LocalToken:  true,
		},
		"mesh-gateway token": {
			TokenFlags:  []string{"-create-mesh-gateway-token"},
			PolicyNames: []string{"mesh-gateway-token"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-mesh-gateway-acl-token"},
			LocalToken:  false,
		},
		"ingress gateway tokens": {
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
		"terminating gateway tokens": {
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
		"acl-replication token": {
			TokenFlags:  []string{"-create-acl-replication-token"},
			PolicyNames: []string{"acl-replication-token"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-acl-replication-acl-token"},
			LocalToken:  false,
		},
		"inject token with namespaces": {
			TokenFlags:  []string{"-create-inject-token", "-enable-namespaces"},
			PolicyNames: []string{"connect-inject-token"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-connect-inject-acl-token"},
			LocalToken:  false,
		},
		"inject token with health checks": {
			TokenFlags:  []string{"-create-inject-token", "-enable-namespaces", "-enable-health-checks"},
			PolicyNames: []string{"connect-inject-token"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-connect-inject-acl-token"},
			LocalToken:  false,
		},
	}
	for testName, c := range cases {
		tt.Run(testName, func(t *testing.T) {
			k8s, testSvr := completeEnterpriseSetup(t)
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
				"-server-address", strings.Split(testSvr.HTTPAddr, ":")[0],
				"-server-port", strings.Split(testSvr.HTTPAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-enable-namespaces",
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

// Test the parsing the namespace from gateway names
func TestRun_GatewayNamespaceParsing(tt *testing.T) {
	tt.Parallel()

	cases := []struct {
		TestName         string
		TokenFlags       []string
		PolicyNames      []string
		ExpectedPolicies []string
	}{
		{
			TestName: "Ingress gateway tokens, namespaces not provided",
			TokenFlags: []string{"-ingress-gateway-name=ingress",
				"-ingress-gateway-name=gateway",
				"-ingress-gateway-name=another-gateway"},
			PolicyNames: []string{"ingress-ingress-gateway-token",
				"gateway-ingress-gateway-token",
				"another-gateway-ingress-gateway-token"},
			ExpectedPolicies: []string{`
namespace "default" {
  service "ingress" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
  service_prefix "" {
    policy = "read"
  }
}`, `
namespace "default" {
  service "gateway" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
  service_prefix "" {
    policy = "read"
  }
}`, `
namespace "default" {
  service "another-gateway" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
  service_prefix "" {
    policy = "read"
  }
}`},
		},
		{
			TestName: "Ingress gateway tokens, namespaces provided",
			TokenFlags: []string{"-ingress-gateway-name=ingress.",
				"-ingress-gateway-name=gateway.namespace1",
				"-ingress-gateway-name=another-gateway.namespace2"},
			PolicyNames: []string{"ingress-ingress-gateway-token",
				"gateway-ingress-gateway-token",
				"another-gateway-ingress-gateway-token"},
			ExpectedPolicies: []string{`
namespace "default" {
  service "ingress" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
  service_prefix "" {
    policy = "read"
  }
}`, `
namespace "namespace1" {
  service "gateway" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
  service_prefix "" {
    policy = "read"
  }
}`, `
namespace "namespace2" {
  service "another-gateway" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
  service_prefix "" {
    policy = "read"
  }
}`},
		},
		{
			TestName: "Terminating gateway tokens, namespaces not provided",
			TokenFlags: []string{"-terminating-gateway-name=terminating",
				"-terminating-gateway-name=gateway",
				"-terminating-gateway-name=another-gateway"},
			PolicyNames: []string{"terminating-terminating-gateway-token",
				"gateway-terminating-gateway-token",
				"another-gateway-terminating-gateway-token"},
			ExpectedPolicies: []string{`
namespace "default" {
  service "terminating" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
}`, `
namespace "default" {
  service "gateway" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
}`, `
namespace "default" {
  service "another-gateway" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
}`},
		},
		{
			TestName: "Terminating gateway tokens, namespaces provided",
			TokenFlags: []string{"-terminating-gateway-name=terminating.",
				"-terminating-gateway-name=gateway.namespace1",
				"-terminating-gateway-name=another-gateway.namespace2"},
			PolicyNames: []string{"terminating-terminating-gateway-token",
				"gateway-terminating-gateway-token",
				"another-gateway-terminating-gateway-token"},
			ExpectedPolicies: []string{`
namespace "default" {
  service "terminating" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
}`, `
namespace "namespace1" {
  service "gateway" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
}`, `
namespace "namespace2" {
  service "another-gateway" {
     policy = "write"
  }
  node_prefix "" {
    policy = "read"
  }
}`},
		},
	}
	for _, c := range cases {
		tt.Run(c.TestName, func(t *testing.T) {
			k8s, testSvr := completeEnterpriseSetup(t)
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
				"-k8s-namespace=" + ns,
				"-server-address", strings.Split(testSvr.HTTPAddr, ":")[0],
				"-server-port", strings.Split(testSvr.HTTPAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-enable-namespaces=true",
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

				fullPolicy, _, err := consul.ACL().PolicyRead(policy.ID, nil)
				require.NoError(err)
				require.Equal(c.ExpectedPolicies[i], fullPolicy.Rules)
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

// Set up test consul agent and kubernetes cluster.
func completeEnterpriseSetup(t *testing.T) (*fake.Clientset, *testutil.TestServer) {
	k8s := fake.NewSimpleClientset()

	svr, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
	})
	require.NoError(t, err)

	return k8s, svr
}
