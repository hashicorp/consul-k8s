// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

//go:build enterprise

package serveraclinit

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/consul/sdk/testutil/retry"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/helper/test"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
)

// Test the auth method and acl binding rule created when namespaces are enabled
// and there's a single consul destination namespace.
func TestRun_ConnectInject_SingleDestinationNamespace(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		Destination   string
		ExtraFlags    []string
		V2BindingRule bool
	}{
		"consul default ns": {
			Destination: "default",
		},
		"consul non-default ns": {
			Destination: "destination",
		},
		"consul non-default ns w/ resource-apis": {
			Destination:   "destination",
			ExtraFlags:    []string{"-enable-resource-apis=true"},
			V2BindingRule: true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			k8s, testAgent := completeSetup(tt, false)
			setUpK8sServiceAccount(tt, k8s, ns)

			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			args := []string{
				"-addresses=" + strings.Split(testAgent.TestServer.HTTPAddr, ":")[0],
				"-http-port=" + strings.Split(testAgent.TestServer.HTTPAddr, ":")[1],
				"-grpc-port=" + strings.Split(testAgent.TestServer.GRPCAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-connect-inject",
				"-partition=default",
				"-enable-namespaces",
				"-consul-inject-destination-namespace", c.Destination,
				"-acl-binding-rule-selector=serviceaccount.name!=default",
			}

			if len(c.ExtraFlags) > 0 {
				args = append(args, c.ExtraFlags...)
			}

			responseCode := cmd.Run(args)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			bootToken := getBootToken(t, k8s, resourcePrefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testAgent.TestServer.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(t, err)

			// Ensure there's only one auth method.
			namespaceQuery := &api.QueryOptions{
				Namespace: c.Destination,
			}
			methods, _, err := consul.ACL().AuthMethodList(namespaceQuery)
			require.NoError(t, err)
			if c.Destination == "default" {
				// If the destination mamespace is default then AuthMethodList
				// will return the component-auth-method as well.
				require.Len(t, methods, 2)
			} else {
				require.Len(t, methods, 1)
			}

			// Check the ACL auth method is created in the expected namespace.
			authMethodName := resourcePrefix + "-k8s-auth-method"
			actMethod, _, err := consul.ACL().AuthMethodRead(authMethodName, namespaceQuery)
			require.NoError(t, err)
			require.NotNil(t, actMethod)
			require.Equal(t, "kubernetes", actMethod.Type)
			require.Equal(t, "Kubernetes Auth Method", actMethod.Description)
			require.NotContains(t, actMethod.Config, "MapNamespaces")
			require.NotContains(t, actMethod.Config, "ConsulNamespacePrefix")

			// Check the binding rule is as expected.
			rules, _, err := consul.ACL().BindingRuleList(authMethodName, namespaceQuery)
			require.NoError(t, err)
			require.Len(t, rules, 1)
			aclRule, _, err := consul.ACL().BindingRuleRead(rules[0].ID, namespaceQuery)
			require.NoError(t, err)
			require.NotNil(t, aclRule)
			if c.V2BindingRule {
				require.Equal(t, api.BindingRuleBindTypeTemplatedPolicy, aclRule.BindType)
				require.Equal(t, "builtin/workload-identity", aclRule.BindName)
				require.Equal(t, "${serviceaccount.name}", aclRule.BindVars.Name)
			} else {
				require.Equal(t, api.BindingRuleBindTypeService, aclRule.BindType)
				require.Equal(t, "${serviceaccount.name}", aclRule.BindName)
			}
			require.Equal(t, "Kubernetes binding rule", aclRule.Description)
			require.Equal(t, "serviceaccount.name!=default", aclRule.Selector)

			// Check that the default namespace got an attached ACL policy
			defNamespace, _, err := consul.Namespaces().Read("default", &api.QueryOptions{})
			require.NoError(t, err)
			require.NotNil(t, defNamespace)
			require.NotNil(t, defNamespace.ACLs)
			require.Len(t, defNamespace.ACLs.PolicyDefaults, 1)
			require.Equal(t, "cross-namespace-policy", defNamespace.ACLs.PolicyDefaults[0].Name)

			if c.Destination != "default" {
				// Check that only one namespace was created besides the
				// already existing `default` namespace
				namespaces, _, err := consul.Namespaces().List(&api.QueryOptions{})
				require.NoError(t, err)
				require.Len(t, namespaces, 2)

				// Check the created namespace properties
				actNamespace, _, err := consul.Namespaces().Read(c.Destination, &api.QueryOptions{})
				require.NoError(t, err)
				require.NotNil(t, actNamespace)
				require.Equal(t, c.Destination, actNamespace.Name)
				require.Equal(t, "Auto-generated by consul-k8s", actNamespace.Description)
				require.NotNil(t, actNamespace.ACLs)
				require.Len(t, actNamespace.ACLs.PolicyDefaults, 1)
				require.Equal(t, "cross-namespace-policy", actNamespace.ACLs.PolicyDefaults[0].Name)
				require.Contains(t, actNamespace.Meta, "external-source")
				require.Equal(t, "kubernetes", actNamespace.Meta["external-source"])
			}
		})
	}
}

