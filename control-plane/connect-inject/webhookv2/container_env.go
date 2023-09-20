// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhookv2

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	corev1 "k8s.io/api/core/v1"
)

func (w *MeshWebhook) containerEnvVars(pod corev1.Pod) []corev1.EnvVar {
	raw, ok := pod.Annotations[constants.AnnotationMeshDestinations]
	if !ok || raw == "" {
		return []corev1.EnvVar{}
	}

	var result []corev1.EnvVar
	for _, raw := range strings.Split(raw, ",") {
		parts := strings.SplitN(raw, ":", 3)
		port, _ := common.PortValue(pod, strings.TrimSpace(parts[1]))
		if port > 0 {
			service := parts[0]
			pieces := strings.Split(service, ".")
			serviceName := strings.TrimSpace(pieces[1])
			serviceName = strings.ToUpper(strings.Replace(serviceName, "-", "_", -1))
			portName := strings.TrimSpace(pieces[0])
			portName = strings.ToUpper(strings.Replace(portName, "-", "_", -1))
			portStr := strconv.Itoa(int(port))

			result = append(result, corev1.EnvVar{
				Name:  fmt.Sprintf("%s_%s_CONNECT_SERVICE_HOST", serviceName, portName),
				Value: "127.0.0.1",
			}, corev1.EnvVar{
				Name:  fmt.Sprintf("%s_%s_CONNECT_SERVICE_PORT", serviceName, portName),
				Value: portStr,
			})
		}
	}

	return result
}
