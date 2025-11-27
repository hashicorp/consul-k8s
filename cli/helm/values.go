// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package helm

// HACK this is a temporary hard-coded struct. We should actually generate this from our `values.yaml` file.

// Values is the Helm values that may be set for the Consul Helm Chart.
type Values struct {
	Global              Global              `yaml:"global"`
	Server              Server              `yaml:"server"`
	ExternalServers     ExternalServers     `yaml:"externalServers"`
	Client              Client              `yaml:"client"`
	DNS                 DNS                 `yaml:"dns"`
	UI                  UI                  `yaml:"ui"`
	SyncCatalog         SyncCatalog         `yaml:"syncCatalog"`
	ConnectInject       ConnectInject       `yaml:"connectInject"`
	Controller          Controller          `yaml:"controller"`
	MeshGateway         MeshGateway         `yaml:"meshGateway"`
	IngressGateways     IngressGateways     `yaml:"ingressGateways"`
	TerminatingGateways TerminatingGateways `yaml:"terminatingGateways"`
	APIGateway          APIGateway          `yaml:"apiGateway"`
	WebhookCertManager  WebhookCertManager  `yaml:"webhookCertManager"`
	Prometheus          Prometheus          `yaml:"prometheus"`
	Tests               Tests               `yaml:"tests"`
}

type NodePort struct {
	RPC   interface{} `yaml:"rpc"`
	Serf  interface{} `yaml:"serf"`
	HTTPS interface{} `yaml:"https"`
}

type AdminPartitionsService struct {
	Type        string      `yaml:"type"`
	NodePort    NodePort    `yaml:"nodePort"`
	Annotations interface{} `yaml:"annotations"`
}

type AdminPartitions struct {
	Enabled bool                   `yaml:"enabled"`
	Name    string                 `yaml:"name"`
	Service AdminPartitionsService `yaml:"service"`
}

type Ca struct {
	SecretName string `yaml:"secretName"`
	SecretKey  string `yaml:"secretKey"`
}

type ConnectCA struct {
	Address             string `yaml:"address"`
	AuthMethodPath      string `yaml:"authMethodPath"`
	RootPKIPath         string `yaml:"rootPKIPath"`
	IntermediatePKIPath string `yaml:"intermediatePKIPath"`
	AdditionalConfig    string `yaml:"additionalConfig"`
}

type Vault struct {
	Enabled              bool        `yaml:"enabled"`
	ConsulServerRole     string      `yaml:"consulServerRole"`
	ConsulClientRole     string      `yaml:"consulClientRole"`
	ManageSystemACLsRole string      `yaml:"manageSystemACLsRole"`
	AgentAnnotations     interface{} `yaml:"agentAnnotations"`
	ConsulCARole         string      `yaml:"consulCARole"`
	Ca                   Ca          `yaml:"ca"`
	ConnectCA            ConnectCA   `yaml:"connectCA"`
}

type SecretsBackend struct {
	Vault Vault `yaml:"vault"`
}

type GossipEncryption struct {
	AutoGenerate bool   `yaml:"autoGenerate"`
	SecretName   string `yaml:"secretName"`
	SecretKey    string `yaml:"secretKey"`
}

type CaCert struct {
	SecretName string `yaml:"secretName"`
	SecretKey  string `yaml:"secretKey"`
}

type CaKey struct {
	SecretName string `yaml:"secretName"`
	SecretKey  string `yaml:"secretKey"`
}

type TLS struct {
	Enabled                 bool          `yaml:"enabled"`
	EnableAutoEncrypt       bool          `yaml:"enableAutoEncrypt"`
	ServerAdditionalDNSSANs []interface{} `yaml:"serverAdditionalDNSSANs"`
	ServerAdditionalIPSANs  []interface{} `yaml:"serverAdditionalIPSANs"`
	Verify                  bool          `yaml:"verify"`
	HTTPSOnly               bool          `yaml:"httpsOnly"`
	CaCert                  CaCert        `yaml:"caCert"`
	CaKey                   CaKey         `yaml:"caKey"`
}

type BootstrapToken struct {
	SecretName interface{} `yaml:"secretName"`
	SecretKey  interface{} `yaml:"secretKey"`
}