// Test the auth method and acl binding rule created when namespaces are enabled
// and we're mirroring namespaces.
func TestRun_ConnectInject_NamespaceMirroring(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		MirroringPrefix string
		ExtraFlags      []string
		V2BindingRule   bool
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
		"no prefix w/ resource-apis": {
			MirroringPrefix: "",
			ExtraFlags:      []string{"-enable-resource-apis=true"},
			V2BindingRule:   true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			k8s, testAgent := completeSetup(tt, false)
			setUpK8sServiceAccount(tt, k8s, ns)

			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			args := []string{
				"-addresses=" + strings.Split(testAgent.TestServer.HTTPAddr, ":")[0],
				"-http-port=" + strings.Split(testAgent.TestServer.HTTPAddr, ":")[1],
				"-grpc-port=" + strings.Split(testAgent.TestServer.GRPCAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-connect-inject",
				"-partition=default",
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
				"-inject-k8s-namespace-mirroring-prefix", c.MirroringPrefix,
				"-acl-binding-rule-selector=serviceaccount.name!=default",
			}
			args = append(args, c.ExtraFlags...)
			responseCode := cmd.Run(args)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			bootToken := getBootToken(tt, k8s, resourcePrefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testAgent.TestServer.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(t, err)

			// Check the ACL auth method is as expected.
			authMethodName := resourcePrefix + "-k8s-auth-method"
			method, _, err := consul.ACL().AuthMethodRead(authMethodName, nil)
			require.NoError(t, err)
			require.NotNil(t, method, authMethodName+" not found")
			require.Equal(t, "kubernetes", method.Type)
			require.Equal(t, "Kubernetes Auth Method", method.Description)
			require.Contains(t, method.Config, "MapNamespaces")
			require.Contains(t, method.Config, "ConsulNamespacePrefix")
			require.Equal(t, true, method.Config["MapNamespaces"])
			require.Equal(t, c.MirroringPrefix, method.Config["ConsulNamespacePrefix"])

			// Check the binding rule is as expected.
			rules, _, err := consul.ACL().BindingRuleList(authMethodName, nil)
			require.NoError(t, err)
			require.Len(t, rules, 1)
			aclRule, _, err := consul.ACL().BindingRuleRead(rules[0].ID, nil)
			require.NoError(t, err)
			require.NotNil(t, aclRule)
			if c.V2BindingRule {
				require.Equal(t, api.BindingRuleBindTypeTemplatedPolicy, aclRule.BindType)
				require.Equal(t, "builtin/workload-identity", aclRule.BindName)
				require.Equal(t, "${serviceaccount.name}", aclRule.BindVars.Name)
			} else {
				require.Equal(t, api.BindingRuleBindTypeService, aclRule.BindType)
				require.Equal(t, "${serviceaccount.name}", aclRule.BindName)
			}
			require.Equal(t, "Kubernetes binding rule", aclRule.Description)
			require.Equal(t, "serviceaccount.name!=default", aclRule.Selector)
		})
	}
}

// Test that the anonymous token policy is created in the default partition from
// a non-default partition.
func TestRun_AnonymousToken_CreatedFromNonDefaultPartition(t *testing.T) {
	bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	server := partitionedSetup(t, bootToken, "test")
	k8s := fake.NewSimpleClientset()
	setUpK8sServiceAccount(t, k8s, ns)

	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
		backend:   &FakeSecretsBackend{bootstrapToken: bootToken},
	}
	cmd.init()
	args := []string{
		"-addresses=" + strings.Split(server.HTTPAddr, ":")[0],
		"-http-port=" + strings.Split(server.HTTPAddr, ":")[1],
		"-grpc-port=" + strings.Split(server.GRPCAddr, ":")[1],
		"-resource-prefix=" + resourcePrefix,
		"-k8s-namespace=" + ns,
		"-allow-dns",
		"-partition=test",
		"-enable-namespaces",
	}
	responseCode := cmd.Run(args)
	require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

	consul, err := api.NewClient(&api.Config{
		Address: server.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(t, err)

	anonPolicyName := "anonymous-token-policy"
	// Check that the anonymous token policy was created.
	policy := policyExists(t, anonPolicyName, consul)
	// Should be a global policy.
	require.Len(t, policy.Datacenters, 0)

	// Check that the anonymous token has the policy.
	tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: "anonymous"})
	require.NoError(t, err)
	require.Equal(t, anonPolicyName, tokenData.Policies[0].Name)
}

