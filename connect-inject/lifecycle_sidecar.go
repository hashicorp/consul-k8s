package connectinject

import (
	corev1 "k8s.io/api/core/v1"
	"strings"
)

func (h *Handler) lifecycleSidecar(pod *corev1.Pod) corev1.Container {
	command := []string{
		"consul-k8s",
		"lifecycle-sidecar",
		"-service-config", "/consul/connect-inject/service.hcl",
	}
	if h.AuthMethod != "" {
		command = append(command, "-token-file=/consul/connect-inject/acl-token")
	}
	if h.ConsulCACert != "" {
		command = append(command, "-http-addr", "https://${HOST_IP}:8501")
		command = append(command, "-ca-file", "/consul/connect-inject/consul-ca.pem")
	} else {
		command = append(command, "-http-addr", "${HOST_IP}:8500")
	}
	if period, ok := pod.Annotations[annotationSyncPeriod]; ok {
		command = append(command, "-sync-period="+strings.TrimSpace(period))
	}

	return corev1.Container{
		Name:  "consul-connect-lifecycle-sidecar",
		Image: h.ImageConsulK8S,
		Env: []corev1.EnvVar{
			{
				Name: "HOST_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command: command,
	}
}
