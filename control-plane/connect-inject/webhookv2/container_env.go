// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhookv2

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
)

func (w *MeshWebhook) containerEnvVars(pod corev1.Pod) ([]corev1.EnvVar, error) {
	destinations, err := common.ProcessPodDestinationsForMeshWebhook(pod)
	if err != nil {
		return nil, fmt.Errorf("error processing the destination for container environment variable creation: %s", err.Error())
	}
	if destinations == nil {
		return nil, nil
	}

	var result []corev1.EnvVar
	for _, destination := range destinations.Destinations {
		serviceName := strings.TrimSpace(destination.DestinationRef.Name)
		serviceName = strings.ToUpper(strings.Replace(serviceName, "-", "_", -1))
		portName := strings.TrimSpace(destination.DestinationPort)
		portName = strings.ToUpper(strings.Replace(portName, "-", "_", -1))

		result = append(result, corev1.EnvVar{
			Name:  fmt.Sprintf("%s_%s_CONNECT_SERVICE_HOST", serviceName, portName),
			Value: destination.GetIpPort().Ip,
		}, corev1.EnvVar{
			Name:  fmt.Sprintf("%s_%s_CONNECT_SERVICE_PORT", serviceName, portName),
			Value: strconv.Itoa(int(destination.GetIpPort().Port)),
		})
	}

	return result, nil
}