type ReplicationToken struct {
	SecretName string `yaml:"secretName"`
	SecretKey  string `yaml:"secretKey"`
}

type Acls struct {
	ManageSystemACLs       bool             `yaml:"manageSystemACLs"`
	BootstrapToken         BootstrapToken   `yaml:"bootstrapToken"`
	CreateReplicationToken bool             `yaml:"createReplicationToken"`
	ReplicationToken       ReplicationToken `yaml:"replicationToken"`
}

type EnterpriseLicense struct {
	SecretName            string `yaml:"secretName"`
	SecretKey             string `yaml:"secretKey"`
	EnableLicenseAutoload bool   `yaml:"enableLicenseAutoload"`
}

type Federation struct {
	Enabled                bool          `yaml:"enabled"`
	CreateFederationSecret bool          `yaml:"createFederationSecret"`
	PrimaryDatacenter      string        `yaml:"primaryDatacenter"`
	PrimaryGateways        []interface{} `yaml:"primaryGateways"`
}

type GlobalMetrics struct {
	Enabled                   bool   `yaml:"enabled"`
	EnableAgentMetrics        bool   `yaml:"enableAgentMetrics"`
	AgentMetricsRetentionTime string `yaml:"agentMetricsRetentionTime"`
	EnableGatewayMetrics      bool   `yaml:"enableGatewayMetrics"`
}

type Requests struct {
	Memory string `yaml:"memory"`
	CPU    string `yaml:"cpu"`
}

type Limits struct {
	Memory string `yaml:"memory"`
	CPU    string `yaml:"cpu"`
}

type Resources struct {
	Requests Requests `yaml:"requests"`
	Limits   Limits   `yaml:"limits"`
}

type Openshift struct {
	Enabled bool `yaml:"enabled"`
}

type Global struct {
	Enabled                   bool              `yaml:"enabled"`
	LogLevel                  string            `yaml:"logLevel"`
	LogJSON                   bool              `yaml:"logJSON"`
	Name                      interface{}       `yaml:"name"`
	Domain                    string            `yaml:"domain"`
	AdminPartitions           AdminPartitions   `yaml:"adminPartitions"`
	Image                     string            `yaml:"image"`
	ImagePullSecrets          []interface{}     `yaml:"imagePullSecrets"`
	ImageK8S                  string            `yaml:"imageK8S"`
	Datacenter                string            `yaml:"datacenter"`
	EnablePodSecurityPolicies bool              `yaml:"enablePodSecurityPolicies"`
	SecretsBackend            SecretsBackend    `yaml:"secretsBackend"`
	GossipEncryption          GossipEncryption  `yaml:"gossipEncryption"`
	Recursors                 []interface{}     `yaml:"recursors"`
	TLS                       TLS               `yaml:"tls"`
	EnableConsulNamespaces    bool              `yaml:"enableConsulNamespaces"`
	Acls                      Acls              `yaml:"acls"`
	EnterpriseLicense         EnterpriseLicense `yaml:"enterpriseLicense"`
	Federation                Federation        `yaml:"federation"`
	Metrics                   GlobalMetrics     `yaml:"metrics"`
	ImageEnvoy                string            `yaml:"imageEnvoy"`
	Openshift                 Openshift         `yaml:"openshift"`
}

type ServerCert struct {
	SecretName interface{} `yaml:"secretName"`
}

type Serflan struct {
	Port int `yaml:"port"`
}

type Ports struct {
	Serflan Serflan `yaml:"serflan"`
}

type ServiceAccount struct {
	Annotations interface{} `yaml:"annotations"`
}

type SecurityContext struct {
	RunAsNonRoot bool `yaml:"runAsNonRoot"`
	RunAsGroup   int  `yaml:"runAsGroup"`
	RunAsUser    int  `yaml:"runAsUser"`
	FsGroup      int  `yaml:"fsGroup"`
}

type ContainerSecurityContext struct {
	Server interface{} `yaml:"server"`
}

type DisruptionBudget struct {
	Enabled        bool        `yaml:"enabled"`
	MaxUnavailable interface{} `yaml:"maxUnavailable"`
}

type ServerService struct {
	Annotations interface{} `yaml:"annotations"`
}

type ExtraEnvironmentVars struct {
}

