package connectinject

import (
	corev1 "k8s.io/api/core/v1"
)

// containerInit returns the init container spec for registering the Consul
// service, setting up the Envoy bootstrap, etc.
func (h *Handler) getConsulInitContainer(pod *corev1.Pod, k8sNamespace string) (corev1.Container, error) {

	// Create expected volume mounts
	volMounts := []corev1.VolumeMount{
		corev1.VolumeMount{
			Name:      volumeName,
			MountPath: "/consul/connect-inject",
		},
	}

	return corev1.Container{
		Name:  "consul-init-container",
		Image: h.ImageConsulK8S,
		Env: []corev1.EnvVar{
			{
				Name: "HOST_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
				},
			},
			{
				// We need this to make agent calls!
				// TODO: does this even work for https??
				Name:  "CONSUL_HTTP_ADDR",
				Value: "$(HOST_IP):8500",
			},
			{
				Name: "POD_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
				},
			},
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			},
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
				},
			},
		},
		VolumeMounts: volMounts,
		Command:      []string{"/bin/sh", "-ec", "consul-k8s consul-init"},
	}, nil
}