// Test that ACL policies get updated if namespaces/partition config changes.
func TestRun_ACLPolicyUpdates(t *testing.T) {
	t.Parallel()

	k8sNamespaceFlags := []string{"default", "other"}
	for _, k8sNamespaceFlag := range k8sNamespaceFlags {
		t.Run(k8sNamespaceFlag, func(t *testing.T) {
			k8s, testAgent := completeSetup(t, false)
			setUpK8sServiceAccount(t, k8s, k8sNamespaceFlag)

			ui := cli.NewMockUi()
			firstRunArgs := []string{
				"-addresses=" + strings.Split(testAgent.TestServer.HTTPAddr, ":")[0],
				"-http-port=" + strings.Split(testAgent.TestServer.HTTPAddr, ":")[1],
				"-grpc-port=" + strings.Split(testAgent.TestServer.GRPCAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace", k8sNamespaceFlag,
				"-client",
				"-allow-dns",
				"-mesh-gateway",
				"-sync-catalog",
				"-connect-inject",
				"-snapshot-agent",
				"-create-enterprise-license-token",
				"-ingress-gateway-name=igw",
				"-ingress-gateway-name=anotherigw",
				"-terminating-gateway-name=tgw",
				"-terminating-gateway-name=anothertgw",
			}
			// Our second run, we're going to update from partitions and namespaces disabled to
			// namespaces enabled with a single destination ns and partitions enabled.
			secondRunArgs := append(firstRunArgs,
				"-partition=default",
				"-enable-namespaces",
				"-consul-sync-destination-namespace=sync",
				"-consul-inject-destination-namespace=dest")

			// Run the command first to populate the policies.
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			responseCode := cmd.Run(firstRunArgs)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			bootToken := getBootToken(t, k8s, resourcePrefix, k8sNamespaceFlag)
			consul, err := api.NewClient(&api.Config{
				Address: testAgent.TestServer.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(t, err)

			// Check that the expected policies were created.
			// There will be more policies returned in the List API that are defaults
			// existing in Consul on startup, including but not limited to:
			// * global-management
			// * builtin/global-read-only
			// * agent-token
			firstRunExpectedPolicies := []string{
				"anonymous-token-policy",
				"client-policy",
				"sync-catalog-policy",
				"mesh-gateway-policy",
				"snapshot-agent-policy",
				"enterprise-license-token",
				"igw-policy",
				"anotherigw-policy",
				"builtin/global-read-only",
				"tgw-policy",
				"anothertgw-policy",
				"connect-inject-policy",
			}
			policies, _, err := consul.ACL().PolicyList(nil)
			require.NoError(t, err)

			// Collect the actual policies into a map to make it easier to assert
			// on their existence and contents.
			actualPolicies := make(map[string]string)
			for _, p := range policies {
				policy, _, err := consul.ACL().PolicyRead(p.ID, nil)
				require.NoError(t, err)
				actualPolicies[p.Name] = policy.Rules
			}
			for _, expected := range firstRunExpectedPolicies {
				aclRule, ok := actualPolicies[expected]
				require.True(t, ok, "Did not find policy %s", expected)
				// We assert that the policy doesn't have any namespace config
				// in it because later that's what we're using to test that it
				// got updated. builtin/global-ready-only always has namespaces and partitions included
				if expected != "builtin/global-read-only" {
					require.NotContains(t, aclRule, "namespace", "policy", expected)
				}
			}

			// Re-run the command with namespace flags. The policies should be updated.
			// NOTE: We're redefining the command so that the old flag values are
			// reset.
			cmd = Command{
				UI:        ui,
				clientset: k8s,
			}
			responseCode = cmd.Run(secondRunArgs)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			// Check that the policies have all been updated.
			secondRunExpectedPolicies := []string{
				"anonymous-token-policy",
				"client-policy",
				"sync-catalog-policy",
				"connect-inject-policy",
				"mesh-gateway-policy",
				"snapshot-agent-policy",
				"enterprise-license-token",
				"cross-namespace-policy",
				"igw-policy",
				"anotherigw-policy",
				"builtin/global-read-only",
				"tgw-policy",
				"anothertgw-policy",
				"partitions-token",
			}
			policies, _, err = consul.ACL().PolicyList(nil)
			require.NoError(t, err)

			// Collect the actual policies into a map to make it easier to assert
			// on their existence and contents.
			actualPolicies = make(map[string]string)
			for _, p := range policies {
				policy, _, err := consul.ACL().PolicyRead(p.ID, nil)
				require.NoError(t, err)
				actualPolicies[p.Name] = policy.Rules
			}
			for _, expected := range secondRunExpectedPolicies {
				aclRule, ok := actualPolicies[expected]
				require.True(t, ok, "Did not find policy %s", expected)

				switch expected {
				case "connect-inject-policy":
					// The connect inject token doesn't have namespace config,
					// but does change to operator:write from an empty string.
					require.Contains(t, aclRule, "policy = \"write\"")
				case "snapshot-agent-policy", "enterprise-license-token":
					// The snapshot agent and enterprise license tokens shouldn't change.
					require.NotContains(t, aclRule, "namespace")
					require.Contains(t, aclRule, "acl = \"write\"")
				case "partitions-token":
					require.Contains(t, aclRule, "operator = \"write\"")
				case "anonymous-token-policy":
					// TODO: This needs to be revisted due to recent changes in how we update the anonymous policy (NET-5174)
				default:
					// Assert that the policies have the word namespace in them. This
					// tests that they were updated. The actual contents are tested
					// in rules_test.go.
					require.Contains(t, aclRule, "namespace")
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
		// UseV2API, tests the bindingrule is compatible with workloadIdentites.
		UseV2API bool
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
		"(v2) no ns => mirroring ns, no prefix": {
			FirstRunArgs: nil,
			SecondRunArgs: []string{
				"-enable-namespaces",
				"-enable-inject-k8s-namespace-mirroring",
			},
			AuthMethodExpectedNS:                    "default",
			AuthMethodExpectMapNamespacesConfig:     true,
			AuthMethodExpectedNamespacePrefixConfig: "",
			BindingRuleExpectedNS:                   "default",
			UseV2API:                                true,
		},
		"(v2) no ns => mirroring ns, prefix": {
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
			UseV2API:                                true,
		},
		"(v2) no ns => single dest ns": {
			FirstRunArgs: nil,
			SecondRunArgs: []string{
				"-enable-namespaces",
				"-consul-inject-destination-namespace=dest",
			},
			AuthMethodExpectedNS:                    "dest",
			AuthMethodExpectMapNamespacesConfig:     false,
			AuthMethodExpectedNamespacePrefixConfig: "",
			BindingRuleExpectedNS:                   "dest",
			UseV2API:                                true,
		},
		"(v2) mirroring ns => single dest ns": {
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
			UseV2API:                                true,
		},
		"(v2) single dest ns => mirroring ns": {
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
			UseV2API:                                true,
		},
		"(v2) mirroring ns (no prefix) => mirroring ns (no prefix)": {
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
			UseV2API:                                true,
		},
		"(v2) mirroring ns => mirroring ns (same prefix)": {
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
			UseV2API:                                true,
		},
		"(v2) mirroring ns (no prefix) => mirroring ns (prefix)": {
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
			UseV2API:                                true,
		},
		"(v2) mirroring ns (prefix) => mirroring ns (no prefix)": {
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
			UseV2API:                                true,
		},
	}

	for name, c := range cases {
		t.Run(name, func(tt *testing.T) {
			k8s, testAgent := completeSetup(tt, c.UseV2API)
			setUpK8sServiceAccount(tt, k8s, ns)

			ui := cli.NewMockUi()
			defaultArgs := []string{
				"-addresses=" + strings.Split(testAgent.TestServer.HTTPAddr, ":")[0],
				"-http-port=" + strings.Split(testAgent.TestServer.HTTPAddr, ":")[1],
				"-grpc-port=" + strings.Split(testAgent.TestServer.GRPCAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-partition=default",
				"-connect-inject",
			}

			if c.UseV2API {
				defaultArgs = append(defaultArgs, "-enable-resource-apis=true")
			}

			// First run. NOTE: we don't assert anything here since we've
			// tested these results in other tests. What we care about here
			// is the result after the second run.
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			responseCode := cmd.Run(append(defaultArgs, c.FirstRunArgs...))
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			// Second run.
			// NOTE: We're redefining the command so that the old flag values are
			// reset.
			cmd = Command{
				UI:        ui,
				clientset: k8s,
			}
			responseCode = cmd.Run(append(defaultArgs, c.SecondRunArgs...))
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			// Now check that everything is as expected.
			bootToken := getBootToken(tt, k8s, resourcePrefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testAgent.TestServer.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(t, err)

			// Check the ACL auth method is as expected.
			authMethodName := resourcePrefix + "-k8s-auth-method"
			method, _, err := consul.ACL().AuthMethodRead(authMethodName, &api.QueryOptions{
				Namespace: c.AuthMethodExpectedNS,
			})
			require.NoError(t, err)
			require.NotNil(t, method, authMethodName+" not found")
			if c.AuthMethodExpectMapNamespacesConfig {
				require.Contains(t, method.Config, "MapNamespaces")
				require.Contains(t, method.Config, "ConsulNamespacePrefix")
				require.Equal(t, true, method.Config["MapNamespaces"])
				require.Equal(t, c.AuthMethodExpectedNamespacePrefixConfig, method.Config["ConsulNamespacePrefix"])
			} else {
				require.NotContains(t, method.Config, "MapNamespaces")
				require.NotContains(t, method.Config, "ConsulNamespacePrefix")
			}

			// Check the binding rule is as expected.
			rules, _, err := consul.ACL().BindingRuleList(authMethodName, &api.QueryOptions{
				Namespace: c.BindingRuleExpectedNS,
			})
			require.NoError(t, err)
			require.Len(t, rules, 1)
			if c.UseV2API {
				require.Equal(tt, api.BindingRuleBindTypeTemplatedPolicy, rules[0].BindType)
			} else {
				require.Equal(tt, api.BindingRuleBindTypeService, rules[0].BindType)
			}
		})
	}
}

// Test the tokens and policies that are created when namespaces is enabled.
func TestRun_TokensWithNamespacesEnabled(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		TokenFlags  []string
		PolicyNames []string
		PolicyDCs   []string
		SecretNames []string
		LocalToken  bool
	}{
		"enterprise-license token": {
			TokenFlags:  []string{"-create-enterprise-license-token"},
			PolicyNames: []string{"enterprise-license-token"},
			PolicyDCs:   []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-enterprise-license-acl-token"},
			LocalToken:  true,
		},
		"acl-replication token": {
			TokenFlags:  []string{"-create-acl-replication-token"},
			PolicyNames: []string{"acl-replication-token"},
			PolicyDCs:   nil,
			SecretNames: []string{resourcePrefix + "-acl-replication-acl-token"},
			LocalToken:  false,
		},
		"partitions token": {
			TokenFlags:  []string{"-partition=default"},
			PolicyNames: []string{"partitions-token"},
			PolicyDCs:   []string{"dc1"},
			SecretNames: []string{resourcePrefix + "-partitions-acl-token"},
			LocalToken:  true,
		},
	}
	for testName, c := range cases {
		t.Run(testName, func(t *testing.T) {
			k8s, testSvr := completeSetup(t, false)
			setUpK8sServiceAccount(t, k8s, ns)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			cmdArgs := append([]string{
				"-addresses", strings.Split(testSvr.TestServer.HTTPAddr, ":")[0],
				"-http-port", strings.Split(testSvr.TestServer.HTTPAddr, ":")[1],
				"-grpc-port", strings.Split(testSvr.TestServer.GRPCAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-k8s-namespace=" + ns,
				"-partition=default",
				"-enable-namespaces",
			}, c.TokenFlags...)

			responseCode := cmd.Run(cmdArgs)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			// Check that the expected policy was created.
			bootToken := getBootToken(t, k8s, resourcePrefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testSvr.TestServer.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(t, err)

			// Check that the expected policy was created.
			for i := range c.PolicyNames {
				policy := policyExists(t, c.PolicyNames[i], consul)
				require.Equal(t, c.PolicyDCs, policy.Datacenters)
				// Test that the token was created as a Kubernetes Secret.
				tokenSecret, err := k8s.CoreV1().Secrets(ns).Get(context.Background(), c.SecretNames[i], metav1.GetOptions{})
				require.NoError(t, err)
				require.NotNil(t, tokenSecret)
				token, ok := tokenSecret.Data["token"]
				require.True(t, ok)
				// Test that the token has the expected policies in Consul.
				tokenData, _, err := consul.ACL().TokenReadSelf(&api.QueryOptions{Token: string(token)})
				require.NoError(t, err)
				require.Equal(t, c.PolicyNames[i], tokenData.Policies[0].Name)
				require.Equal(t, c.LocalToken, tokenData.Local)
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
				require.Equal(t, 0, responseCode, ui.ErrorWriter.String())
			})
		})
	}
}

// Test the parsing the namespace from gateway names.
func TestRun_GatewayNamespaceParsing(t *testing.T) {
	t.Parallel()

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
			PolicyNames: []string{"ingress-policy",
				"gateway-policy",
				"another-gateway-policy"},
			ExpectedPolicies: []string{`
partition "default" {
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
  }
}`, `
partition "default" {
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
  }
}`, `
partition "default" {
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
  }
}`},
		},
		{
			TestName: "Ingress gateway tokens, namespaces provided",
			TokenFlags: []string{"-ingress-gateway-name=ingress.",
				"-ingress-gateway-name=gateway.namespace1",
				"-ingress-gateway-name=another-gateway.namespace2"},
			PolicyNames: []string{"ingress-policy",
				"gateway-policy",
				"another-gateway-policy"},
			ExpectedPolicies: []string{`
partition "default" {
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
  }
}`, `
partition "default" {
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
  }
}`, `
partition "default" {
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
  }
}`},
		},
		{
			TestName: "Terminating gateway tokens, namespaces not provided",
			TokenFlags: []string{"-terminating-gateway-name=terminating",
				"-terminating-gateway-name=gateway",
				"-terminating-gateway-name=another-gateway"},
			PolicyNames: []string{"terminating-policy",
				"gateway-policy",
				"another-gateway-policy"},
			ExpectedPolicies: []string{`
partition "default" {
  namespace "default" {
    service "terminating" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
  }
}`, `
partition "default" {
  namespace "default" {
    service "gateway" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
  }
}`, `
partition "default" {
  namespace "default" {
    service "another-gateway" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
  }
}`},
		},
		{
			TestName: "Terminating gateway tokens, namespaces provided",
			TokenFlags: []string{"-terminating-gateway-name=terminating.",
				"-terminating-gateway-name=gateway.namespace1",
				"-terminating-gateway-name=another-gateway.namespace2"},
			PolicyNames: []string{"terminating-policy",
				"gateway-policy",
				"another-gateway-policy"},
			ExpectedPolicies: []string{`
partition "default" {
  namespace "default" {
    service "terminating" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
  }
}`, `
partition "default" {
  namespace "namespace1" {
    service "gateway" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
  }
}`, `
partition "default" {
  namespace "namespace2" {
    service "another-gateway" {
       policy = "write"
    }
    node_prefix "" {
      policy = "read"
    }
  }
}`},
		},
	}
	for _, c := range cases {
		t.Run(c.TestName, func(t *testing.T) {
			k8s, testSvr := completeSetup(t, false)
			setUpK8sServiceAccount(t, k8s, ns)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmd.init()
			cmdArgs := append([]string{
				"-k8s-namespace=" + ns,
				"-addresses", strings.Split(testSvr.TestServer.HTTPAddr, ":")[0],
				"-http-port", strings.Split(testSvr.TestServer.HTTPAddr, ":")[1],
				"-grpc-port", strings.Split(testSvr.TestServer.GRPCAddr, ":")[1],
				"-resource-prefix=" + resourcePrefix,
				"-enable-namespaces=true",
				"-partition=default",
			}, c.TokenFlags...)

			responseCode := cmd.Run(cmdArgs)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			// Check that the expected policy was created.
			bootToken := getBootToken(t, k8s, resourcePrefix, ns)
			consul, err := api.NewClient(&api.Config{
				Address: testSvr.TestServer.HTTPAddr,
				Token:   bootToken,
			})
			require.NoError(t, err)

			for i := range c.PolicyNames {
				policy := policyExists(t, c.PolicyNames[i], consul)

				fullPolicy, _, err := consul.ACL().PolicyRead(policy.ID, nil)
				require.NoError(t, err)
				require.Equal(t, c.ExpectedPolicies[i], fullPolicy.Rules)
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
				require.Equal(t, 0, responseCode, ui.ErrorWriter.String())
			})
		})
	}
}

// Test that server-acl-init used the local auth method to create the desired token in the primary datacenter.
// The test works by running the login command and then ensuring that the token
// returned has the correct role for the component.
func TestRun_NamespaceEnabled_ValidateLoginToken_PrimaryDatacenter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		ComponentName string
		TokenFlags    []string
		Roles         []string
		Namespace     string
		GlobalToken   bool
	}{
		{
			ComponentName: "connect-injector",
			TokenFlags:    []string{"-connect-inject"},
			Roles:         []string{resourcePrefix + "-connect-inject-acl-role"},
			Namespace:     ns,
			GlobalToken:   false,
		},
		{
			ComponentName: "sync-catalog",
			TokenFlags:    []string{"-sync-catalog"},
			Roles:         []string{resourcePrefix + "-sync-catalog-acl-role"},
			Namespace:     ns,
			GlobalToken:   false,
		},
	}
	for _, c := range cases {
		t.Run(c.ComponentName, func(t *testing.T) {
			authMethodName := fmt.Sprintf("%s-%s", resourcePrefix, componentAuthMethod)
			serviceAccountName := fmt.Sprintf("%s-%s", resourcePrefix, c.ComponentName)

			k8s, testSvr := completeSetup(t, false)
			_, jwtToken := setUpK8sServiceAccount(t, k8s, c.Namespace)

			k8sMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				if r != nil && r.URL.Path == "/apis/authentication.k8s.io/v1/tokenreviews" && r.Method == "POST" {
					w.Write([]byte(test.TokenReviewsResponse(serviceAccountName, c.Namespace)))
				}
				if r != nil && r.URL.Path == fmt.Sprintf("/api/v1/namespaces/%s/serviceaccounts/%s", c.Namespace, serviceAccountName) &&
					r.Method == "GET" {
					w.Write([]byte(test.ServiceAccountGetResponse(serviceAccountName, c.Namespace)))
				}
			}))
			t.Cleanup(k8sMockServer.Close)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmdArgs := append([]string{
				"-timeout=1m",
				"-resource-prefix=" + resourcePrefix,
				"-enable-namespaces",
				"-k8s-namespace=" + c.Namespace,
				"-enable-namespaces",
				"-consul-inject-destination-namespace", c.Namespace,
				"-auth-method-host=" + k8sMockServer.URL,
				"-addresses", strings.Split(testSvr.TestServer.HTTPAddr, ":")[0],
				"-http-port", strings.Split(testSvr.TestServer.HTTPAddr, ":")[1],
				"-grpc-port", strings.Split(testSvr.TestServer.GRPCAddr, ":")[1],
			}, c.TokenFlags...)
			cmd.init()
			responseCode := cmd.Run(cmdArgs)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			client, err := api.NewClient(&api.Config{
				Address: testSvr.TestServer.HTTPAddr,
			})
			require.NoError(t, err)

			tok, _, err := client.ACL().Login(&api.ACLLoginParams{
				AuthMethod:  authMethodName,
				BearerToken: jwtToken,
				Meta:        map[string]string{},
			}, &api.WriteOptions{})
			require.NoError(t, err)

			require.Equal(t, len(tok.Roles), len(c.Roles))
			for _, role := range tok.Roles {
				require.Contains(t, c.Roles, role.Name)
			}
			require.Equal(t, !c.GlobalToken, tok.Local)
		})
	}
}

// Test that server-acl-init used the global auth method to create the desired token in the secondary datacenter.
// The test works by running the login command and then ensuring that the token
// returned has the correct role for the component.
func TestRun_NamespaceEnabled_ValidateLoginToken_SecondaryDatacenter(t *testing.T) {
	t.Parallel()

	cases := []struct {
		ComponentName string
		TokenFlags    []string
		Roles         []string
		Namespace     string
		GlobalToken   bool
	}{
		{
			ComponentName: "connect-injector",
			TokenFlags:    []string{"-connect-inject"},
			Roles:         []string{resourcePrefix + "-connect-inject-acl-role-dc2"},
			Namespace:     ns,
			GlobalToken:   true,
		},
		{
			ComponentName: "sync-catalog",
			TokenFlags:    []string{"-sync-catalog"},
			Roles:         []string{resourcePrefix + "-sync-catalog-acl-role-dc2"},
			Namespace:     ns,
			GlobalToken:   true,
		},
	}
	for _, c := range cases {
		t.Run(c.ComponentName, func(t *testing.T) {
			bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
			tokenFile := common.WriteTempFile(t, bootToken)
			authMethodName := fmt.Sprintf("%s-%s-%s", resourcePrefix, componentAuthMethod, "dc2")
			serviceAccountName := fmt.Sprintf("%s-%s", resourcePrefix, c.ComponentName)

			k8s, _, consulHTTPAddr, consulGRPCAddr := mockReplicatedSetup(t, bootToken)
			_, jwtToken := setUpK8sServiceAccount(t, k8s, c.Namespace)

			k8sMockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("content-type", "application/json")
				if r != nil && r.URL.Path == "/apis/authentication.k8s.io/v1/tokenreviews" && r.Method == "POST" {
					w.Write([]byte(test.TokenReviewsResponse(serviceAccountName, c.Namespace)))
				}
				if r != nil && r.URL.Path == fmt.Sprintf("/api/v1/namespaces/%s/serviceaccounts/%s", c.Namespace, serviceAccountName) &&
					r.Method == "GET" {
					w.Write([]byte(test.ServiceAccountGetResponse(serviceAccountName, c.Namespace)))
				}
			}))
			t.Cleanup(k8sMockServer.Close)

			// Run the command.
			ui := cli.NewMockUi()
			cmd := Command{
				UI:        ui,
				clientset: k8s,
			}
			cmdArgs := append([]string{
				"-federation",
				"-timeout=1m",
				"-resource-prefix=" + resourcePrefix,
				"-enable-namespaces",
				"-k8s-namespace=" + c.Namespace,
				"-enable-namespaces",
				"-consul-inject-destination-namespace", c.Namespace,
				"-acl-replication-token-file", tokenFile,
				"-auth-method-host=" + k8sMockServer.URL,
				"-addresses", strings.Split(consulHTTPAddr, ":")[0],
				"-http-port", strings.Split(consulHTTPAddr, ":")[1],
				"-grpc-port", strings.Split(consulGRPCAddr, ":")[1],
			}, c.TokenFlags...)
			cmd.init()
			responseCode := cmd.Run(cmdArgs)
			require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

			client, err := api.NewClient(&api.Config{
				Address:    consulHTTPAddr,
				Datacenter: "dc1",
			})
			require.NoError(t, err)

			retry.Run(t, func(r *retry.R) {
				tok, _, err := client.ACL().Login(&api.ACLLoginParams{
					AuthMethod:  authMethodName,
					BearerToken: jwtToken,
					Meta:        map[string]string{},
				}, &api.WriteOptions{})
				require.NoError(r, err)

				require.Equal(r, len(tok.Roles), len(c.Roles))
				for _, role := range tok.Roles {
					require.Contains(r, c.Roles, role.Name)
				}
				require.Equal(r, !c.GlobalToken, tok.Local)
			})
		})
	}
}

