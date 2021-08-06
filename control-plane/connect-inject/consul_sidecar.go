package connectinject

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// consulSidecar starts the consul-sidecar command to only run
// the metrics merging server when metrics merging feature is enabled.
// It always disables service registration because for connect we no longer
// need to keep services registered as this is handled in the endpoints-controller.
func (h *Handler) consulSidecar(pod corev1.Pod) (corev1.Container, error) {
	metricsPorts, err := h.MetricsConfig.mergedMetricsServerConfiguration(pod)
	if err != nil {
		return corev1.Container{}, err
	}

	command := []string{
		"consul-k8s-control-plane",
		"consul-sidecar",
		"-enable-service-registration=false",
		"-enable-metrics-merging=true",
		fmt.Sprintf("-merged-metrics-port=%s", metricsPorts.mergedPort),
		fmt.Sprintf("-service-metrics-port=%s", metricsPorts.servicePort),
		fmt.Sprintf("-service-metrics-path=%s", metricsPorts.servicePath),
		fmt.Sprintf("-log-level=%s", h.LogLevel),
		fmt.Sprintf("-log-json=%t", h.LogJSON),
	}

	return corev1.Container{
		Name:  "consul-sidecar",
		Image: h.ImageConsulK8S,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command:   command,
		Resources: h.ConsulSidecarResources,
	}, nil
}
