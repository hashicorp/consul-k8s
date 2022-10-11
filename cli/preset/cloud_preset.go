package preset

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/hashicorp/consul-k8s/cli/common"
	"github.com/hashicorp/consul-k8s/cli/common/terminal"
	"github.com/hashicorp/consul-k8s/cli/config"
	"github.com/hashicorp/hcp-sdk-go/clients/cloud-global-network-manager-service/preview/2022-02-15/models"
	"github.com/hashicorp/hcp-sdk-go/httpclient"
	"github.com/hashicorp/hcp-sdk-go/resource"

	hcpgnm "github.com/hashicorp/hcp-sdk-go/clients/cloud-global-network-manager-service/preview/2022-02-15/client/global_network_manager_service"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	secretNameHCPClientID     = "consul-hcp-client-id"
	secretKeyHCPClientID      = "client-id"
	secretNameHCPClientSecret = "consul-hcp-client-secret"
	secretKeyHCPClientSecret  = "client-secret"
	secretNameHCPResourceID   = "consul-hcp-resource-id"
	secretKeyHCPResourceID    = "resource-id"
	secretNameHCPAPIHostname  = "consul-hcp-api-host"
	secretKeyHCPAPIHostname   = "api-hostname"
	secretNameHCPAuthURL      = "consul-hcp-auth-url"
	secretKeyHCPAuthURL       = "auth-url"
	secretNameHCPScadaAddress = "consul-hcp-scada-address"
	secretKeyHCPScadaAddress  = "scada-address"
	secretNameGossipKey       = "consul-gossip-key"
	secretKeyGossipKey        = "key"
	secretNameBootstrapToken  = "consul-bootstrap-token"
	secretKeyBootstrapToken   = "token"
	secretNameServerCA        = "consul-server-ca"
	secretNameServerCert      = "consul-server-cert"
)

// CloudBootstrapConfig represents the response fetched from the agent
// bootstrap config endpoint in HCP.
type CloudBootstrapConfig struct {
	BootstrapResponse *models.HashicorpCloudGlobalNetworkManager20220215AgentBootstrapResponse
	ConsulConfig      ConsulConfig
	HCPConfig         HCPConfig
}

// HCPConfig represents the resource-id, client-id, and client-secret
// provided by the user in order to make a call to fetch the agent bootstrap
// config data from the endpoint in HCP.
type HCPConfig struct {
	ResourceID   string
	ClientID     string
	ClientSecret string
	AuthURL      string
	APIHostname  string
	ScadaAddress string
}

// ConsulConfig represents 'cluster.consul_config' in the response
// fetched from the agent bootstrap config endpoint in HCP.
type ConsulConfig struct {
	ACL ACL `json:"acl"`
}

// ACL represents 'cluster.consul_config.acl' in the response
// fetched from the agent bootstrap config endpoint in HCP.
type ACL struct {
	Tokens Tokens `json:"tokens"`
}

// Tokens represents 'cluster.consul_config.acl.tokens' in the
// response fetched from the agent bootstrap config endpoint in HCP.
type Tokens struct {
	Agent             string `json:"agent"`
	InitialManagement string `json:"initial_management"`
}

// CloudPreset struct is an implementation of the Preset interface that is used
// to fetch agent bootrap config from HCP, save it to secrets, and provide a
// Helm values map that is used during installation.
type CloudPreset struct {
	HCPConfig           *HCPConfig
	KubernetesClient    kubernetes.Interface
	KubernetesNamespace string
	UI                  terminal.UI
	SkipSavingSecrets   bool
	Context             context.Context
	HTTPClient          *http.Client
}

// GetValueMap must fetch configuration from HCP, save various secrets from
// the response, and map the secret names into the returned value map.
func (i *CloudPreset) GetValueMap() (map[string]interface{}, error) {
	bootstrapConfig, err := i.fetchAgentBootstrapConfig()
	if err != nil {
		return nil, err
	}

	if !i.SkipSavingSecrets {
		err = i.saveSecretsFromBootstrapConfig(bootstrapConfig)
		if err != nil {
			return nil, err
		}
	}

	return i.getHelmConfigWithMapSecretNames(bootstrapConfig), nil
}

