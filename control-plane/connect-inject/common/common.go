package common

import (
	"strconv"
	"strings"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	corev1 "k8s.io/api/core/v1"
)

// PortValue returns the port of the container for the string value passed
// in as an argument on the provided pod.
func PortValue(pod corev1.Pod, value string) (int32, error) {
	value = strings.Split(value, ",")[0]
	// First search for the named port.
	for _, c := range pod.Spec.Containers {
		for _, p := range c.Ports {
			if p.Name == value {
				return p.ContainerPort, nil
			}
		}
	}

	// Named port not found, return the parsed value.
	raw, err := strconv.ParseInt(value, 0, 32)
	return int32(raw), err
}

// TransparentProxyEnabled returns true if transparent proxy should be enabled for this pod.
// It returns an error when the annotation value cannot be parsed by strconv.ParseBool or if we are unable
// to read the pod's namespace label when it exists.
func TransparentProxyEnabled(namespace corev1.Namespace, pod corev1.Pod, globalEnabled bool) (bool, error) {
	// First check to see if the pod annotation exists to override the namespace or global settings.
	if raw, ok := pod.Annotations[constants.KeyTransparentProxy]; ok {
		return strconv.ParseBool(raw)
	}
	// Next see if the namespace has been defaulted.
	if raw, ok := namespace.Labels[constants.KeyTransparentProxy]; ok {
		return strconv.ParseBool(raw)
	}
	// Else fall back to the global default.
	return globalEnabled, nil
}

// ShouldOverwriteProbes returns true if we need to overwrite readiness/liveness probes for this pod.
// It returns an error when the annotation value cannot be parsed by strconv.ParseBool.
func ShouldOverwriteProbes(pod corev1.Pod, globalOverwrite bool) (bool, error) {
	if raw, ok := pod.Annotations[constants.AnnotationTransparentProxyOverwriteProbes]; ok {
		return strconv.ParseBool(raw)
	}

	return globalOverwrite, nil
}
