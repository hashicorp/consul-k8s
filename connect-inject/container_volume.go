package connectinject

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

const (
	// volumeName is the name of the volume that is created to store the
	// Consul Connect injection data.
	volumeName = "consul-connect-inject-data"
	// volumeNameCA is the name of the volume that is created to store the
	// provided CA certificate if configured with TLS support.
	volumeNameCA = "consul-tls-ca"
)

// containerVolume returns the volume data to add to the pod. This volume
// is used for shared data between containers.
func (h *Handler) containerVolume() corev1.Volume {
	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	}
}

// containerVolumeCA returns the volume data to add to the pod. This volume
// is used if a CA certificate secret is defined for use with Consul.
func (h *Handler) containerVolumeCA() corev1.Volume {
	var parts []string

	if parts = strings.SplitN(h.ConsulCACert, ":", 2); len(parts) < 2 {
		panic(fmt.Sprintf("Expected valid secret name and mount path for ConsulCACert. Got: %s", h.ConsulCACert))
	}

	return corev1.Volume{
		Name: volumeNameCA,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: parts[0],
			},
		},
	}
}
