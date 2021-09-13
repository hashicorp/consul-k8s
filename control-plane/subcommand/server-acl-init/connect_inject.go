package serveraclinit

import (
	"errors"
	"fmt"

	"github.com/hashicorp/consul-k8s/control-plane/namespaces"
	"github.com/hashicorp/consul/api"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// We use the default Kubernetes service as the default host
// for the auth method created in Consul.
// This is recommended as described here:
// https://kubernetes.io/docs/tasks/access-application-cluster/access-cluster/#accessing-the-api-from-a-pod
const defaultKubernetesHost = "https://kubernetes.default.svc"

// configureConnectInject sets up auth methods so that connect injection will
// work.
func (c *Command) configureConnectInjectAuthMethod(consulClient *api.Client) error {

	authMethodName := c.withPrefix("k8s-auth-method")

	// Create the auth method template. This requires calls to the
	// kubernetes environment.
	authMethodTmpl, err := c.createAuthMethodTmpl(authMethodName)
	if err != nil {
		return err
	}

	// Set up the auth method in the specific namespace if not mirroring.
	// If namespaces and mirroring are enabled, this is not necessary because
	// the auth method will fall back to being created in the Consul `default`
	// namespace automatically, as is necessary for mirroring.
	// Note: if the config changes, an auth method will be created in the
	// correct namespace, but the old auth method will not be removed.
	writeOptions := api.WriteOptions{}
	if c.flagEnableNamespaces && !c.flagEnableInjectK8SNSMirroring {
		writeOptions.Namespace = c.flagConsulInjectDestinationNamespace

		if c.flagConsulInjectDestinationNamespace != consulDefaultNamespace {
			// If not the default namespace, check if it exists, creating it
			// if necessary. The Consul namespace must exist for the AuthMethod
			// to be created there.
			err = c.untilSucceeds(fmt.Sprintf("checking or creating namespace %s",
				c.flagConsulInjectDestinationNamespace),
				func() error {
					_, err := namespaces.EnsureExists(consulClient, c.flagConsulInjectDestinationNamespace, "cross-namespace-policy")
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
			// AuthMethod based on the name provided. This means that any
			// configuration changes will correctly update the AuthMethod.
			_, _, err = consulClient.ACL().AuthMethodCreate(&authMethodTmpl, &writeOptions)
			return err
		})
	if err != nil {
		return err
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
	err = c.untilSucceeds(fmt.Sprintf("listing binding rules for auth method %s", authMethodName),
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
			return errors.New("unable to find a matching ACL binding rule to update")
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
			authMethodServiceAccount, err = c.clientset.CoreV1().ServiceAccounts(c.flagK8sNamespace).Get(c.ctx, saName, metav1.GetOptions{})
			return err
		})
	if err != nil {
		return api.ACLAuthMethod{}, err
	}

	// ServiceAccounts always have a secret name. The secret
	// contains the JWT token.
	// Because there could be multiple secrets attached to the service account,
	// we need pick the first one of type "kubernetes.io/service-account-token".
	var saSecret *apiv1.Secret
	for _, secretRef := range authMethodServiceAccount.Secrets {
		var secret *apiv1.Secret
		err = c.untilSucceeds(fmt.Sprintf("getting %s Secret", secretRef.Name),
			func() error {
				var err error
				secret, err = c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Get(c.ctx, secretRef.Name, metav1.GetOptions{})
				return err
			})
		if secret != nil && secret.Type == apiv1.SecretTypeServiceAccountToken {
			saSecret = secret
			break
		}
	}
	if err != nil {
		return api.ACLAuthMethod{}, err
	}
	// This is very unlikely to happen because Kubernetes ensure that there is always
	// a secret of type ServiceAccountToken.
	if saSecret == nil {
		return api.ACLAuthMethod{},
			fmt.Errorf("found no secret of type 'kubernetes.io/service-account-token' associated with the %s service account", saName)
	}

	kubernetesHost := defaultKubernetesHost

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
