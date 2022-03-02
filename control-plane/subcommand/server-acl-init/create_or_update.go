package serveraclinit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/consul-k8s/control-plane/subcommand/common"
	"github.com/hashicorp/consul/api"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// addRoleAndBindingRule adds an ACLRole and ACLBindingRule which reference the authMethod.
func (c *Command) addRoleAndBindingRule(client *api.Client, serviceAccountName string, authMethodName string, policies []*api.ACLRolePolicyLink) error {

	// This is the ACLRole which will allow the component which uses the serviceaccount
	// to be able to do a consul login.
	aclRoleName := fmt.Sprintf("%s-acl-role", serviceAccountName)
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
	abr := api.ACLBindingRule{
		Description: fmt.Sprintf("Binding Rule for %s", serviceAccountName),
		AuthMethod:  authMethodName,
		Selector:    fmt.Sprintf("serviceaccount.name==%q", serviceAccountName),
		BindType:    api.BindingRuleBindTypeRole,
		BindName:    aclRoleName,
	}
	return c.updateOrCreateBindingRule(client, authMethodName, &abr, true)
}

// updateOrCreateACLRole will query to see if existing role is in place and update them
// or create them if they do not yet exist.
func (c *Command) updateOrCreateACLRole(client *api.Client, role *api.ACLRole) error {
	err := c.untilSucceeds(fmt.Sprintf("update or create acl role for %s", role.Name),
		func() error {
			var err error
			aclRole, _, err := client.ACL().RoleReadByName(role.Name, &api.QueryOptions{})
			if err != nil {
				c.log.Error("unable to read ACL Roles", err)
				return err
			}
			if aclRole != nil {
				_, _, err := client.ACL().RoleUpdate(aclRole, &api.WriteOptions{})
				if err != nil {
					c.log.Error("unable to update role", err)
					return err
				}
				return nil
			}
			_, _, err = client.ACL().RoleCreate(role, &api.WriteOptions{})
			if err != nil {
				c.log.Error("unable to create role", err)
				return err
			}
			return err
		})
	return err
}

