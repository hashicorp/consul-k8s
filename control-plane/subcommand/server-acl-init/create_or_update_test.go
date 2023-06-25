package serveraclinit

import (
	"testing"

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
	consul, err := api.NewClient(&api.Config{
		Address: svr.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(err)

	// Create the policy manually.
	policyDescription := "not the expected description"
	policyName := "policy-name"
	policy, _, err := consul.ACL().PolicyCreate(&api.ACLPolicy{
		Name:        policyName,
		Description: policyDescription,
	}, nil)
	require.NoError(err)

	// Now run the function.
	err = cmd.createOrUpdateACLPolicy(api.ACLPolicy{
		Name:        policyName,
		Description: "expected description",
	}, consul)
	require.EqualError(err,
		"policy found with name \"policy-name\" but not with expected description \"expected description\";"+
			" if this policy was created manually it must be renamed to something else because this name is reserved by consul-k8s",
	)

	// Check that the policy wasn't modified.
	rereadPolicy, _, err := consul.ACL().PolicyRead(policy.ID, nil)
	require.NoError(err)
	require.Equal(policyDescription, rereadPolicy.Description)
}

func TestCreateOrUpdateACLPolicy_Update(t *testing.T) {
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
	consul, err := api.NewClient(&api.Config{
		Address: svr.HTTPAddr,
		Token:   bootToken,
	})
	require.NoError(err)
	connectInjectRule, err := cmd.injectRules()
	require.NoError(err)
	aclReplRule, err := cmd.aclReplicationRules()
	require.NoError(err)
	policyDescription := "policy-description"
	policyName := "policy-name"
	policy, _, err := consul.ACL().PolicyCreate(&api.ACLPolicy{
		Name:        "new-policy-name",
		Description: "new-policy-desc",
	}, nil)
	require.NoError(err)
	cases := []struct {
		Name              string
		ID                string
		PolicyDescription string
		PolicyName        string
		Rules             string
		Err               error
		ExpPolicy         *api.ACLPolicy
	}{
		{
			Name:              "create",
			ID:                "",
			PolicyDescription: policyDescription,
			PolicyName:        policyName,
			Rules:             connectInjectRule,
			Err:               nil,
			ExpPolicy: &api.ACLPolicy{
				Name:        policyName,
				Description: policyDescription,
				Rules:       connectInjectRule,
			},
		},
		{
			Name:              "update",
			ID:                policy.ID,
			PolicyDescription: policy.Description,
			PolicyName:        policy.Name,
			Rules:             aclReplRule,
			Err:               nil,
			ExpPolicy: &api.ACLPolicy{
				Name:        policyName,
				Description: policyDescription,
				Rules:       aclReplRule,
			},
		},
	}
	for _, tt := range cases {
		t.Run(tt.Name, func(t *testing.T) {
			err = cmd.createOrUpdateACLPolicy(api.ACLPolicy{
				Name:        tt.PolicyName,
				Description: tt.PolicyDescription,
				Rules:       tt.Rules,
			}, consul)
			require.Equal(tt.Err, err)
			if tt.ID != "" {
				readPolicy, _, err := consul.ACL().PolicyRead(tt.ID, nil)
				require.NoError(err)
				require.Equal(tt.Rules, readPolicy.Rules)
			}
		})
	}
}
