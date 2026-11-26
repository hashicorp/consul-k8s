// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/consul/sdk/testutil"
	"github.com/hashicorp/go-hclog"
	"github.com/mitchellh/cli"
	"github.com/stretchr/testify/require"
	"k8s.io/client-go/kubernetes/fake"
)

// Test that we give an error if an ACL policy we were going to create
// already exists but it has a different description than what consul-k8s
// expected. In this case, it's likely that a user manually created an ACL
// policy with the same name and so we want to error.
func TestCreateOrUpdateACLPolicy_ErrorsIfDescriptionDoesNotMatch(t *testing.T) {
	require := require.New(t)
	ui := cli.NewMockUi()
	k8s := fake.NewSimpleClientset()
	cmd := Command{
		UI:              ui,
		clientset:       k8s,
		log:             hclog.NewNullLogger(),
		flagSyncCatalog: true,
	}

	// Start Consul.
	bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	svr, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.Tokens.InitialManagement = bootToken
	})
	require.NoError(err)
	defer svr.Stop()
	svr.WaitForLeader(t)

	// Get a Consul client.
	client, err := consul.NewDynamicClient(&api.Config{
		Address: svr.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(err)

	// Create the policy manually.
	policyDescription := "not the expected description"
	policyName := "policy-name"
	policy, _, err := client.ConsulClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name:        policyName,
		Description: policyDescription,
	}, nil)
	require.NoError(err)

	// Now run the function.
	err = cmd.createOrUpdateACLPolicy(api.ACLPolicy{
		Name:        policyName,
		Description: "expected description",
	}, client)
	require.EqualError(err,
		"policy found with name \"policy-name\" but not with expected description \"expected description\";"+
			" if this policy was created manually it must be renamed to something else because this name is reserved by consul-k8s",
	)

	// Check that the policy wasn't modified.
	rereadPolicy, _, err := client.ConsulClient.ACL().PolicyRead(policy.ID, nil)
	require.NoError(err)
	require.Equal(policyDescription, rereadPolicy.Description)
}

func TestCreateOrUpdateACLPolicy(t *testing.T) {
	require := require.New(t)
	ui := cli.NewMockUi()
	k8s := fake.NewSimpleClientset()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
		log:       hclog.NewNullLogger(),
	}
	cmd.init()
	// Start Consul.
	bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	svr, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.Tokens.InitialManagement = bootToken
	})
	require.NoError(err)
	defer svr.Stop()
	svr.WaitForLeader(t)

	// Get a Consul client.
	client, err := consul.NewDynamicClient(&api.Config{
		Address: svr.HTTPAddr,
		Token:   bootToken,
	})

	// Make sure the ACL system is bootstrapped first
	require.Eventually(func() bool {
		_, _, err := client.ConsulClient.ACL().PolicyList(nil)
		return err == nil
	}, 5*time.Second, 500*time.Millisecond)

	require.NoError(err)
	connectInjectRule, err := cmd.injectRules()
	require.NoError(err)
	aclReplRule, err := cmd.aclReplicationRules()
	require.NoError(err)
	policyDescription := "policy-description"
	policyName := "policy-name"
	cases := []struct {
		Name              string
		PolicyDescription string
		PolicyName        string
		Rules             string
	}{
		{
			Name:              "create",
			PolicyDescription: policyDescription,
			PolicyName:        policyName,
			Rules:             connectInjectRule,
		},
		{
			Name:              "update",
			PolicyDescription: policyDescription,
			PolicyName:        policyName,
			Rules:             aclReplRule,
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			err = cmd.createOrUpdateACLPolicy(api.ACLPolicy{
				Name:        tt.PolicyName,
				Description: tt.PolicyDescription,
				Rules:       tt.Rules,
			}, client)
			require.Nil(err)
			policy, _, err := client.ConsulClient.ACL().PolicyReadByName(tt.PolicyName, nil)
			require.Nil(err)
			require.Equal(tt.Rules, policy.Rules)
			require.Equal(tt.PolicyName, policy.Name)
			require.Equal(tt.PolicyDescription, policy.Description)
		})
	}
}