// fetchAgentBootstrapConfig use the resource-id, client-id, and client-secret
// to call to the agent bootstrap config endpoint and parse the response into a
// CloudBootstrapConfig struct.
func (i *CloudPreset) fetchAgentBootstrapConfig() (*CloudBootstrapConfig, error) {
	i.UI.Output("Fetching Consul cluster configuration from HCP", terminal.WithHeaderStyle())
	httpClientCfg := httpclient.Config{}
	clientRuntime, err := httpclient.New(httpClientCfg)
	if err != nil {
		return nil, err
	}

	hcpgnmClient := hcpgnm.New(clientRuntime, nil)
	clusterResource, err := resource.FromString(i.HCPConfig.ResourceID)
	if err != nil {
		return nil, err
	}

	params := hcpgnm.NewAgentBootstrapConfigParamsWithContext(i.Context).
		WithID(clusterResource.ID).
		WithLocationOrganizationID(clusterResource.Organization).
		WithLocationProjectID(clusterResource.Project).
		WithHTTPClient(i.HTTPClient)

	resp, err := hcpgnmClient.AgentBootstrapConfig(params, nil)
	if err != nil {
		return nil, err
	}

	bootstrapConfig := resp.GetPayload()
	i.UI.Output("HCP configuration successfully fetched.", terminal.WithSuccessStyle())

	return i.parseBootstrapConfigResponse(bootstrapConfig)
}

// parseBootstrapConfigResponse unmarshals the boostrap parseBootstrapConfigResponse
// and also sets the HCPConfig values to return CloudBootstrapConfig struct.
func (i *CloudPreset) parseBootstrapConfigResponse(bootstrapRepsonse *models.HashicorpCloudGlobalNetworkManager20220215AgentBootstrapResponse) (*CloudBootstrapConfig, error) {
	var cbc CloudBootstrapConfig
	var consulConfig ConsulConfig
	err := json.Unmarshal([]byte(bootstrapRepsonse.Bootstrap.ConsulConfig), &consulConfig)
	if err != nil {
		return nil, err
	}
	cbc.ConsulConfig = consulConfig
	cbc.HCPConfig = *i.HCPConfig
	cbc.BootstrapResponse = bootstrapRepsonse

	return &cbc, nil
}

func getOptionalSecretFromHCPConfig(hcpConfigValue, valuesConfigKey, secretName, secretKey string) string {
	if hcpConfigValue != "" {
		// Need to make sure the below has strict spaces and no tabs
		return fmt.Sprintf(`%s:
        secretName: %s
        secretKey: %s
      `, valuesConfigKey, secretName, secretKey)
	}
	return ""
}

// getHelmConfigWithMapSecretNames maps the secret names were agent bootstrap
// config values have been saved, maps them into the Helm values template for
// the cloud preset, and returns the value map.
func (i *CloudPreset) getHelmConfigWithMapSecretNames(cfg *CloudBootstrapConfig) map[string]interface{} {
	apiHostCfg := getOptionalSecretFromHCPConfig(cfg.HCPConfig.APIHostname, "apiHost", secretNameHCPAPIHostname, secretKeyHCPAPIHostname)
	authURLCfg := getOptionalSecretFromHCPConfig(cfg.HCPConfig.AuthURL, "authUrl", secretNameHCPAuthURL, secretKeyHCPAuthURL)
	scadaAddressCfg := getOptionalSecretFromHCPConfig(cfg.HCPConfig.ScadaAddress, "scadaAddress", secretNameHCPScadaAddress, secretKeyHCPScadaAddress)

	// Need to make sure the below has strict spaces and no tabs
	values := fmt.Sprintf(`
global:
  datacenter: %s
  tls:
    enabled: true
    enableAutoEncrypt: true
    caCert:
      secretName: %s
      secretKey: %s
  gossipEncryption:
    secretName: %s
    secretKey: %s
  acls:
    manageSystemACLs: true
    bootstrapToken:
      secretName: %s
      secretKey: %s
  cloud:
    enabled: true
    resourceId:
      secretName: %s
      secretKey: %s
    clientId:
      secretName: %s
      secretKey: %s
    clientSecret:
      secretName: %s
      secretKey: %s
    %s
    %s
    %s
server:
  replicas: %d
  serverCert: 
    secretName: %s
connectInject:
  enabled: true
controller:
  enabled: true
`, cfg.BootstrapResponse.Cluster.ID, secretNameServerCA, corev1.TLSCertKey,
		secretNameGossipKey, secretKeyGossipKey, secretNameBootstrapToken,
		secretKeyBootstrapToken,
		secretNameHCPResourceID, secretKeyHCPResourceID,
		secretNameHCPClientID, secretKeyHCPClientID,
		secretNameHCPClientSecret, secretKeyHCPClientSecret,
		apiHostCfg, authURLCfg, scadaAddressCfg,
		cfg.BootstrapResponse.Cluster.BootstrapExpect, secretNameServerCert)
	valuesMap := config.ConvertToMap(values)
	return valuesMap
}

