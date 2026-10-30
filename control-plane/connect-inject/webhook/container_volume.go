// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
)

// volumeName is the name of the volume that is created to store the
// Consul Connect injection data.
const volumeName = "consul-connect-inject-data"
const accessLogVolumeName = "envoy-access-logs"

// containerVolume returns the volume data to add to the pod. This volume
// is used for shared data between containers.
func (w *MeshWebhook) containerVolume() corev1.Volume {
	return corev1.Volume{
		Name: volumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
		},
	}
}

func accessLogVolume() corev1.Volume {
	return corev1.Volume{
		Name: accessLogVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumDefault},
		},
	}
}

func accessLogVolumeMount(path string) corev1.VolumeMount {
	return corev1.VolumeMount{
		Name:      accessLogVolumeName,
		MountPath: filepath.Dir(path),
	}
}
