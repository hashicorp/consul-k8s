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
	// First, check if there's already an acl binding rule. If so, then this
	// work is already done.

	authMethodName := c.withPrefix("k8s-auth-method")
	var existingRules []*api.ACLBindingRule
	err := c.untilSucceeds(fmt.Sprintf("listing binding rules for auth method %s", authMethodName),
		func() error {
			var err error
			existingRules, _, err = consulClient.ACL().BindingRuleList(authMethodName, &api.QueryOptions{})
			return err
		})
	if err != nil {
		return err
	}
	// If Consul namespaces are enabled, someone may have changed their namespace
	// configuration, so auth methods and binding rules should be updated accordingly
	if len(existingRules) > 0 && !c.flagEnableNamespaces {
		c.Log.Info(fmt.Sprintf("Binding rule for %s already exists", authMethodName))
		return nil
	}

	var kubeSvc *apiv1.Service
	err = c.untilSucceeds("getting kubernetes service IP",
		func() error {
			var err error
			kubeSvc, err = c.clientset.CoreV1().Services("default").Get("kubernetes", metav1.GetOptions{})
			return err
		})
	if err != nil {
		return err
	}

	// Get the Secret name for the auth method ServiceAccount.
	var authMethodServiceAccount *apiv1.ServiceAccount
	saName := c.withPrefix("connect-injector-authmethod-svc-account")
	err = c.untilSucceeds(fmt.Sprintf("getting %s ServiceAccount", saName),
		func() error {
			var err error
			authMethodServiceAccount, err = c.clientset.CoreV1().ServiceAccounts(c.flagK8sNamespace).Get(saName, metav1.GetOptions{})
			return err
		})
	if err != nil {
		return err
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
		return err
	}

	// Now we're ready to set up Consul's auth method.
	authMethodTmpl := api.ACLAuthMethod{
		Name:        authMethodName,
		Description: "Kubernetes AuthMethod",
		Type:        "kubernetes",
		Config: map[string]interface{}{
			"Host":              fmt.Sprintf("https://%s:443", kubeSvc.Spec.ClusterIP),
			"CACert":            string(saSecret.Data["ca.crt"]),
			"ServiceAccountJWT": string(saSecret.Data["token"]),
		},
	}

	// Add options for mirroring namespaces
	if c.flagEnableInjectK8SNSMirroring {
		authMethodTmpl.Config["MapNamespaces"] = true
		authMethodTmpl.Config["ConsulNamespacePrefix"] = c.flagInjectK8SNSMirroringPrefix
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
	}

	var authMethod *api.ACLAuthMethod
	err = c.untilSucceeds(fmt.Sprintf("creating auth method %s", authMethodTmpl.Name),
		func() error {
			var err error
			// `AuthMethodCreate` will also be able to update an existing
			// AuthMethod based on the name provided. This means that any namespace
			// configuration changes will correctly update the AuthMethod.
			authMethod, _, err = consulClient.ACL().AuthMethodCreate(&authMethodTmpl, &writeOptions)
			return err
		})
	if err != nil {
		return err
	}

	// Create the binding rule.
	abr := api.ACLBindingRule{
		Description: "Kubernetes binding rule",
		AuthMethod:  authMethod.Name,
		BindType:    api.BindingRuleBindTypeService,
		BindName:    "${serviceaccount.name}",
		Selector:    c.flagBindingRuleSelector,
	}

	// Add a namespace if appropriate
	// If namespaces and mirroring are enabled, this is not necessary because
	// the binding rule will fall back to being created in the Consul `default`
	// namespace automatically, as is necessary for mirroring.
	if c.flagEnableNamespaces && !c.flagEnableInjectK8SNSMirroring {
		abr.Namespace = c.flagConsulInjectDestinationNamespace
	}

	// If the binding rule already exists and namespaces are enabled, update it
	if len(existingRules) > 0 && c.flagEnableNamespaces {
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

		err = c.untilSucceeds(fmt.Sprintf("updating acl binding rule for %s", authMethodTmpl.Name),
			func() error {
				_, _, err := consulClient.ACL().BindingRuleUpdate(&abr, nil)
				return err
			})
	} else {
		// Otherwise create the binding rule
		err = c.untilSucceeds(fmt.Sprintf("creating acl binding rule for %s", authMethodTmpl.Name),
			func() error {
				_, _, err := consulClient.ACL().BindingRuleCreate(&abr, nil)
				return err
			})
	}
	return err
}
