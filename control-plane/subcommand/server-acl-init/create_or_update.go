// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package serveraclinit

import (
	"fmt"
	"strings"

	"github.com/hashicorp/consul/api"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
)

// createACLPolicyRoleAndBindingRule will create the ACL Policy for the component
// then create a set of ACLRole and ACLBindingRule which tie the component's serviceaccount
// to the authMethod, allowing the serviceaccount to later be allowed to issue a Consul Login.
func (c *Command) createACLPolicyRoleAndBindingRule(componentName, rules, dc, primaryDC string, global, primary bool, authMethodName, serviceAccountName string, client *consul.DynamicClient) error {
	// Create policy with the given rules.
	policyName := fmt.Sprintf("%s-policy", componentName)
	if c.flagFederation && !primary {
		// If performing ACL replication, we must ensure policy names are
		// globally unique so we append the datacenter name but only in secondary datacenters..
		policyName += fmt.Sprintf("-%s", dc)
	}
	var datacenters []string
	if !global && dc != "" {
		datacenters = append(datacenters, dc)
	}
	policyTmpl := api.ACLPolicy{
		Name:        policyName,
		Description: fmt.Sprintf("%s Token Policy", policyName),
		Rules:       rules,
		Datacenters: datacenters,
	}
	err := c.untilSucceeds(fmt.Sprintf("creating %s policy", policyTmpl.Name),
		func() error {
			return c.createOrUpdateACLPolicy(policyTmpl, client)
		})
	if err != nil {
		return err
	}

	// Create an ACLRolePolicyLink list to attach to the ACLRole.
	ap := &api.ACLRolePolicyLink{
		Name: policyName,
	}
	var apl []*api.ACLRolePolicyLink
	apl = append(apl, ap)

	// Add the ACLRole and ACLBindingRule.
	return c.addRoleAndBindingRule(client, componentName, serviceAccountName, authMethodName, apl, global, primary, primaryDC, dc)
}

// addRoleAndBindingRule adds an ACLRole and ACLBindingRule which reference the authMethod.
func (c *Command) addRoleAndBindingRule(client *consul.DynamicClient, componentName, serviceAccountName, authMethodName string, policies []*api.ACLRolePolicyLink, global, primary bool, primaryDC, dc string) error {
	// This is the ACLRole which will allow the component which uses the serviceaccount
	// to be able to do a consul login.
	aclRoleName := c.withPrefix(fmt.Sprintf("%s-acl-role", componentName))
	if c.flagFederation && !primary {
		// If performing ACL replication, we must ensure policy names are
		// globally unique so we append the datacenter name but only in secondary datacenters.
		aclRoleName += fmt.Sprintf("-%s", dc)
	}
	role := &api.ACLRole{
		Name:        aclRoleName,
		Description: fmt.Sprintf("ACL Role for %s", serviceAccountName),
		Policies:    policies,
	}
	err := c.updateOrCreateACLRole(client, role)
	if err != nil {
		c.log.Error("unable to update or create ACL Role", err)
		return err
	}

	// Create the ACLBindingRule, this ties the Policies defined in the Role to the authMethod via serviceaccount.
	abr := &api.ACLBindingRule{
		Description: fmt.Sprintf("Binding Rule for %s", serviceAccountName),
		AuthMethod:  authMethodName,
		Selector:    fmt.Sprintf("serviceaccount.name==%q", serviceAccountName),
		BindType:    api.BindingRuleBindTypeRole,
		BindName:    aclRoleName,
	}
	writeOptions := &api.WriteOptions{}
	if global && dc != primaryDC {
		writeOptions.Datacenter = primaryDC
	}
	return c.createOrUpdateBindingRule(client, authMethodName, abr, &api.QueryOptions{}, writeOptions)
}

// updateOrCreateACLRole will query to see if existing role is in place and update them
// or create them if they do not yet exist.
func (c *Command) updateOrCreateACLRole(client *consul.DynamicClient, role *api.ACLRole) error {
	err := c.untilSucceeds(fmt.Sprintf("update or create acl role for %s", role.Name),
		func() error {
			var err error
			err = client.RefreshClient()
			if err != nil {
				c.log.Error("could not refresh client", err)
			}
			aclRole, _, err := client.ConsulClient.ACL().RoleReadByName(role.Name, &api.QueryOptions{})
			if err != nil {
				c.log.Error("unable to read ACL Roles", err)
				return err
			}
			if aclRole != nil {
				_, _, err := client.ConsulClient.ACL().RoleUpdate(aclRole, &api.WriteOptions{})
				if err != nil {
					c.log.Error("unable to update role", err)
					return err
				}
				return nil
			}
			_, _, err = client.ConsulClient.ACL().RoleCreate(role, &api.WriteOptions{})
			if err != nil {
				c.log.Error("unable to create role", err)
				return err
			}
			return err
		})
	return err
}

