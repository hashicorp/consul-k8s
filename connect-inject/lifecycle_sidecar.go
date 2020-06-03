package connectinject

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"strings"
)

const (
	lifecycleContainerCPULimit      = "10m"
	lifecycleContainerCPURequest    = "10m"
	lifecycleContainerMemoryLimit   = "25Mi"
	lifecycleContainerMemoryRequest = "25Mi"
)

func (h *Handler) lifecycleSidecar(pod *corev1.Pod) corev1.Container {
	command := []string{
		"consul-k8s",
		"lifecycle-sidecar",
		"-service-config", "/consul/connect-inject/service.hcl",
		"-consul-binary", "/consul/connect-inject/consul",
	}
	if h.AuthMethod != "" {
		command = append(command, "-token-file=/consul/connect-inject/acl-token")
	}

	if period, ok := pod.Annotations[annotationSyncPeriod]; ok {
		command = append(command, "-sync-period="+strings.TrimSpace(period))
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

	resources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(lifecycleContainerCPULimit),
			corev1.ResourceMemory: resource.MustParse(lifecycleContainerMemoryLimit),
		},
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    resource.MustParse(lifecycleContainerCPURequest),
			corev1.ResourceMemory: resource.MustParse(lifecycleContainerMemoryRequest),
		},
	}

	return corev1.Container{
		Name:  "consul-connect-lifecycle-sidecar",
		Image: h.ImageConsulK8S,
		Env:   envVariables,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command:   command,
		Resources: resources,
	}
}
