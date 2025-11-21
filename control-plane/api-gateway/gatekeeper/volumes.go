// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"path/filepath"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
)

// volumesAndMounts generates the list of volumes for the Deployment and the list of volume
// mounts for the primary container in the Deployment. There are two volumes that are created:
// - one empty volume for holding connect-inject data
// - one volume holding all TLS certificates referenced by the Gateway.
func volumesAndMounts(gateway v1beta1.Gateway) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := []corev1.Volume{
		{
			Name: volumeNameForConnectInject,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
			},
		},
		{
			Name: volumeNameForTLSCerts,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName:  gateway.Name,
					DefaultMode: common.PointerTo(int32(444)),
					Optional:    common.PointerTo(false),
				},
			},
		},
	}

	mounts := []corev1.VolumeMount{
		{
			Name:      volumeNameForConnectInject,
			MountPath: "/consul/connect-inject",
		},
		{
			Name:      volumeNameForTLSCerts,
			MountPath: "/consul/gateway-certificates",
		},
	}

	return volumes, mounts
}

const accessLogVolumeName = "envoy-access-logs"

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