// createConnectBindingRule will query to see if existing binding rules are in place and update them
// or create them if they do not yet exist.
func (c *Command) createConnectBindingRule(client *consul.DynamicClient, authMethodName string, abr *api.ACLBindingRule) error {
	// Binding rule list api call query options.
	queryOptions := api.QueryOptions{}

	// If namespaces and mirroring are enabled, this is not necessary because
	// the binding rule will fall back to being created in the Consul `default`
	// namespace automatically, as is necessary for mirroring.
	if c.flagEnableNamespaces && !c.flagEnableInjectK8SNSMirroring {
		abr.Namespace = c.flagConsulInjectDestinationNamespace
		queryOptions.Namespace = c.flagConsulInjectDestinationNamespace
	}

	return c.createOrUpdateBindingRule(client, authMethodName, abr, &queryOptions, nil)
}

func (c *Command) createOrUpdateBindingRule(client *consul.DynamicClient, authMethodName string, abr *api.ACLBindingRule, queryOptions *api.QueryOptions, writeOptions *api.WriteOptions) error {
	var existingRules []*api.ACLBindingRule
	err := c.untilSucceeds(fmt.Sprintf("listing binding rules for auth method %s", authMethodName),
		func() error {
			var err error
			err = client.RefreshClient()
			if err != nil {
				c.log.Error("could not refresh client", err)
			}
			existingRules, _, err = client.ConsulClient.ACL().BindingRuleList(authMethodName, queryOptions)
			return err
		})
	if err != nil {
		return err
	}

	// If the binding rule already exists, update it
	// This updates the binding rule any time the acl bootstrapping
	// command is rerun, which is a bit of extra overhead, but is
	// necessary to pick up any potential config changes.
	if len(existingRules) > 0 {
		// Find the policy that matches our name and description
		// and that's the ID we need
		for _, existingRule := range existingRules {
			if existingRule.BindName == abr.BindName && existingRule.Description == abr.Description {
				abr.ID = existingRule.ID
			}
		}

		// This will only happen if there are existing policies
		// for this auth method, but none that match the binding
		// rule set up here in the bootstrap method. Hence the
		// new binding rule must be created as it belongs to the
		// same auth method.
		if abr.ID == "" {
			c.log.Info("unable to find a matching ACL binding rule to update. creating ACL binding rule.")
			err = c.untilSucceeds(fmt.Sprintf("creating acl binding rule for %s", authMethodName),
				func() error {
					err = client.RefreshClient()
					if err != nil {
						c.log.Error("could not refresh client", err)
					}
					_, _, err := client.ConsulClient.ACL().BindingRuleCreate(abr, writeOptions)
					return err
				})
		} else {
			err = c.untilSucceeds(fmt.Sprintf("updating acl binding rule for %s", authMethodName),
				func() error {
					err = client.RefreshClient()
					if err != nil {
						c.log.Error("could not refresh client", err)
					}
					_, _, err := client.ConsulClient.ACL().BindingRuleUpdate(abr, writeOptions)
					return err
				})
		}
	} else {
		// Otherwise create the binding rule
		err = c.untilSucceeds(fmt.Sprintf("creating acl binding rule for %s", authMethodName),
			func() error {
				err = client.RefreshClient()
				if err != nil {
					c.log.Error("could not refresh client", err)
				}
				_, _, err := client.ConsulClient.ACL().BindingRuleCreate(abr, writeOptions)
				return err
			})
	}
	return err
}

// createLocalACL creates a policy and acl token for this dc (datacenter), i.e.
// the policy is only valid for this datacenter and the token is a local token.
func (c *Command) createLocalACL(name, rules, dc string, isPrimary bool, consulClient *consul.DynamicClient) error {
	return c.createACL(name, rules, true, dc, isPrimary, consulClient, "")
}

// createGlobalACL creates a global policy and acl token. The policy is valid
// for all datacenters and the token is global. dc must be passed because the
// policy name may have the datacenter name appended.
func (c *Command) createGlobalACL(name, rules, dc string, isPrimary bool, consulClient *consul.DynamicClient) error {
	return c.createACL(name, rules, false, dc, isPrimary, consulClient, "")
}

// createACLWithSecretID creates a global policy and acl token with provided secret ID.
func (c *Command) createACLWithSecretID(name, rules, dc string, isPrimary bool, consulClient *consul.DynamicClient, secretID string, local bool) error {
	return c.createACL(name, rules, local, dc, isPrimary, consulClient, secretID)
}

