// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

// AmbientMeshEnabled returns true if the pod should be served by the node-level ambient proxy
// instead of an injected sidecar. The pod annotation takes precedence over the namespace label.
// When the feature is disabled cluster-wide, it always returns false.
func AmbientMeshEnabled(namespace corev1.Namespace, pod corev1.Pod, featureEnabled bool) (bool, error) {
	if !featureEnabled {
		return false, nil
	}
	if raw, ok := pod.Annotations[constants.KeyAmbientMesh]; ok {
		return strconv.ParseBool(raw)
	}
	if raw, ok := namespace.Labels[constants.KeyAmbientMesh]; ok {
		return strconv.ParseBool(raw)
	}
	return false, nil
}

// IsAmbientPod returns true if the webhook enrolled the pod in the ambient mesh.
func IsAmbientPod(pod corev1.Pod) bool {
	return pod.Labels[constants.KeyAmbientMesh] == constants.LabelAmbientMeshEnabled &&
		pod.Annotations[constants.KeyAmbientMeshStatus] != ""
}
