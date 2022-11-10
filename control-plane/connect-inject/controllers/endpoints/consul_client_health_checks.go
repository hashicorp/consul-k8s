package endpoints

import (
	"fmt"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	"github.com/hashicorp/consul-k8s/control-plane/consul"
	"github.com/hashicorp/consul-server-connection-manager/discovery"
	"github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-version"
	corev1 "k8s.io/api/core/v1"
)

const minSupportedConsulDataplaneVersion = "v1.0.0-beta1"

// isConsulDataplaneSupported returns true if the consul-k8s version on the pod supports
// consul-dataplane architecture of Consul.
func isConsulDataplaneSupported(pod corev1.Pod) bool {
	if anno, ok := pod.Annotations[constants.AnnotationConsulK8sVersion]; ok {
		consulK8sVersion, err := version.NewVersion(anno)
		if err != nil {
			return false
		}
		consulDPSupportedVersion, err := version.NewVersion(minSupportedConsulDataplaneVersion)
		if err != nil {
			return false
		}
		if !consulK8sVersion.LessThan(consulDPSupportedVersion) {
			return true
		}
	}
	return false
}

func (r *Controller) consulClientCfgForNodeAgent(serverClient *api.Client, pod corev1.Pod, state discovery.State) (*api.Config, error) {
	ccCfg := &api.Config{
		Scheme: r.ConsulClientConfig.APIClientConfig.Scheme,
	}

	consulClientHttpPort := 8500
	if ccCfg.Scheme == "https" {
		consulClientHttpPort = 8501
		ccCfg.TLSConfig.CAFile = r.ConsulClientConfig.APIClientConfig.TLSConfig.CAFile
	}
	if r.consulClientHttpPort != 0 {
		consulClientHttpPort = r.consulClientHttpPort
	}
	ccCfg.Address = fmt.Sprintf("%s:%d", pod.Status.HostIP, consulClientHttpPort)

	ccCfg.Token = state.Token

	// Check if auto-encrypt is enabled. If it is, we need to retrieve and set a different CA for the Consul client.
	if r.EnableAutoEncrypt {
		// Get Connect CA.
		caRoots, _, err := serverClient.Agent().ConnectCARoots(nil)
		if err != nil {
			return nil, err
		}
		if caRoots == nil {
			return nil, fmt.Errorf("ca root list is nil")
		}
		if caRoots.Roots == nil {
			return nil, fmt.Errorf("ca roots is nil")
		}
		if len(caRoots.Roots) == 0 {
			return nil, fmt.Errorf("the list of root CAs is empty")
		}

		for _, root := range caRoots.Roots {
			if root.Active {
				ccCfg.TLSConfig.CAFile = ""
				ccCfg.TLSConfig.CAPem = []byte(root.RootCertPEM)
				break
			}
		}
	}
	if r.EnableConsulNamespaces {
		ccCfg.Namespace = r.consulNamespace(pod.Namespace)
	}
	return ccCfg, nil
}

func (r *Controller) updateHealthCheckOnConsulClient(consulClientCfg *api.Config, pod corev1.Pod, endpoints corev1.Endpoints, status string) error {
	consulClient, err := consul.NewClient(consulClientCfg, r.ConsulClientConfig.APITimeout)
	if err != nil {
		return err
	}
	filter := fmt.Sprintf(`Name == "Kubernetes Health Check" and ServiceID == %q`, serviceID(pod, endpoints))
	checks, err := consulClient.Agent().ChecksWithFilter(filter)
	if err != nil {
		return err
	}
	if len(checks) > 1 {
		return fmt.Errorf("more than one Kubernetes health check found")
	}
	if len(checks) == 0 {
		r.Log.Info("detected no health checks to update", "name", endpoints.Name, "ns", endpoints.Namespace, "service-id", serviceID(pod, endpoints))
		return nil
	}
	for checkID := range checks {
		output := "Kubernetes health checks passing"
		if status == api.HealthCritical {
			output = fmt.Sprintf(`Pod "%s/%s" is not ready`, pod.Namespace, pod.Name)
		}
		r.Log.Info("updating health check status", "name", endpoints.Name, "ns", endpoints.Namespace, "status", status)
		err = consulClient.Agent().UpdateTTL(checkID, output, status)
		if err != nil {
			return err
		}
	}
	return nil
}