// TestUpdateOrCreateACLRole_UpdatesPoliciesOnRerun verifies that when
// updateOrCreateACLRole is called a second time with a different policy list
// (simulating an upgrade where policy rules change), the role in Consul is
// actually updated to reflect the new policies — not silently written back
// with the stale data that was read from Consul.
func TestUpdateOrCreateACLRole_UpdatesPoliciesOnRerun(t *testing.T) {
	require := require.New(t)
	ui := cli.NewMockUi()
	k8s := fake.NewSimpleClientset()
	cmd := Command{
		UI:        ui,
		clientset: k8s,
		log:       hclog.NewNullLogger(),
	}
	cmd.init()

	// Start Consul with ACLs enabled.
	bootToken := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	svr, err := testutil.NewTestServerConfigT(t, func(c *testutil.TestServerConfig) {
		c.ACL.Enabled = true
		c.ACL.Tokens.InitialManagement = bootToken
	})
	require.NoError(err)
	defer svr.Stop()
	svr.WaitForLeader(t)

	client, err := consul.NewDynamicClient(&api.Config{
		Address: svr.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(err)

	// Wait for the ACL system to be ready.
	require.Eventually(func() bool {
		_, _, err := client.ConsulClient.ACL().PolicyList(nil)
		return err == nil
	}, 5*time.Second, 500*time.Millisecond)

	roleName := "test-acl-role"
	policyV1Name := "policy-v1"
	policyV2Name := "policy-v2"
	policyDescription := func(name string) string {
		return name + " Token Policy"
	}

	// Create both policies in Consul upfront.
	_, _, err = client.ConsulClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name:        policyV1Name,
		Description: policyDescription(policyV1Name),
		Rules:       `node_prefix "" { policy = "read" }`,
	}, nil)
	require.NoError(err)

	_, _, err = client.ConsulClient.ACL().PolicyCreate(&api.ACLPolicy{
		Name:        policyV2Name,
		Description: policyDescription(policyV2Name),
		Rules:       `service_prefix "" { policy = "write" }`,
	}, nil)
	require.NoError(err)

	// --- Run 1: create the role linked to policy-v1 ---
	roleV1 := &api.ACLRole{
		Name:        roleName,
		Description: "test role",
		Policies:    []*api.ACLRolePolicyLink{{Name: policyV1Name}},
	}
	err = cmd.updateOrCreateACLRole(client, roleV1)
	require.NoError(err)

	// Verify: role exists and links to policy-v1.
	role, _, err := client.ConsulClient.ACL().RoleReadByName(roleName, nil)
	require.NoError(err)
	require.NotNil(role)
	require.Len(role.Policies, 1)
	require.Equal(policyV1Name, role.Policies[0].Name, "after first run role should link to policy-v1")

	// --- Run 2: simulate upgrade — call again with policy-v2 ---
	roleV2 := &api.ACLRole{
		Name:        roleName,
		Description: "test role",
		Policies:    []*api.ACLRolePolicyLink{{Name: policyV2Name}},
	}
	err = cmd.updateOrCreateACLRole(client, roleV2)
	require.NoError(err)

	// Verify: role must now link to policy-v2, not the old policy-v1.
	// Before the fix, this assertion FAILS because RoleUpdate(aclRole) writes
	// back the stale read object, so the role still points to policy-v1.
	role, _, err = client.ConsulClient.ACL().RoleReadByName(roleName, nil)
	require.NoError(err)
	require.NotNil(role)
	require.Len(role.Policies, 1)
	require.Equal(policyV2Name, role.Policies[0].Name,
		"after second run role should link to policy-v2 (updated), not the old policy-v1")
}

// Test that for a global component in a secondary datacenter, the existing binding rule
// is looked up in the primary datacenter (where it is written), so that a stale
// name-only rule from before the namespace fix is updated rather than duplicated.
func TestAddRoleAndBindingRule_SecondaryDCUpdatesRuleInPrimary(t *testing.T) {
	const (
		primaryDC = "dc1"
		secondDC  = "dc2"
		ruleID    = "stale-rule-id"
		saName    = "consul-connect-injector"
		roleName  = "consul-connect-inject-acl-role-dc2"
		authName  = "consul-k8s-component-auth-method"
		ns        = "consul"
	)

	// A duplicate stale rule, as left behind by earlier re-runs in a secondary datacenter.
	const dupRuleID = "stale-duplicate-rule-id"
	updated := map[string]*api.ACLBindingRule{}
	var created bool
	svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/acl/role/name/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == http.MethodPut && r.URL.Path == "/v1/acl/role":
			fmt.Fprint(w, `{"ID":"role-id","Name":"`+roleName+`"}`)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/acl/binding-rules":
			// Binding rules are only present in the primary datacenter.
			if r.URL.Query().Get("dc") != primaryDC {
				fmt.Fprint(w, `[]`)
				return
			}
			var rules []api.ACLBindingRule
			for _, id := range []string{ruleID, dupRuleID} {
				rules = append(rules, api.ACLBindingRule{
					ID:          id,
					Description: "Binding Rule for " + saName,
					AuthMethod:  authName,
					Selector:    fmt.Sprintf("serviceaccount.name==%q", saName),
					BindType:    api.BindingRuleBindTypeRole,
					BindName:    roleName,
				})
			}
			require.NoError(t, json.NewEncoder(w).Encode(rules))
		case r.Method == http.MethodPut && (r.URL.Path == "/v1/acl/binding-rule/"+ruleID || r.URL.Path == "/v1/acl/binding-rule/"+dupRuleID):
			require.Equal(t, primaryDC, r.URL.Query().Get("dc"))
			rule := &api.ACLBindingRule{}
			require.NoError(t, json.NewDecoder(r.Body).Decode(rule))
			updated[rule.ID] = rule
			fmt.Fprint(w, `{"ID":"`+rule.ID+`"}`)
		case r.Method == http.MethodPut && r.URL.Path == "/v1/acl/binding-rule":
			created = true
			fmt.Fprint(w, `{"ID":"new-rule-id"}`)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer svr.Close()

	client, err := consul.NewDynamicClient(&api.Config{Address: svr.URL})
	require.NoError(t, err)

	cmd := Command{
		UI:                 cli.NewMockUi(),
		log:                hclog.NewNullLogger(),
		ctx:                context.Background(),
		flagK8sNamespace:   ns,
		flagFederation:     true,
		flagResourcePrefix: "consul",
		retryDuration:      10 * time.Millisecond,
		apiTimeoutDuration: 5 * time.Second,
	}
	err = cmd.addRoleAndBindingRule(client, "connect-inject", saName, authName,
		[]*api.ACLRolePolicyLink{{Name: "policy"}}, true, false, primaryDC, secondDC)
	require.NoError(t, err)

	require.False(t, created, "a duplicate binding rule must not be created")
	require.Len(t, updated, 2, "every existing matching binding rule must be updated")
	for _, id := range []string{ruleID, dupRuleID} {
		require.Contains(t, updated, id)
		require.Equal(t, fmt.Sprintf("serviceaccount.name==%q and serviceaccount.namespace==%q", saName, ns), updated[id].Selector)
	}
}