type Server struct {
	Enabled                   string                   `yaml:"enabled"`
	Image                     interface{}              `yaml:"image"`
	Replicas                  int                      `yaml:"replicas"`
	BootstrapExpect           interface{}              `yaml:"bootstrapExpect"`
	ServerCert                ServerCert               `yaml:"serverCert"`
	ExposeGossipAndRPCPorts   bool                     `yaml:"exposeGossipAndRPCPorts"`
	Ports                     Ports                    `yaml:"ports"`
	Storage                   string                   `yaml:"storage"`
	StorageClass              interface{}              `yaml:"storageClass"`
	Connect                   bool                     `yaml:"connect"`
	ServiceAccount            ServiceAccount           `yaml:"serviceAccount"`
	Resources                 Resources                `yaml:"resources"`
	SecurityContext           SecurityContext          `yaml:"securityContext"`
	ContainerSecurityContext  ContainerSecurityContext `yaml:"containerSecurityContext"`
	UpdatePartition           int                      `yaml:"updatePartition"`
	DisruptionBudget          DisruptionBudget         `yaml:"disruptionBudget"`
	ExtraConfig               string                   `yaml:"extraConfig"`
	ExtraVolumes              []interface{}            `yaml:"extraVolumes"`
	ExtraContainers           []interface{}            `yaml:"extraContainers"`
	Affinity                  string                   `yaml:"affinity"`
	Tolerations               string                   `yaml:"tolerations"`
	TopologySpreadConstraints string                   `yaml:"topologySpreadConstraints"`
	NodeSelector              interface{}              `yaml:"nodeSelector"`
	PriorityClassName         string                   `yaml:"priorityClassName"`
	ExtraLabels               interface{}              `yaml:"extraLabels"`
	Annotations               interface{}              `yaml:"annotations"`
	Service                   ServerService            `yaml:"service"`
	ExtraEnvironmentVars      ExtraEnvironmentVars     `yaml:"extraEnvironmentVars"`
}

type ExternalServers struct {
	Enabled           bool          `yaml:"enabled"`
	Hosts             []interface{} `yaml:"hosts"`
	HTTPSPort         int           `yaml:"httpsPort"`
	TLSServerName     interface{}   `yaml:"tlsServerName"`
	UseSystemRoots    bool          `yaml:"useSystemRoots"`
	K8SAuthMethodHost interface{}   `yaml:"k8sAuthMethodHost"`
}

type NodeMeta struct {
	PodName string `yaml:"pod-name"`
	HostIP  string `yaml:"host-ip"`
}

type ClientContainerSecurityContext struct {
	Client  interface{} `yaml:"client"`
	ACLInit interface{} `yaml:"aclInit"`
	TLSInit interface{} `yaml:"tlsInit"`
}

type ConfigSecret struct {
	SecretName interface{} `yaml:"secretName"`
	SecretKey  interface{} `yaml:"secretKey"`
}

type SnapshotAgent struct {
	Enabled        bool           `yaml:"enabled"`
	Replicas       int            `yaml:"replicas"`
	ConfigSecret   ConfigSecret   `yaml:"configSecret"`
	ServiceAccount ServiceAccount `yaml:"serviceAccount"`
	Resources      Resources      `yaml:"resources"`
	CaCert         interface{}    `yaml:"caCert"`
}

type Client struct {
	Enabled                  string                         `yaml:"enabled"`
	Image                    interface{}                    `yaml:"image"`
	Join                     interface{}                    `yaml:"join"`
	DataDirectoryHostPath    interface{}                    `yaml:"dataDirectoryHostPath"`
	Grpc                     bool                           `yaml:"grpc"`
	NodeMeta                 NodeMeta                       `yaml:"nodeMeta"`
	ExposeGossipPorts        bool                           `yaml:"exposeGossipPorts"`
	ServiceAccount           ServiceAccount                 `yaml:"serviceAccount"`
	Resources                Resources                      `yaml:"resources"`
	SecurityContext          SecurityContext                `yaml:"securityContext"`
	ContainerSecurityContext ClientContainerSecurityContext `yaml:"containerSecurityContext"`
	ExtraConfig              string                         `yaml:"extraConfig"`
	ExtraVolumes             []interface{}                  `yaml:"extraVolumes"`
	ExtraContainers          []interface{}                  `yaml:"extraContainers"`
	Tolerations              string                         `yaml:"tolerations"`
	NodeSelector             interface{}                    `yaml:"nodeSelector"`
	Affinity                 interface{}                    `yaml:"affinity"`
	PriorityClassName        string                         `yaml:"priorityClassName"`
	Annotations              interface{}                    `yaml:"annotations"`
	ExtraLabels              interface{}                    `yaml:"extraLabels"`
	ExtraEnvironmentVars     ExtraEnvironmentVars           `yaml:"extraEnvironmentVars"`
	DNSPolicy                interface{}                    `yaml:"dnsPolicy"`
	HostNetwork              bool                           `yaml:"hostNetwork"`
	UpdateStrategy           interface{}                    `yaml:"updateStrategy"`
	SnapshotAgent            SnapshotAgent                  `yaml:"snapshotAgent"`
}

