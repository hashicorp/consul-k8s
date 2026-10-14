// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
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