// saveSecretsFromBootstrapConfig takes the following items from the
// agent bootstrap config from HCP and saves them into known secret names and
// keys:
// - HCP configresource-id.
// - HCP client-id.
// - HCP client-secret.
// - HCP auth URL (optional)
// - HCP api hostname (optional)
// - HCP scada address (optional)
// - ACL bootstrap token.
// - gossip encryption key.
// - server tls cert and key.
// - server CA cert.
func (i *CloudPreset) saveSecretsFromBootstrapConfig(config *CloudBootstrapConfig) error {
	// create namespace
	if err := i.createNamespaceIfNotExists(); err != nil {
		return err
	}

	// HCP resource id
	if config.HCPConfig.ResourceID != "" {
		data := map[string][]byte{
			secretKeyHCPResourceID: []byte(config.HCPConfig.ResourceID),
		}
		if err := i.saveSecret(secretNameHCPResourceID, data, corev1.SecretTypeOpaque); err != nil {
			return err
		}
		i.UI.Output(fmt.Sprintf("HCP resource id saved in '%s' secret in namespace '%s'.",
			secretKeyHCPResourceID, i.KubernetesNamespace), terminal.WithSuccessStyle())
	}

	// HCP client id
	if config.HCPConfig.ClientID != "" {
		data := map[string][]byte{
			secretKeyHCPClientID: []byte(config.HCPConfig.ClientID),
		}
		if err := i.saveSecret(secretNameHCPClientID, data, corev1.SecretTypeOpaque); err != nil {
			return err
		}
		i.UI.Output(fmt.Sprintf("HCP client id saved in '%s' secret in namespace '%s'.",
			secretKeyHCPClientID, i.KubernetesNamespace), terminal.WithSuccessStyle())
	}

	// HCP client secret
	if config.HCPConfig.ClientSecret != "" {
		data := map[string][]byte{
			secretKeyHCPClientSecret: []byte(config.HCPConfig.ClientSecret),
		}
		if err := i.saveSecret(secretNameHCPClientSecret, data, corev1.SecretTypeOpaque); err != nil {
			return err
		}
		i.UI.Output(fmt.Sprintf("HCP client secret saved in '%s' secret in namespace '%s'.",
			secretKeyHCPClientSecret, i.KubernetesNamespace), terminal.WithSuccessStyle())
	}

	// bootstrap token
	if config.ConsulConfig.ACL.Tokens.InitialManagement != "" {
		data := map[string][]byte{
			secretKeyBootstrapToken: []byte(config.ConsulConfig.ACL.Tokens.InitialManagement),
		}
		if err := i.saveSecret(secretNameBootstrapToken, data, corev1.SecretTypeOpaque); err != nil {
			return err
		}
		i.UI.Output(fmt.Sprintf("ACL bootstrap token saved as '%s' key in '%s' secret in namespace '%s'.",
			secretKeyBootstrapToken, secretNameBootstrapToken, i.KubernetesNamespace), terminal.WithSuccessStyle())
	}

	// gossip key
	if config.BootstrapResponse.Bootstrap.GossipKey != "" {
		data := map[string][]byte{
			secretKeyGossipKey: []byte(config.BootstrapResponse.Bootstrap.GossipKey),
		}
		if err := i.saveSecret(secretNameGossipKey, data, corev1.SecretTypeOpaque); err != nil {
			return err
		}
		i.UI.Output(fmt.Sprintf("Gossip encryption key saved as '%s' key in '%s' secret in namespace '%s'.",
			secretKeyGossipKey, secretNameGossipKey, i.KubernetesNamespace), terminal.WithSuccessStyle())
	}

	// server cert secret
	if config.BootstrapResponse.Bootstrap.ServerTLS.Cert != "" {
		data := map[string][]byte{
			corev1.TLSCertKey:       []byte(config.BootstrapResponse.Bootstrap.ServerTLS.Cert),
			corev1.TLSPrivateKeyKey: []byte(config.BootstrapResponse.Bootstrap.ServerTLS.PrivateKey),
		}
		if err := i.saveSecret(secretNameServerCert, data, corev1.SecretTypeTLS); err != nil {
			return err
		}
		i.UI.Output(fmt.Sprintf("Server TLS cert and key saved as '%s' and '%s' key in '%s secret in namespace '%s'.",
			corev1.TLSCertKey, corev1.TLSPrivateKeyKey, secretNameServerCert, i.KubernetesNamespace), terminal.WithSuccessStyle())
	}

	// server CA
	if len(config.BootstrapResponse.Bootstrap.ServerTLS.CertificateAuthorities) > 0 &&
		config.BootstrapResponse.Bootstrap.ServerTLS.CertificateAuthorities[0] != "" {
		data := map[string][]byte{
			corev1.TLSCertKey: []byte(config.BootstrapResponse.Bootstrap.ServerTLS.CertificateAuthorities[0]),
		}
		if err := i.saveSecret(secretNameServerCA, data, corev1.SecretTypeOpaque); err != nil {
			return err
		}
		i.UI.Output(fmt.Sprintf("Server TLS CA saved as '%s' key in '%s' secret in namespace '%s'.",
			corev1.TLSCertKey, secretNameServerCA, i.KubernetesNamespace), terminal.WithSuccessStyle())
	}
	// Optional secrets
	// HCP auth url
	if config.HCPConfig.AuthURL != "" {
		data := map[string][]byte{
			secretKeyHCPAuthURL: []byte(config.HCPConfig.AuthURL),
		}
		if err := i.saveSecret(secretNameHCPAuthURL, data, corev1.SecretTypeOpaque); err != nil {
			return err
		}
		i.UI.Output(fmt.Sprintf("HCP auth url saved as '%s' key in '%s' secret in namespace '%s'.",
			secretKeyHCPAuthURL, secretNameHCPAuthURL, i.KubernetesNamespace), terminal.WithSuccessStyle())
	}

	// HCP api hostname
	if config.HCPConfig.APIHostname != "" {
		data := map[string][]byte{
			secretKeyHCPAPIHostname: []byte(config.HCPConfig.APIHostname),
		}
		if err := i.saveSecret(secretNameHCPAPIHostname, data, corev1.SecretTypeOpaque); err != nil {
			return err
		}
		i.UI.Output(fmt.Sprintf("HCP api hostname saved as '%s' key in '%s' secret in namespace '%s'.",
			secretKeyHCPAPIHostname, secretNameHCPAPIHostname, i.KubernetesNamespace), terminal.WithSuccessStyle())
	}

	// HCP scada address
	if config.HCPConfig.ScadaAddress != "" {
		data := map[string][]byte{
			secretKeyHCPScadaAddress: []byte(config.HCPConfig.ScadaAddress)
		}
		if err := i.saveSecret(secretNameHCPScadaAddress, data, corev1.SecretTypeOpaque); err != nil {
			return err
		}
		i.UI.Output(fmt.Sprintf("HCP scada address saved as '%s' key in '%s' secret in namespace '%s'.",
			secretKeyHCPScadaAddress, secretNameHCPScadaAddress, i.KubernetesNamespace), terminal.WithSuccessStyle())
	}

	return nil
}