type DNS struct {
	Enabled           string      `yaml:"enabled"`
	EnableRedirection bool        `yaml:"enableRedirection"`
	Type              string      `yaml:"type"`
	ClusterIP         interface{} `yaml:"clusterIP"`
	Annotations       interface{} `yaml:"annotations"`
	AdditionalSpec    interface{} `yaml:"additionalSpec"`
}

type Port struct {
	HTTP  int `yaml:"http"`
	HTTPS int `yaml:"https"`
}

type ServiceNodePort struct {
	HTTP  interface{} `yaml:"http"`
	HTTPS interface{} `yaml:"https"`
}

type UIService struct {
	Enabled        bool            `yaml:"enabled"`
	Type           interface{}     `yaml:"type"`
	Port           Port            `yaml:"port"`
	NodePort       ServiceNodePort `yaml:"nodePort"`
	Annotations    interface{}     `yaml:"annotations"`
	AdditionalSpec interface{}     `yaml:"additionalSpec"`
}
type Ingress struct {
	Enabled          bool          `yaml:"enabled"`
	IngressClassName string        `yaml:"ingressClassName"`
	PathType         string        `yaml:"pathType"`
	Hosts            []interface{} `yaml:"hosts"`
	TLS              []interface{} `yaml:"tls"`
	Annotations      interface{}   `yaml:"annotations"`
}

type UIMetrics struct {
	Enabled  string `yaml:"enabled"`
	Provider string `yaml:"provider"`
	BaseURL  string `yaml:"baseURL"`
}

type DashboardURLTemplates struct {
	Service string `yaml:"service"`
}

type UI struct {
	Enabled               string                `yaml:"enabled"`
	Service               UIService             `yaml:"service"`
	Ingress               Ingress               `yaml:"ingress"`
	Metrics               UIMetrics             `yaml:"metrics"`
	DashboardURLTemplates DashboardURLTemplates `yaml:"dashboardURLTemplates"`
}

type ConsulNamespaces struct {
	ConsulDestinationNamespace string `yaml:"consulDestinationNamespace"`
	MirroringK8S               bool   `yaml:"mirroringK8S"`
	MirroringK8SPrefix         string `yaml:"mirroringK8SPrefix"`
}

type ACLSyncToken struct {
	SecretName interface{} `yaml:"secretName"`
	SecretKey  interface{} `yaml:"secretKey"`
}

type SyncCatalog struct {
	Enabled               bool             `yaml:"enabled"`
	Image                 interface{}      `yaml:"image"`
	Default               bool             `yaml:"default"`
	PriorityClassName     string           `yaml:"priorityClassName"`
	ToConsul              bool             `yaml:"toConsul"`
	ToK8S                 bool             `yaml:"toK8S"`
	K8SPrefix             interface{}      `yaml:"k8sPrefix"`
	K8SAllowNamespaces    []string         `yaml:"k8sAllowNamespaces"`
	K8SDenyNamespaces     []string         `yaml:"k8sDenyNamespaces"`
	K8SSourceNamespace    interface{}      `yaml:"k8sSourceNamespace"`
	ConsulNamespaces      ConsulNamespaces `yaml:"consulNamespaces"`
	AddK8SNamespaceSuffix bool             `yaml:"addK8SNamespaceSuffix"`
	ConsulPrefix          interface{}      `yaml:"consulPrefix"`
	K8STag                interface{}      `yaml:"k8sTag"`
	ConsulNodeName        string           `yaml:"consulNodeName"`
	SyncClusterIPServices bool             `yaml:"syncClusterIPServices"`
	NodePortSyncType      string           `yaml:"nodePortSyncType"`
	ACLSyncToken          ACLSyncToken     `yaml:"aclSyncToken"`
	NodeSelector          interface{}      `yaml:"nodeSelector"`
	Affinity              interface{}      `yaml:"affinity"`
	Tolerations           interface{}      `yaml:"tolerations"`
	ServiceAccount        ServiceAccount   `yaml:"serviceAccount"`
	Resources             Resources        `yaml:"resources"`
	LogLevel              string           `yaml:"logLevel"`
	ConsulWriteInterval   interface{}      `yaml:"consulWriteInterval"`
	ExtraLabels           interface{}      `yaml:"extraLabels"`
}

