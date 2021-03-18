package connectinject

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func (h *Handler) consulSidecar(pod corev1.Pod) (corev1.Container, error) {
	command := []string{
		"consul-k8s",
		"consul-sidecar",
		"-service-config", "/consul/connect-inject/service.hcl",
		"-consul-binary", "/consul/connect-inject/consul",
	}
	if h.AuthMethod != "" {
		command = append(command, "-token-file=/consul/connect-inject/acl-token")
	}

	if period, ok := pod.Annotations[annotationSyncPeriod]; ok {
		command = append(command, "-sync-period="+strings.TrimSpace(period))
	}

	run, err := h.shouldRunMergedMetricsServer(pod)
	if err != nil {
		return corev1.Container{}, err
	}

	// If we need to run the merged metrics server, configure consul
	// sidecar with the appropriate metrics flags.
	if run {
		mergedMetricsPort, err := h.mergedMetricsPort(pod)
		if err != nil {
			return corev1.Container{}, err
		}
		serviceMetricsPath := h.serviceMetricsPath(pod)

		// Don't need to check the error since it's checked in the call to
		// h.shouldRunMergedMetricsServer() above.
		serviceMetricsPort, _ := h.serviceMetricsPort(pod)

		command = append(command, []string{
			"-enable-metrics-merging=true",
			fmt.Sprintf("-merged-metrics-port=%s", mergedMetricsPort),
			fmt.Sprintf("-service-metrics-port=%s", serviceMetricsPort),
			fmt.Sprintf("-service-metrics-path=%s", serviceMetricsPath),
		}...)
	}

	envVariables := []corev1.EnvVar{
		{
			Name: "HOST_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
			},
		},
	}

	if h.ConsulCACert != "" {
		envVariables = append(envVariables,
			// Kubernetes will interpolate HOST_IP when creating this environment
			// variable.
			corev1.EnvVar{
				Name:  "CONSUL_HTTP_ADDR",
				Value: "https://$(HOST_IP):8501",
			},
			corev1.EnvVar{
				Name:  "CONSUL_CACERT",
				Value: "/consul/connect-inject/consul-ca.pem",
			},
		)
	} else {
		envVariables = append(envVariables,
			// Kubernetes will interpolate HOST_IP when creating this environment
			// variable.
			corev1.EnvVar{
				Name:  "CONSUL_HTTP_ADDR",
				Value: "$(HOST_IP):8500",
			})
	}

	return corev1.Container{
		Name:  "consul-sidecar",
		Image: h.ImageConsulK8S,
		Env:   envVariables,
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