// createNamespaceIfNotExists checks to see if a given namespace exists and if
// it does not will create it.  This function is needed to ensure a namespace
// exists before HCP config secrets are saved.
func (i *CloudPreset) createNamespaceIfNotExists() error {
	i.UI.Output(fmt.Sprintf("Checking if %s namespace needs to be created", i.KubernetesNamespace), terminal.WithHeaderStyle())
	// Create k8s namespace if it doesn't exist.
	_, err := i.KubernetesClient.CoreV1().Namespaces().Get(context.Background(), i.KubernetesNamespace, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = i.KubernetesClient.CoreV1().Namespaces().Create(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: i.KubernetesNamespace,
			},
		}, metav1.CreateOptions{})
		if err != nil {
			return err
		}
		i.UI.Output(fmt.Sprintf("Namespace '%s' has been created.", i.KubernetesNamespace), terminal.WithSuccessStyle())

	} else if err != nil {
		return err
	} else {
		i.UI.Output(fmt.Sprintf("Namespace '%s' already exists.", i.KubernetesNamespace), terminal.WithSuccessStyle())
	}
	return nil
}

// saveSecret saves given key value pairs into a given secret in a given
// namespace.  It is the generic function that helps saves all of the specific
// cloud preset secrets.
func (i *CloudPreset) saveSecret(secretName string, kvps map[string][]byte, secretType corev1.SecretType) error {
	_, err := i.KubernetesClient.CoreV1().Secrets(i.KubernetesNamespace).Get(context.Background(), secretName, metav1.GetOptions{})
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: i.KubernetesNamespace,
			Labels:    map[string]string{common.CLILabelKey: common.CLILabelValue},
		},
		Data: kvps,
		Type: secretType,
	}
	if k8serrors.IsNotFound(err) {
		_, err = i.KubernetesClient.CoreV1().Secrets(i.KubernetesNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		return fmt.Errorf("'%s' secret in '%s' namespace already exists", secretName, i.KubernetesNamespace)
	}
	return nil
}
