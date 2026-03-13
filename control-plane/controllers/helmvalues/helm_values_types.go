package helmvalues

// HelmValues represents the structure stored in the ConfigMap.
type HelmValues struct {
	// These two fields mirror root chart values used by the Helm helper template
	// "consul.fullname".
	FullNameOverride string `json:"fullnameOverride"`
	NameOverride     string `json:"nameOverride"`

	Release             ReleaseConfig             `json:"release"`
	Global              GlobalConfig              `json:"global"`
	TerminatingGateways TerminatingGatewaysConfig `json:"terminatingGateways"`
	ConnectInject       ConnectInjectConfig       `json:"connectInject"`
	ExternalServers     ExternalServersConfig     `json:"externalServers"`
}

type ReleaseConfig struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
}

type GlobalConfig struct {
	Name                      string            `json:"name"`
	Enabled                   bool              `json:"enabled"`
	LogLevel                  string            `json:"logLevel"`
	LogJSON                   bool              `json:"logJSON"`
	Datacenter                string            `json:"datacenter"`
	EnableConsulNamespaces    bool              `json:"enableConsulNamespaces"`
	AdminPartitions           AdminPartitions   `json:"adminPartitions"`
	TLS                       TLSConfig         `json:"tls"`
	ACLs                      ACLsConfig        `json:"acls"`
	Metrics                   MetricsConfig     `json:"metrics"`
	ImageK8S                  string            `json:"imageK8S"`
	ImagePullPolicy           string            `json:"imagePullPolicy"`
	ImageConsulDataplane      string            `json:"imageConsulDataplane"`
	ExtraLabels               map[string]string `json:"extraLabels"`
	SecretsBackend            SecretsBackend    `json:"secretsBackend"`
	ConsulAPITimeout          string            `json:"consulAPITimeout"`
	EnablePodSecurityPolicies bool              `json:"enablePodSecurityPolicies"`
	OpenShiftEnabled          bool              `json:"openShiftEnabled"`
}

type AdminPartitions struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name"`
}

type TLSConfig struct {
	Enabled bool   `json:"enabled"`
	CACert  CACert `json:"caCert"`
}

type CACert struct {
	SecretName string `json:"secretName"`
	SecretKey  string `json:"secretKey,omitempty"`
}

type ACLsConfig struct {
	ManageSystemACLs bool `json:"manageSystemACLs"`
}

type MetricsConfig struct {
	Enabled              bool `json:"enabled"`
	EnableGatewayMetrics bool `json:"enableGatewayMetrics"`
}

type TerminatingGatewaysConfig struct {
	Enabled  bool      `json:"enabled"`
	LogLevel string    `json:"logLevel"`
	Defaults Defaults  `json:"defaults"`
	Gateways []Gateway `json:"gateways"`
}

type Defaults struct {
	Replicas                  int                    `json:"replicas"`
	ConsulNamespace           string                 `json:"consulNamespace"`
	Annotations               string                 `json:"annotations"`
	Affinity                  string                 `json:"affinity"`
	Tolerations               string                 `json:"tolerations"`
	TopologySpreadConstraints string                 `json:"topologySpreadConstraints"`
	NodeSelector              string                 `json:"nodeSelector"`
	PriorityClassName         string                 `json:"priorityClassName"`
	Resources                 map[string]interface{} `json:"resources"`
	ExtraVolumes              []interface{}          `json:"extraVolumes"`
}

type Gateway struct {
	Name string `json:"name"`
}

type ConnectInjectConfig struct {
	Enabled bool `json:"enabled"`
}
type SecretsBackend struct {
	Vault Vault `json:"vault"`
}

type Vault struct {
	Enabled          bool              `json:"enabled"`
	ConsulCARole     string            `json:"consulCARole"`
	CA               VaultCA           `json:"ca"`
	AgentAnnotations map[string]string `json:"agentAnnotations,omitempty"`
	VaultNamespace   string            `json:"vaultNamespace,omitempty"`
}

type VaultCA struct {
	SecretName string `json:"secretName,omitempty"`
	SecretKey  string `json:"secretKey,omitempty"`
}

type ExternalServersConfig struct {
	Enabled         bool     `json:"enabled"`
	Hosts           []string `json:"hosts"`
	HTTPSPort       int      `json:"httpsPort"`
	GRPCPort        int      `json:"grpcPort"`
	TLSServerName   string   `json:"tlsServerName,omitempty"`
	UseSystemRoots  bool     `json:"useSystemRoots"`
	SkipServerWatch bool     `json:"skipServerWatch"`
}