// Test that createOrUpdateBindingRule keeps its behavior for the common cases (no match creates,
// a single match updates that rule) and only updates additional rules when duplicates match.
// Rules with a different bind name or description must never be touched.
func TestCreateOrUpdateBindingRule_MatchingRules(t *testing.T) {
	const (
		authName    = "auth-method"
		bindName    = "role-a"
		description = "Binding Rule for sa-a"
	)
	rule := func(id, bind, desc string) api.ACLBindingRule {
		return api.ACLBindingRule{ID: id, AuthMethod: authName, Description: desc, BindName: bind,
			BindType: api.BindingRuleBindTypeRole, Selector: "serviceaccount.name==\"old\""}
	}
	unrelatedBind := rule("unrelated-bind", "role-b", description)
	unrelatedDesc := rule("unrelated-desc", bindName, "Kubernetes binding rule")

	cases := map[string]struct {
		existing    []api.ACLBindingRule
		expUpdated  []string
		expCreated  bool
		writeOption *api.WriteOptions
	}{
		"no rules creates": {existing: nil, expCreated: true},
		"only unrelated rules creates": {
			existing:   []api.ACLBindingRule{unrelatedBind, unrelatedDesc},
			expCreated: true,
		},
		"single match updates only that rule": {
			existing:   []api.ACLBindingRule{unrelatedBind, rule("m1", bindName, description), unrelatedDesc},
			expUpdated: []string{"m1"},
		},
		"duplicates are all updated": {
			existing:   []api.ACLBindingRule{rule("m1", bindName, description), unrelatedBind, rule("m2", bindName, description), rule("m3", bindName, description)},
			expUpdated: []string{"m1", "m2", "m3"},
		},
		"nil write options (connect-inject rule path)": {
			existing:   []api.ACLBindingRule{rule("m1", bindName, description), rule("m2", bindName, description)},
			expUpdated: []string{"m1", "m2"},
		},
	}

	for name, c := range cases {
		c := c
		t.Run(name, func(t *testing.T) {
			var updated []string
			created := false
			svr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v1/acl/binding-rules":
					require.NoError(t, json.NewEncoder(w).Encode(c.existing))
				case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/acl/binding-rule/"):
					got := &api.ACLBindingRule{}
					require.NoError(t, json.NewDecoder(r.Body).Decode(got))
					require.Equal(t, strings.TrimPrefix(r.URL.Path, "/v1/acl/binding-rule/"), got.ID)
					require.Equal(t, "new-selector", got.Selector)
					updated = append(updated, got.ID)
					fmt.Fprint(w, `{"ID":"`+got.ID+`"}`)
				case r.Method == http.MethodPut && r.URL.Path == "/v1/acl/binding-rule":
					created = true
					fmt.Fprint(w, `{"ID":"new"}`)
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			defer svr.Close()

			client, err := consul.NewDynamicClient(&api.Config{Address: svr.URL})
			require.NoError(t, err)
			cmd := Command{
				log:                hclog.NewNullLogger(),
				ctx:                context.Background(),
				retryDuration:      10 * time.Millisecond,
				apiTimeoutDuration: 5 * time.Second,
			}
			abr := &api.ACLBindingRule{AuthMethod: authName, Description: description, BindName: bindName,
				BindType: api.BindingRuleBindTypeRole, Selector: "new-selector"}
			var wo *api.WriteOptions
			if name != "nil write options (connect-inject rule path)" {
				wo = &api.WriteOptions{}
			}
			require.NoError(t, cmd.createOrUpdateBindingRule(client, authName, abr, &api.QueryOptions{}, wo))

			require.ElementsMatch(t, c.expUpdated, updated)
			require.Equal(t, c.expCreated, created)
		})
	}
}
