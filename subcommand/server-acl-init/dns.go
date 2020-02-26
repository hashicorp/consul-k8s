package serveraclinit

import (
	"github.com/hashicorp/consul/api"
)

// configureDNSPolicies sets up policies and tokens so that Consul DNS will
// work.
func (c *Command) configureDNSPolicies(consulClient *api.Client) error {
	dnsRules, err := c.dnsRules()
	if err != nil {
		c.Log.Error("Error templating dns rules", "err", err)
		return err
	}

	// Create policy for the anonymous token
	dnsPolicy := api.ACLPolicy{
		Name:        "dns-policy",
		Description: "DNS Policy",
		Rules:       dnsRules,
	}

	err = c.untilSucceeds("creating dns policy - PUT /v1/acl/policy",
		func() error {
			return c.createOrUpdateACLPolicy(dnsPolicy, consulClient)
		})
	if err != nil {
		return err
	}

	// Create token to get sent to TokenUpdate
	aToken := api.ACLToken{
		AccessorID: "00000000-0000-0000-0000-000000000002",
		Policies:   []*api.ACLTokenPolicyLink{{Name: dnsPolicy.Name}},
	}

	// Update anonymous token to include this policy
	return c.untilSucceeds("updating anonymous token with DNS policy",
		func() error {
			_, _, err := consulClient.ACL().TokenUpdate(&aToken, &api.WriteOptions{})
			return err
		})
}