// Test that the partition token can be created when it's provided with a file.
func TestRun_PartitionTokenDefaultPartition_WithProvidedSecretID(t *testing.T) {
	t.Parallel()

	k8s, testSvr := completeSetup(t, false)
	setUpK8sServiceAccount(t, k8s, ns)

	partitionToken := "123e4567-e89b-12d3-a456-426614174000"
	partitionTokenFile, err := os.CreateTemp("", "partitiontoken")
	require.NoError(t, err)
	defer os.RemoveAll(partitionTokenFile.Name())

	partitionTokenFile.WriteString(partitionToken)
	// Run the command.
	ui := cli.NewMockUi()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
	}
	cmd.init()
	cmdArgs := []string{
		"-timeout=1m",
		"-k8s-namespace=" + ns,
		"-addresses", strings.Split(testSvr.TestServer.HTTPAddr, ":")[0],
		"-http-port", strings.Split(testSvr.TestServer.HTTPAddr, ":")[1],
		"-grpc-port", strings.Split(testSvr.TestServer.GRPCAddr, ":")[1],
		"-resource-prefix=" + resourcePrefix,
		"-partition=default",
		"-partition-token-file", partitionTokenFile.Name(),
	}

	responseCode := cmd.Run(cmdArgs)
	require.Equal(t, 0, responseCode, ui.ErrorWriter.String())

	// Check that this token is created.
	consul, err := api.NewClient(&api.Config{
		Address: testSvr.TestServer.HTTPAddr,
		Token:   partitionToken,
	})
	require.NoError(t, err)
	token, _, err := consul.ACL().TokenReadSelf(nil)
	require.NoError(t, err)

	for _, policyLink := range token.Policies {
		policy := policyExists(t, policyLink.Name, consul)
		require.Equal(t, policy.Datacenters, []string{"dc1"})

		// Test that the token was not created as a Kubernetes Secret.
		_, err := k8s.CoreV1().Secrets(ns).Get(context.Background(), resourcePrefix+"-partitions-acl-token", metav1.GetOptions{})
		require.True(t, k8serrors.IsNotFound(err))
	}

	// Test that if the same command is run again, it doesn't error.
	t.Run(t.Name()+"-retried", func(t *testing.T) {
		ui = cli.NewMockUi()
		cmd = Command{
			UI:        ui,
			clientset: k8s,
		}
		cmd.init()
		responseCode = cmd.Run(cmdArgs)
		require.Equal(t, 0, responseCode, ui.ErrorWriter.String())
	})
}

// partitionedSetup is a helper function which creates a server and a consul agent that runs as
// a client in the provided partitionName. The bootToken is the token used as the bootstrap token
// for both the client and the server. The helper creates a server, then creates a partition with
// the provided partitionName and then creates a client in said partition.
func partitionedSetup(t *testing.T, bootToken string, partitionName string) *testutil.TestServer {
	server := test.TestServerWithMockConnMgrWatcher(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.Tokens.InitialManagement = bootToken
	})

	server.Cfg.APIClientConfig.Token = bootToken
	serverAPIClient, err := consul.NewClient(server.Cfg.APIClientConfig, 5*time.Second)
	require.NoError(t, err)

	// Anti-flake: This can fail with "ACL system must be bootstrapped before making any requests that require authorization ..."
	// hence the retries on error.
	require.EventuallyWithTf(t, func(collect *assert.CollectT) {
		_, _, err = serverAPIClient.Partitions().Create(context.Background(), &api.Partition{Name: partitionName}, &api.WriteOptions{})
		require.NoError(collect, err)
	}, 5*time.Second, 100*time.Millisecond, "failed to create partition: %s", partitionName)

	return server.TestServer
}
