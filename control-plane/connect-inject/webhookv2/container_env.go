// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhookv2

import (
	corev1 "k8s.io/api/core/v1"
)

func (w *MeshWebhook) containerEnvVars(pod corev1.Pod) []corev1.EnvVar {
	// (TODO: ashwin) make this work with current upstreams
	//raw, ok := pod.Annotations[constants.AnnotationMeshDestinations]
	//if !ok || raw == "" {
	//	return []corev1.EnvVar{}
	//}
	//
	//var result []corev1.EnvVar
	//for _, raw := range strings.Split(raw, ",") {
	//	parts := strings.SplitN(raw, ":", 3)
	//	port, _ := common.PortValue(pod, strings.TrimSpace(parts[1]))
	//	if port > 0 {
	//		name := strings.TrimSpace(parts[0])
	//		name = strings.ToUpper(strings.Replace(name, "-", "_", -1))
	//		portStr := strconv.Itoa(int(port))
	//
	//		result = append(result, corev1.EnvVar{
	//			Name:  fmt.Sprintf("%s_CONNECT_SERVICE_HOST", name),
	//			Value: "127.0.0.1",
	//		}, corev1.EnvVar{
	//			Name:  fmt.Sprintf("%s_CONNECT_SERVICE_PORT", name),
	//			Value: portStr,
	//		})
	//	}
	//}

	return []corev1.EnvVar{}
}