type TransparentProxy struct {
	DefaultEnabled         bool `yaml:"defaultEnabled"`
	DefaultOverwriteProbes bool `yaml:"defaultOverwriteProbes"`
}

type Metrics struct {
	DefaultEnabled              bool   `yaml:"defaultEnabled"`
	DefaultEnableMerging        bool   `yaml:"defaultEnableMerging"`
	DefaultMergedMetricsPort    int    `yaml:"defaultMergedMetricsPort"`
	DefaultPrometheusScrapePort int    `yaml:"defaultPrometheusScrapePort"`
	DefaultPrometheusScrapePath string `yaml:"defaultPrometheusScrapePath"`
}

type ACLInjectToken struct {
	SecretName interface{} `yaml:"secretName"`
	SecretKey  interface{} `yaml:"secretKey"`
}

type SidecarProxy struct {
	Resources Resources `yaml:"resources"`
	Lifecycle Lifecycle `yaml:"lifecycle"`
}

type InitContainer struct {
	Resources Resources `yaml:"resources"`
}

type Lifecycle struct {
	DefaultEnabled                      bool   `yaml:"defaultEnabled"`
	DefaultEnableShutdownDrainListeners bool   `yaml:"defaultEnableShutdownDrainListeners"`
	DefaultShutdownGracePeriodSeconds   int    `yaml:"defaultShutdownGracePeriodSeconds"`
	DefaultGracefulPort                 int    `yaml:"defaultGracefulPort"`
	DefaultGracefulShutdownPath         string `yaml:"defaultGracefulShutdownPath"`
	DefaultStartupGracePeriodSeconds    int    `yaml:"defaultStartupGracePeriodSeconds"`
	DefaultGracefulStartupPath          string `yaml:"defaultGracefulStartupPath"`
}

type ConnectInject struct {
	Enabled                bool             `yaml:"enabled"`
	Replicas               int              `yaml:"replicas"`
	Image                  interface{}      `yaml:"image"`
	Default                bool             `yaml:"default"`
	TransparentProxy       TransparentProxy `yaml:"transparentProxy"`
	Metrics                Metrics          `yaml:"metrics"`
	EnvoyExtraArgs         interface{}      `yaml:"envoyExtraArgs"`
	PriorityClassName      string           `yaml:"priorityClassName"`
	ImageConsul            interface{}      `yaml:"imageConsul"`
	LogLevel               string           `yaml:"logLevel"`
	ServiceAccount         ServiceAccount   `yaml:"serviceAccount"`
	Resources              Resources        `yaml:"resources"`
	FailurePolicy          string           `yaml:"failurePolicy"`
	NamespaceSelector      string           `yaml:"namespaceSelector"`
	K8SAllowNamespaces     []string         `yaml:"k8sAllowNamespaces"`
	K8SDenyNamespaces      []interface{}    `yaml:"k8sDenyNamespaces"`
	ConsulNamespaces       ConsulNamespaces `yaml:"consulNamespaces"`
	NodeSelector           interface{}      `yaml:"nodeSelector"`
	Affinity               interface{}      `yaml:"affinity"`
	Tolerations            interface{}      `yaml:"tolerations"`
	ACLBindingRuleSelector string           `yaml:"aclBindingRuleSelector"`
	OverrideAuthMethodName string           `yaml:"overrideAuthMethodName"`
	ACLInjectToken         ACLInjectToken   `yaml:"aclInjectToken"`
	SidecarProxy           SidecarProxy     `yaml:"sidecarProxy"`
	InitContainer          InitContainer    `yaml:"initContainer"`
}

