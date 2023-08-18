// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
	"github.com/hashicorp/consul/api"
)

// configureAnonymousPolicy sets up policies and tokens so that Consul DNS and
// cross-datacenter Consul connect calls will work.
func (c *Command) configureAnonymousPolicy(consulClient *api.Client) error {
	exists, err := checkIfAnonymousTokenPolicyExists(consulClient)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	anonRules, err := c.anonymousTokenRules()
	if err != nil {
		c.log.Error("Error templating anonymous token rules", "err", err)
		return err
	}

	// Create policy for the anonymous token
	anonPolicy := api.ACLPolicy{
		Name:        "anonymous-token-policy",
		Description: "Anonymous token Policy",
		Rules:       anonRules,
	}

	err = c.untilSucceeds("creating anonymous token policy - PUT /v1/acl/policy",
		func() error {
			return c.createOrUpdateACLPolicy(anonPolicy, consulClient)
		})
	if err != nil {
		return err
	}

	// Create token to get sent to TokenUpdate
	aToken := api.ACLToken{
		AccessorID: "00000000-0000-0000-0000-000000000002",
		Policies:   []*api.ACLTokenPolicyLink{{Name: anonPolicy.Name}},
	}

	// Update anonymous token to include this policy
	return c.untilSucceeds("updating anonymous token with policy",
		func() error {
			_, _, err := consulClient.ACL().TokenUpdate(&aToken, &api.WriteOptions{})
			return err
		})
}

func checkIfAnonymousTokenPolicyExists(consulClient *api.Client) (bool, error) {
	token, _, err := consulClient.ACL().TokenRead("00000000-0000-0000-0000-000000000002", nil)
	if err != nil {
		return false, err
	}
	existingPolicies, _, err := consulClient.ACL().PolicyList(&api.QueryOptions{})
	if err != nil {
		return false, err
	}
	policyID := ""
	for _, existingPolicy := range existingPolicies {
		if existingPolicy.Name == "anonymous-token-policy" && existingPolicy.Description == "Anonymous token Policy" {
			policyID = existingPolicy.ID
		}
	}
	for _, policy := range token.Policies {
		if policy.ID == policyID {
			return true, nil
		}
	}
	return false, nil
}
