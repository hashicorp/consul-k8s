package serveraclinit

import (
	"errors"
	"fmt"

	"github.com/hashicorp/consul/api"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// configureConnectInject sets up auth methods so that connect injection will
// work.
func (c *Command) configureConnectInject(consulClient *api.Client) error {

	authMethodName := c.withPrefix("k8s-auth-method")

	// If not running namespaces, check if there's already an auth method.
	// This means no changes need to be made to it. Binding rules should
	// still be checked in case a user has updated their config.
	var createAuthMethod bool
	if !c.flagEnableNamespaces {
		// Check if an auth method exists with the given name
		err := c.untilSucceeds(fmt.Sprintf("checking if %s auth method exists", authMethodName),
			func() error {
				am, _, err := consulClient.ACL().AuthMethodRead(authMethodName, &api.QueryOptions{})
				// This call returns nil if an AuthMethod does
				// not exist with that name. This means we will
				// need to create one.
				if err == nil && am == nil {
					createAuthMethod = true
				}
				return err
			})
		if err != nil {
			return err
		}
	}

	// If namespaces are enabled, a namespace configuration change may need
	// the auth method to be updated (as with a different mirroring prefix)
	// or a new auth method created (if a new destination namespace is specified).
	if c.flagEnableNamespaces || createAuthMethod {
		// Create the auth method template. This requires calls to the
		// kubernetes environment.
		authMethodTmpl, err := c.createAuthMethodTmpl(authMethodName)
		if err != nil {
			return err
		}

		// Set up the auth method in the specific namespace if not mirroring
		// If namespaces and mirroring are enabled, this is not necessary because
		// the auth method will fall back to being created in the Consul `default`
		// namespace automatically, as is necessary for mirroring.
		// Note: if the config changes, an auth method will be created in the
		// correct namespace, but the old auth method will not be removed.
		writeOptions := api.WriteOptions{}
		if c.flagEnableNamespaces && !c.flagEnableInjectK8SNSMirroring {
			writeOptions.Namespace = c.flagConsulInjectDestinationNamespace

			if c.flagConsulInjectDestinationNamespace != "default" {
				// If not the default namespace, check if it exists, creating it
				// if necessary. The Consul namespace must exist for the AuthMethod
				// to be created there.
				err = c.untilSucceeds(fmt.Sprintf("checking or creating namespace %s",
					c.flagConsulInjectDestinationNamespace),
					func() error {
						err := c.checkAndCreateNamespace(c.flagConsulInjectDestinationNamespace, consulClient)
						return err
					})
				if err != nil {
					return err
				}
			}
		}

		err = c.untilSucceeds(fmt.Sprintf("creating auth method %s", authMethodTmpl.Name),
			func() error {
				var err error
				// `AuthMethodCreate` will also be able to update an existing
				// AuthMethod based on the name provided. This means that any namespace
				// configuration changes will correctly update the AuthMethod.
				_, _, err = consulClient.ACL().AuthMethodCreate(&authMethodTmpl, &writeOptions)
				return err
			})
		if err != nil {
			return err
		}
	}

	// Create the binding rule.
	abr := api.ACLBindingRule{
		Description: "Kubernetes binding rule",
		AuthMethod:  authMethodName,
		BindType:    api.BindingRuleBindTypeService,
		BindName:    "${serviceaccount.name}",
		Selector:    c.flagBindingRuleSelector,
	}

	// Binding rule list api call query options
	queryOptions := api.QueryOptions{}

	// Add a namespace if appropriate
	// If namespaces and mirroring are enabled, this is not necessary because
	// the binding rule will fall back to being created in the Consul `default`
	// namespace automatically, as is necessary for mirroring.
	if c.flagEnableNamespaces && !c.flagEnableInjectK8SNSMirroring {
		abr.Namespace = c.flagConsulInjectDestinationNamespace
		queryOptions.Namespace = c.flagConsulInjectDestinationNamespace
	}

	var existingRules []*api.ACLBindingRule
	err := c.untilSucceeds(fmt.Sprintf("listing binding rules for auth method %s", authMethodName),
		func() error {
			var err error
			existingRules, _, err = consulClient.ACL().BindingRuleList(authMethodName, &queryOptions)
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
			return errors.New("Unable to find a matching ACL binding rule to update")
		}

		err = c.untilSucceeds(fmt.Sprintf("updating acl binding rule for %s", authMethodName),
			func() error {
				_, _, err := consulClient.ACL().BindingRuleUpdate(&abr, nil)
				return err
			})
	} else {
		// Otherwise create the binding rule
		err = c.untilSucceeds(fmt.Sprintf("creating acl binding rule for %s", authMethodName),
			func() error {
				_, _, err := consulClient.ACL().BindingRuleCreate(&abr, nil)
				return err
			})
	}
	return err
}

func (c *Command) createAuthMethodTmpl(authMethodName string) (api.ACLAuthMethod, error) {
	// Get the Secret name for the auth method ServiceAccount.
	var authMethodServiceAccount *apiv1.ServiceAccount
	saName := c.withPrefix("connect-injector-authmethod-svc-account")
	err := c.untilSucceeds(fmt.Sprintf("getting %s ServiceAccount", saName),
		func() error {
			var err error
			authMethodServiceAccount, err = c.clientset.CoreV1().ServiceAccounts(c.flagK8sNamespace).Get(saName, metav1.GetOptions{})
			return err
		})
	if err != nil {
		return api.ACLAuthMethod{}, err
	}

	// ServiceAccounts always have a secret name. The secret
	// contains the JWT token.
	saSecretName := authMethodServiceAccount.Secrets[0].Name

	// Get the secret that will contain the ServiceAccount JWT token.
	var saSecret *apiv1.Secret
	err = c.untilSucceeds(fmt.Sprintf("getting %s Secret", saSecretName),
		func() error {
			var err error
			saSecret, err = c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Get(saSecretName, metav1.GetOptions{})
			return err
		})
	if err != nil {
		return api.ACLAuthMethod{}, err
	}

	kubernetesHost := "https://kubernetes.default.svc"

	// Check if custom auth method Host and CACert are provided
	if c.flagInjectAuthMethodHost != "" {
		kubernetesHost = c.flagInjectAuthMethodHost
	}

	// Now we're ready to set up Consul's auth method.
	authMethodTmpl := api.ACLAuthMethod{
		Name:        authMethodName,
		Description: "Kubernetes Auth Method",
		Type:        "kubernetes",
		Config: map[string]interface{}{
			"Host":              kubernetesHost,
			"CACert":            string(saSecret.Data["ca.crt"]),
			"ServiceAccountJWT": string(saSecret.Data["token"]),
		},
	}

	// Add options for mirroring namespaces
	if c.flagEnableNamespaces && c.flagEnableInjectK8SNSMirroring {
		authMethodTmpl.Config["MapNamespaces"] = true
		authMethodTmpl.Config["ConsulNamespacePrefix"] = c.flagInjectK8SNSMirroringPrefix
	}

	return authMethodTmpl, nil
}