type ACLToken struct {
	SecretName interface{} `yaml:"secretName"`
	SecretKey  interface{} `yaml:"secretKey"`
}

type Controller struct {
	Enabled           bool           `yaml:"enabled"`
	Replicas          int            `yaml:"replicas"`
	LogLevel          string         `yaml:"logLevel"`
	ServiceAccount    ServiceAccount `yaml:"serviceAccount"`
	Resources         Resources      `yaml:"resources"`
	NodeSelector      interface{}    `yaml:"nodeSelector"`
	Tolerations       interface{}    `yaml:"tolerations"`
	Affinity          interface{}    `yaml:"affinity"`
	PriorityClassName string         `yaml:"priorityClassName"`
	ACLToken          ACLToken       `yaml:"aclToken"`
}

type WanAddress struct {
	Source string `yaml:"source"`
	Port   int    `yaml:"port"`
	Static string `yaml:"static"`
}

type InitServiceInitContainer struct {
	Resources Resources `yaml:"resources"`
}

type MeshGateway struct {
	Enabled                  bool                     `yaml:"enabled"`
	Replicas                 int                      `yaml:"replicas"`
	WanAddress               WanAddress               `yaml:"wanAddress"`
	Service                  Service                  `yaml:"service"`
	HostNetwork              bool                     `yaml:"hostNetwork"`
	DNSPolicy                interface{}              `yaml:"dnsPolicy"`
	ConsulServiceName        string                   `yaml:"consulServiceName"`
	ContainerPort            int                      `yaml:"containerPort"`
	HostPort                 interface{}              `yaml:"hostPort"`
	ServiceAccount           ServiceAccount           `yaml:"serviceAccount"`
	Resources                Resources                `yaml:"resources"`
	InitServiceInitContainer InitServiceInitContainer `yaml:"initServiceInitContainer"`
	Affinity                 string                   `yaml:"affinity"`
	Tolerations              interface{}              `yaml:"tolerations"`
	NodeSelector             interface{}              `yaml:"nodeSelector"`
	PriorityClassName        string                   `yaml:"priorityClassName"`
	Annotations              interface{}              `yaml:"annotations"`
}

type ServicePorts struct {
	Port     int         `yaml:"port"`
	NodePort interface{} `yaml:"nodePort"`
}

type DefaultsService struct {
	Type           string         `yaml:"type"`
	Ports          []ServicePorts `yaml:"ports"`
	Annotations    interface{}    `yaml:"annotations"`
	AdditionalSpec interface{}    `yaml:"additionalSpec"`
}

type IngressGatewayDefaults struct {
	Replicas                      int             `yaml:"replicas"`
	Service                       DefaultsService `yaml:"service"`
	ServiceAccount                ServiceAccount  `yaml:"serviceAccount"`
	Resources                     Resources       `yaml:"resources"`
	Affinity                      string          `yaml:"affinity"`
	Tolerations                   interface{}     `yaml:"tolerations"`
	NodeSelector                  interface{}     `yaml:"nodeSelector"`
	PriorityClassName             string          `yaml:"priorityClassName"`
	TerminationGracePeriodSeconds int             `yaml:"terminationGracePeriodSeconds"`
	Annotations                   interface{}     `yaml:"annotations"`
	ConsulNamespace               string          `yaml:"consulNamespace"`
}

type Gateways struct {
	Name string `yaml:"name"`
}

type IngressGateways struct {
	Enabled  bool                   `yaml:"enabled"`
	Defaults IngressGatewayDefaults `yaml:"defaults"`
	Gateways []Gateways             `yaml:"gateways"`
}

type Defaults struct {
	Replicas          int            `yaml:"replicas"`
	ExtraVolumes      []interface{}  `yaml:"extraVolumes"`
	Resources         Resources      `yaml:"resources"`
	Affinity          string         `yaml:"affinity"`
	Tolerations       interface{}    `yaml:"tolerations"`
	NodeSelector      interface{}    `yaml:"nodeSelector"`
	PriorityClassName string         `yaml:"priorityClassName"`
	Annotations       interface{}    `yaml:"annotations"`
	ServiceAccount    ServiceAccount `yaml:"serviceAccount"`
	ConsulNamespace   string         `yaml:"consulNamespace"`
}

