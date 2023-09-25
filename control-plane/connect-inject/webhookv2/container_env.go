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
	upstreams, err := common.ProcessPodUpstreamsForMeshWebhook(pod)
	if err != nil {
		return nil, fmt.Errorf("error processing the upstream for container environment variable creation: %s", err.Error())
	}
	if upstreams == nil {
		return nil, nil
	}

	var result []corev1.EnvVar
	for _, upstream := range upstreams.Upstreams {
		serviceName := strings.TrimSpace(upstream.DestinationRef.Name)
		serviceName = strings.ToUpper(strings.Replace(serviceName, "-", "_", -1))
		portName := strings.TrimSpace(upstream.DestinationPort)
		portName = strings.ToUpper(strings.Replace(portName, "-", "_", -1))

		result = append(result, corev1.EnvVar{
			Name:  fmt.Sprintf("%s_%s_CONNECT_SERVICE_HOST", serviceName, portName),
			Value: upstream.GetIpPort().Ip,
		}, corev1.EnvVar{
			Name:  fmt.Sprintf("%s_%s_CONNECT_SERVICE_PORT", serviceName, portName),
			Value: strconv.Itoa(int(upstream.GetIpPort().Port)),
		})
	}

	return result, nil
}
