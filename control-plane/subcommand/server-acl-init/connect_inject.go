package serveraclinit

import (
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
func (c *Command) configureConnectInjectAuthMethod(consulClient *api.Client, authMethodName string) error {

	// Create the auth method template. This requires calls to the
	// kubernetes environment.
	authMethodTmpl, err := c.createAuthMethodTmpl(authMethodName, true)
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

	c.log.Info("creating inject binding rule")
	// Create the binding rule.
	abr := api.ACLBindingRule{
		Description: "Kubernetes binding rule",
		AuthMethod:  authMethodName,
		BindType:    api.BindingRuleBindTypeService,
		BindName:    "${serviceaccount.name}",
		Selector:    c.flagBindingRuleSelector,
	}
	return c.createConnectBindingRule(consulClient, authMethodName, &abr)
}

// createAuthMethodTmpl sets up the auth method template based on the connect-injector's service account
// jwt token. It is common for both the connect inject auth method and the component auth method
// with the option to add namespace specific configuration to the auth method template via `useNS`.
func (c *Command) createAuthMethodTmpl(authMethodName string, useNS bool) (api.ACLAuthMethod, error) {
	// Get the Secret name for the auth method ServiceAccount.
	var authMethodServiceAccount *apiv1.ServiceAccount
	serviceAccountName := c.withPrefix("auth-method")
	err := c.untilSucceeds(fmt.Sprintf("getting %s ServiceAccount", serviceAccountName),
		func() error {
			var err error
			authMethodServiceAccount, err = c.clientset.CoreV1().ServiceAccounts(c.flagK8sNamespace).Get(c.ctx, serviceAccountName, metav1.GetOptions{})
			return err
		})
	if err != nil {
		return api.ACLAuthMethod{}, err
	}

	var saSecret *apiv1.Secret
	secretNames := []string{}
	if len(authMethodServiceAccount.Secrets) == 0 {
		// In Kube 1.21+ there is no automatically generated JWT token for a serviceaccount.
		// Furthermore there is no reference to it in the serviceaccount. Instead use the one we
		// have expected to have been created.
		secretNames = append(secretNames, c.withPrefix("auth-method"))
	} else {
		// ServiceAccounts always have a secret name in k8s < 1.21. The secret contains the JWT token.
		// Because there could be multiple secrets attached to the service account,
		// we need pick the first one of type "kubernetes.io/service-account-token".
		for _, secretRef := range authMethodServiceAccount.Secrets {
			secretNames = append(secretNames, secretRef.Name)
		}
	}
	for _, secretName := range secretNames {
		var secret *apiv1.Secret
		err = c.untilSucceeds(fmt.Sprintf("getting %s Secret", secretName),
			func() error {
				var err error
				secret, err = c.clientset.CoreV1().Secrets(c.flagK8sNamespace).Get(c.ctx, secretName, metav1.GetOptions{})
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

	// This is unlikely to happen since we now deploy the secret through Helm, but should catch any corner-cases
	// where the secret is not deployed for some reason.
	if saSecret == nil {
		return api.ACLAuthMethod{},
			fmt.Errorf("found no secret of type 'kubernetes.io/service-account-token' associated with the %s service account", serviceAccountName)
	}

	kubernetesHost := defaultKubernetesHost

	// Check if custom auth method Host and CACert are provided
	if c.flagAuthMethodHost != "" {
		kubernetesHost = c.flagAuthMethodHost
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

	// Add options for mirroring namespaces, this is only used by the connect inject auth method
	// and so can be disabled for the component auth method.
	if useNS && c.flagEnableNamespaces && c.flagEnableInjectK8SNSMirroring {
		authMethodTmpl.Config["MapNamespaces"] = true
		authMethodTmpl.Config["ConsulNamespacePrefix"] = c.flagInjectK8SNSMirroringPrefix
	}

	return authMethodTmpl, nil
}