type TerminatingGateways struct {
	Enabled  bool       `yaml:"enabled"`
	Defaults Defaults   `yaml:"defaults"`
	Gateways []Gateways `yaml:"gateways"`
}

type CopyAnnotations struct {
	Service interface{} `yaml:"service"`
}

type ManagedGatewayClass struct {
	Enabled                     bool                      `yaml:"enabled"`
	NodeSelector                interface{}               `yaml:"nodeSelector"`
	ServiceType                 string                    `yaml:"serviceType"`
	UseHostPorts                bool                      `yaml:"useHostPorts"`
	CopyAnnotations             CopyAnnotations           `yaml:"copyAnnotations"`
	OpenshiftSCCName            string                    `yaml:"openshiftSCCName"`
	MapPrivilegedContainerPorts int                       `yaml:"mapPrivilegedContainerPorts"`
	Probes                      ManagedGatewayClassProbes `yaml:"probes"`
}

// ProbeHTTPGet models the HTTP GET action of a Kubernetes Probe.
type ProbeHTTPGet struct {
	Path   string      `yaml:"path"`
	Port   interface{} `yaml:"port"` // int or string (named port)
	Host   string      `yaml:"host"`
	Scheme string      `yaml:"scheme"`
	// Headers intentionally omitted for now; can be added if needed.
}

// ProbeTCPSocket models the TCP socket action of a Kubernetes Probe.
type ProbeTCPSocket struct {
	Port interface{} `yaml:"port"` // int or string
	Host string      `yaml:"host"`
}

// ProbeExec models the exec action of a Kubernetes Probe.
type ProbeExec struct {
	Command []string `yaml:"command"`
}

// ProbeSpec is a simplified Kubernetes-style Probe configuration allowing http, tcp, or exec.
// Only one of HTTPGet, TCPSocket, or Exec should be set. Enabled defaults to true if any action is specified.
type ProbeSpec struct {
	Enabled             *bool           `yaml:"enabled"`
	HTTPGet             *ProbeHTTPGet   `yaml:"httpGet"`
	TCPSocket           *ProbeTCPSocket `yaml:"tcpSocket"`
	Exec                *ProbeExec      `yaml:"exec"`
	InitialDelaySeconds int             `yaml:"initialDelaySeconds"`
	PeriodSeconds       int             `yaml:"periodSeconds"`
	TimeoutSeconds      int             `yaml:"timeoutSeconds"`
	SuccessThreshold    int             `yaml:"successThreshold"`
	FailureThreshold    int             `yaml:"failureThreshold"`
}

// ManagedGatewayClassProbes groups the three standard Kubernetes probe types applied to gateway pods.
type ManagedGatewayClassProbes struct {
	Liveness  ProbeSpec `yaml:"liveness"`
	Readiness ProbeSpec `yaml:"readiness"`
	Startup   ProbeSpec `yaml:"startup"`
}

type Service struct {
	Annotations interface{} `yaml:"annotations"`
}

type APIGatewayController struct {
	Replicas          int         `yaml:"replicas"`
	Annotations       interface{} `yaml:"annotations"`
	PriorityClassName string      `yaml:"priorityClassName"`
	NodeSelector      interface{} `yaml:"nodeSelector"`
	Service           Service     `yaml:"service"`
}

type APIGateway struct {
	Enabled             bool                 `yaml:"enabled"`
	Image               interface{}          `yaml:"image"`
	LogLevel            string               `yaml:"logLevel"`
	ManagedGatewayClass ManagedGatewayClass  `yaml:"managedGatewayClass"`
	ConsulNamespaces    ConsulNamespaces     `yaml:"consulNamespaces"`
	ServiceAccount      ServiceAccount       `yaml:"serviceAccount"`
	Controller          APIGatewayController `yaml:"controller"`
}

type WebhookCertManager struct {
	Tolerations interface{} `yaml:"tolerations"`
}

type Prometheus struct {
	Enabled bool `yaml:"enabled"`
}

type Tests struct {
	Enabled bool `yaml:"enabled"`
}
