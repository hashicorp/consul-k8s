// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/consul-k8s/control-plane/api-gateway/common"
	"golang.org/x/exp/maps"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/gateway-api/apis/v1beta1"
)

// volumesAndMounts generates the list of volumes for the Deployment and the list of volume
// mounts for the primary container in the Deployment. In addition to the "default" volume
// containing connect-inject data, there will be one volume + mount per unique Secret
// referenced in the Gateway's listener TLS configurations. The volume references the Secret
// directly.
func volumesAndMounts(gateway v1beta1.Gateway) ([]corev1.Volume, []corev1.VolumeMount) {
	volumes := map[string]corev1.Volume{
		volumeNameForConnectInject: {
			Name: volumeNameForConnectInject,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory},
			},
		},
	}
	mounts := map[string]corev1.VolumeMount{
		volumeNameForConnectInject: {
			Name:      volumeNameForConnectInject,
			MountPath: "/consul/connect-inject",
		},
	}

	for i, listener := range gateway.Spec.Listeners {
		if listener.TLS == nil {
			continue
		}

		for j, ref := range listener.TLS.CertificateRefs {
			refNamespace := common.ValueOr(ref.Namespace, gateway.Namespace)

			volumeName := fmt.Sprintf("listener-%d-cert-%d-volume", i, j)

			volumes[volumeName] = corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName:  string(ref.Name),
						DefaultMode: common.PointerTo(int32(444)),
						Optional:    common.PointerTo(false),
					},
				},
			}

			mounts[volumeName] = corev1.VolumeMount{
				Name:      volumeName,
				MountPath: fmt.Sprintf("/consul/gateway-certificates/%s/%s", refNamespace, ref.Name),
			}
		}
	}

	vols := maps.Values(volumes)
	slices.SortFunc(vols, func(a, b corev1.Volume) int {
		return strings.Compare(a.Name, b.Name)
	})

	mts := maps.Values(mounts)
	slices.SortFunc(mts, func(a, b corev1.VolumeMount) int {
		return strings.Compare(a.Name, b.Name)
	})

	return vols, mts
}
