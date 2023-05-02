package apigateway

type HelmConfig struct {
	Image           string
	Replicas        int32
	LogLevel        string
	NodeSelector    map[string]string
	Tolerations     map[string]string
	ServiceType     string
	UseHostPorts    bool
	CopyAnnotations map[string]string
	MaxInstances    int32
	MinInstances    int32
}