// updateOrCreateBindingRule will query to see if existing binding rules are in place and update them
// or create them if they do not yet exist.
func (c *Command) updateOrCreateBindingRule(client *api.Client, authMethodName string, abr *api.ACLBindingRule, skipNamespacing bool) error {
	// Binding rule list api call query options.
	queryOptions := api.QueryOptions{}

	// If namespaces and mirroring are enabled, this is not necessary because
	// the binding rule will fall back to being created in the Consul `default`
	// namespace automatically, as is necessary for mirroring.
	if !skipNamespacing && c.flagEnableNamespaces && !c.flagEnableInjectK8SNSMirroring {
		abr.Namespace = c.flagConsulInjectDestinationNamespace
		queryOptions.Namespace = c.flagConsulInjectDestinationNamespace
	}

	var existingRules []*api.ACLBindingRule
	err := c.untilSucceeds(fmt.Sprintf("listing binding rules for auth method %s", authMethodName),
		func() error {
			var err error
			existingRules, _, err = client.ACL().BindingRuleList(authMethodName, &queryOptions)
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
		// rule set up here in the bootstrap method.
		if abr.ID == "" {
			return errors.New("unable to find a matching ACL binding rule to update")
		}

		err = c.untilSucceeds(fmt.Sprintf("updating acl binding rule for %s", authMethodName),
			func() error {
				_, _, err := client.ACL().BindingRuleUpdate(abr, nil)
				return err
			})
	} else {
		// Otherwise create the binding rule
		err = c.untilSucceeds(fmt.Sprintf("creating acl binding rule for %s", authMethodName),
			func() error {
				_, _, err := client.ACL().BindingRuleCreate(abr, nil)
				return err
			})

	}
	return err
}

// createACLPolicyRoleAndBindingRule will create the ACL Policy for the component
// then create a set of ACLRole and ACLBindingRule which tie the component's serviceaccount
// to the authMethod, allowing the serviceaccount to later be allowed to issue a Consul Login.
func (c *Command) createACLPolicyRoleAndBindingRule(componentName string, rules string, dc string, isPrimary bool,
	authMethodName string, serviceAccountName string, client *api.Client) error {
	// Create policy with the given rules.
	policyName := fmt.Sprintf("%s-policy", componentName)
	if c.flagFederation && !isPrimary {
		// If performing ACL replication, we must ensure policy names are
		// globally unique so we append the datacenter name but only in secondary datacenters..
		policyName += fmt.Sprintf("-%s", dc)
	}
	var datacenters []string
	// TODO: when we support global auth method logins we will need to append the list of dcs.
	//if !globalToken && dc != "" {
	if dc != "" {
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
	apl := []*api.ACLRolePolicyLink{}
	apl = append(apl, ap)

	// Add the ACLRole and ACLBindingRule.
	err = c.addRoleAndBindingRule(client, serviceAccountName, authMethodName, apl)
	if err != nil {
		return err
	}
	return err
}

// createLocalACL creates a policy and acl token for this dc (datacenter), i.e.
// the policy is only valid for this datacenter and the token is a local token.
func (c *Command) createLocalACL(name, rules, dc string, isPrimary bool, consulClient *api.Client) error {
	return c.createACL(name, rules, true, dc, isPrimary, consulClient, "")
}

// createGlobalACL creates a global policy and acl token. The policy is valid
// for all datacenters and the token is global. dc must be passed because the
// policy name may have the datacenter name appended.
func (c *Command) createGlobalACL(name, rules, dc string, isPrimary bool, consulClient *api.Client) error {
	return c.createACL(name, rules, false, dc, isPrimary, consulClient, "")
}

// createGlobalACLWithSecretID creates a global policy and acl token with provided secret ID.
func (c *Command) createGlobalACLWithSecretID(name, rules, dc string, isPrimary bool, consulClient *api.Client, secretID string) error {
	return c.createACL(name, rules, false, dc, isPrimary, consulClient, secretID)
}

// createACL creates a policy with rules and name. If localToken is true then
// the token will be a local token and the policy will be scoped to only dc.
// If localToken is false, the policy will be global.
// When secretID is provided, we will use that value for the created token and
// will skip writing it to a Kubernetes secret (because in this case we assume that
// this value already exists in some secrets storage).
func (c *Command) createACL(name, rules string, localToken bool, dc string, isPrimary bool, consulClient *api.Client, secretID string) error {
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
			return c.createOrUpdateACLPolicy(policyTmpl, consulClient)
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
		_, _, err = consulClient.ACL().TokenReadSelf(&api.QueryOptions{Token: secretID})
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
			createdToken, _, err := consulClient.ACL().TokenCreate(&tokenTmpl, &api.WriteOptions{})
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

func (c *Command) createOrUpdateACLPolicy(policy api.ACLPolicy, consulClient *api.Client) error {
	// Attempt to create the ACL policy.
	_, _, err := consulClient.ACL().PolicyCreate(&policy, &api.WriteOptions{})

	// With the introduction of Consul namespaces, if someone upgrades into a
	// Consul version with namespace support or changes any of their namespace
	// settings, the policies associated with their ACL tokens will need to be
	// updated to be namespace aware.
	// Allowing the Consul node name to be configurable also requires any sync
	// policy to be updated in case the node name has changed.
	if isPolicyExistsErr(err, policy.Name) {
		if c.flagEnableNamespaces || c.flagCreateSyncToken {
			c.log.Info(fmt.Sprintf("Policy %q already exists, updating", policy.Name))

			// The policy ID is required in any PolicyUpdate call, so first we need to
			// get the existing policy to extract its ID.
			existingPolicies, _, err := consulClient.ACL().PolicyList(&api.QueryOptions{})
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
			_, _, err = consulClient.ACL().PolicyUpdate(&policy, &api.WriteOptions{})
			return err
		} else {
			c.log.Info(fmt.Sprintf("Policy %q already exists, skipping update", policy.Name))
			return nil
		}
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
