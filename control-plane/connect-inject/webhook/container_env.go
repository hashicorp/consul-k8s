// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	corev1 "k8s.io/api/core/v1"
)

func (w *MeshWebhook) containerEnvVars(pod corev1.Pod) []corev1.EnvVar {
	raw, ok := pod.Annotations[constants.AnnotationUpstreams]
	if !ok || raw == "" {
		return []corev1.EnvVar{}
	}

	var result []corev1.EnvVar
	for _, raw := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\n' // Split either comma separated or space separated
	}) {
		parts := strings.SplitN(raw, ":", 3)
		if len(parts) < 2 {
			w.Log.Error(fmt.Errorf("ustream URL is malformed, skipping it: %s", raw), "malformed upstream")
			continue
		}
		port, _ := common.PortValue(pod, strings.TrimSpace(parts[1]))
		if port > 0 {
			name := strings.TrimSpace(parts[0])
			name = strings.ToUpper(strings.Replace(name, "-", "_", -1))
			portStr := strconv.Itoa(int(port))
			addr := constants.Getv4orv6Str("127.0.0.1", "::1")
			result = append(result, corev1.EnvVar{
				Name:  fmt.Sprintf("%s_CONNECT_SERVICE_HOST", name),
				Value: addr,
			}, corev1.EnvVar{
				Name:  fmt.Sprintf("%s_CONNECT_SERVICE_PORT", name),
				Value: portStr,
			})
		}
	}

	return result
}