// createACL creates a policy with rules and name. If localToken is true then
// the token will be a local token and the policy will be scoped to only dc.
// If localToken is false, the policy will be global.
// When secretID is provided, we will use that value for the created token and
// will skip writing it to a Kubernetes secret (because in this case we assume that
// this value already exists in some secrets storage).
func (c *Command) createACL(name, rules string, localToken bool, dc string, isPrimary bool, client *consul.DynamicClient, secretID string) error {
	// Create policy with the given rules.
	policyName := fmt.Sprintf("%s-token", name)
	if c.flagFederation && !isPrimary {
		// If performing ACL replication, we must ensure policy names are
		// globally unique so we append the datacenter name but only in secondary datacenters.
		policyName += fmt.Sprintf("-%s", dc)
	}
	var datacenters []string
	if localToken && dc != "" {
		datacenters = append(datacenters, dc)
	}
	policyTmpl := api.ACLPolicy{
		Name:        policyName,
		Description: fmt.Sprintf("%s Token Policy", policyName),
		Rules:       rules,
		Datacenters: datacenters,
	}
	err := c.untilSucceeds(fmt.Sprintf("creating %s policy", policyTmpl.Name),
		func() error {
			return c.createOrUpdateACLPolicy(policyTmpl, client)
		})
	if err != nil {
		return err
	}

	// Create token for the policy if the secret did not exist previously.
	tokenTmpl := api.ACLToken{
		Description: fmt.Sprintf("%s Token", policyTmpl.Name),
		Policies:    []*api.ACLTokenPolicyLink{{Name: policyTmpl.Name}},
		Local:       localToken,
	}

	// Check if the replication token already exists in some form.
	// When secretID is not provided, we assume that replication token should exist
	// as a Kubernetes secret.
	secretName := c.withPrefix(name + "-acl-token")
	if secretID == "" {
		// Check if the secret already exists, if so, we assume the ACL has already been
		// created and return.
		_, err = c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Get(c.ctx, secretName, metav1.GetOptions{})
		if err == nil {
			c.log.Info(fmt.Sprintf("Secret %q already exists", secretName))
			return nil
		}
	} else {
		// If secretID is provided, we check if the token with secretID already exists in Consul
		// and exit if it does. Otherwise, set the secretID to the provided value.
		_, _, err = client.ConsulClient.ACL().TokenReadSelf(&api.QueryOptions{Token: secretID})
		if err == nil {
			c.log.Info("ACL replication token already exists; skipping creation")
			return nil
		} else {
			tokenTmpl.SecretID = secretID
		}
	}

	var token string
	err = c.untilSucceeds(fmt.Sprintf("creating token for policy %s", policyTmpl.Name),
		func() error {
			err = client.RefreshClient()
			if err != nil {
				c.log.Error("could not refresh client", err)
			}
			createdToken, _, err := client.ConsulClient.ACL().TokenCreate(&tokenTmpl, &api.WriteOptions{})
			if err == nil {
				token = createdToken.SecretID
			}
			return err
		})
	if err != nil {
		return err
	}

	if secretID == "" {
		// Write token to a Kubernetes secret.
		return c.untilSucceeds(fmt.Sprintf("writing Secret for token %s", policyTmpl.Name),
			func() error {
				secret := &apiv1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:   secretName,
						Labels: map[string]string{common.CLILabelKey: common.CLILabelValue},
					},
					Data: map[string][]byte{
						common.ACLTokenSecretKey: []byte(token),
					},
				}
				_, err := c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Create(c.ctx, secret, metav1.CreateOptions{})
				return err
			})
	}
	return nil
}

func (c *Command) createOrUpdateACLPolicy(policy api.ACLPolicy, client *consul.DynamicClient) error {
	err := client.RefreshClient()
	if err != nil {
		c.log.Error("could not refresh client", err)
	}

	// Attempt to create the ACL policy.
	_, _, err = client.ConsulClient.ACL().PolicyCreate(&policy, &api.WriteOptions{})

	// With the introduction of Consul namespaces, if someone upgrades into a
	// Consul version with namespace support or changes any of their namespace
	// settings, the policies associated with their ACL tokens will need to be
	// updated to be namespace aware.
	// Allowing the Consul node name to be configurable also requires any sync
	// policy to be updated in case the node name has changed.
	if isPolicyExistsErr(err, policy.Name) {
		c.log.Info(fmt.Sprintf("Policy %q already exists, updating", policy.Name))

		// The policy ID is required in any PolicyUpdate call, so first we need to
		// get the existing policy to extract its ID.
		existingPolicies, _, err := client.ConsulClient.ACL().PolicyList(&api.QueryOptions{})
		if err != nil {
			return err
		}

		// Find the policy that matches our name and description
		// and that's the ID we need
		for _, existingPolicy := range existingPolicies {
			if existingPolicy.Name == policy.Name && existingPolicy.Description == policy.Description {
				policy.ID = existingPolicy.ID
			}
		}

		// This shouldn't happen, because we're looking for a policy
		// only after we've hit a `Policy already exists` error.
		// The only time it might happen is if a user has manually created a policy
		// with this name but used a different description. In this case,
		// we don't want to overwrite the policy so we just error.
		if policy.ID == "" {
			return fmt.Errorf("policy found with name %q but not with expected description %q; "+
				"if this policy was created manually it must be renamed to something else because this name is reserved by consul-k8s",
				policy.Name, policy.Description)
		}

		// Update the policy now that we've found its ID
		_, _, err = client.ConsulClient.ACL().PolicyUpdate(&policy, &api.WriteOptions{})
		return err
	}
	return err
}

// isPolicyExistsErr returns true if err is due to trying to call the
// policy create API when the policy already exists.
func isPolicyExistsErr(err error, policyName string) bool {
	return err != nil &&
		strings.Contains(err.Error(), "Unexpected response code: 500") &&
		strings.Contains(err.Error(), fmt.Sprintf("Invalid Policy: A Policy with Name %q already exists", policyName))
}
