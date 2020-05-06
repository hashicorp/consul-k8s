package serveraclinit

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/consul-k8s/subcommand/common"
	"github.com/hashicorp/consul/api"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// createLocalACL creates a policy and acl token for this dc (datacenter), i.e.
// the policy is only valid for this datacenter and the token is a local token.
func (c *Command) createLocalACL(name, rules, dc string, consulClient *api.Client) error {
	return c.createACL(name, rules, true, dc, consulClient)
}

// createGlobalACL creates a global policy and acl token. The policy is valid
// for all datacenters and the token is global. dc must be passed because the
// policy name may have the datacenter name appended.
func (c *Command) createGlobalACL(name, rules, dc string, consulClient *api.Client) error {
	return c.createACL(name, rules, false, dc, consulClient)
}

// createACL creates a policy with rules and name. If localToken is true then
// the token will be a local token and the policy will be scoped to only dc.
// If localToken is false, the policy will be global.
// The token will be written to a Kubernetes secret.
func (c *Command) createACL(name, rules string, localToken bool, dc string, consulClient *api.Client) error {
	// Create policy with the given rules.
	policyName := fmt.Sprintf("%s-token", name)
	if c.flagACLReplicationTokenFile != "" {
		// If performing ACL replication, we must ensure policy names are
		// globally unique so we append the datacenter name.
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

	// Check if the secret already exists, if so, we assume the ACL has already been
	// created and return.
	secretName := c.withPrefix(name + "-acl-token")
	_, err = c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Get(secretName, metav1.GetOptions{})
	if err == nil {
		c.log.Info(fmt.Sprintf("Secret %q already exists", secretName))
		return nil
	}

	// Create token for the policy if the secret did not exist previously.
	tokenTmpl := api.ACLToken{
		Description: fmt.Sprintf("%s Token", policyTmpl.Name),
		Policies:    []*api.ACLTokenPolicyLink{{Name: policyTmpl.Name}},
		Local:       localToken,
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

	// Write token to a Kubernetes secret.
	return c.untilSucceeds(fmt.Sprintf("writing Secret for token %s", policyTmpl.Name),
		func() error {
			secret := &apiv1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name: secretName,
				},
				Data: map[string][]byte{
					common.ACLTokenSecretKey: []byte(token),
				},
			}
			_, err := c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Create(secret)
			return err
		})
}

func (c *Command) createOrUpdateACLPolicy(policy api.ACLPolicy, consulClient *api.Client) error {
	// Attempt to create the ACL policy
	_, _, err := consulClient.ACL().PolicyCreate(&policy, &api.WriteOptions{})

	// With the introduction of Consul namespaces, if someone upgrades into a
	// Consul version with namespace support or changes any of their namespace
	// settings, the policies associated with their ACL tokens will need to be
	// updated to be namespace aware.
	if isPolicyExistsErr(err, policy.Name) {
		if c.flagEnableNamespaces {
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
			if policy.ID == "" {
				return errors.New("Unable to find existing ACL policy")
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

func (c *Command) checkAndCreateNamespace(ns string, consulClient *api.Client) error {
	// Check if the Consul namespace exists
	namespaceInfo, _, err := consulClient.Namespaces().Read(ns, nil)
	if err != nil {
		return err
	}

	// If not, create it
	if namespaceInfo == nil {
		// Create the ACLs config for the cross-Consul-namespace
		// default policy that needs to be attached
		aclConfig := api.NamespaceACLConfig{
			PolicyDefaults: []api.ACLLink{
				{Name: "cross-namespace-policy"},
			},
		}

		consulNamespace := api.Namespace{
			Name:        ns,
			Description: "Auto-generated by the ACL bootstrapping process",
			ACLs:        &aclConfig,
			Meta:        map[string]string{"external-source": "kubernetes"},
		}

		_, _, err = consulClient.Namespaces().Create(&consulNamespace, nil)
		if err != nil {
			return err
		}
		c.log.Info("created consul namespace", "name", consulNamespace.Name)
	}

	return nil
}

// isPolicyExistsErr returns true if err is due to trying to call the
// policy create API when the policy already exists.
func isPolicyExistsErr(err error, policyName string) bool {
	return err != nil &&
		strings.Contains(err.Error(), "Unexpected response code: 500") &&
		strings.Contains(err.Error(), fmt.Sprintf("Invalid Policy: A Policy with Name %q already exists", policyName))
}
