// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package gatekeeper

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	gwv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// Gateway probe annotations allow configuring Kubernetes health probes per-Gateway.
// Each annotation accepts a JSON object following the Kubernetes Probe specification.
//
// Example usage:
//
//	apiVersion: gateway.networking.k8s.io/v1beta1
//	kind: Gateway
//	metadata:
//	  name: my-gateway
//	  annotations:
//	    consul.hashicorp.com/liveness-probe: |
//	      {
//	        "httpGet": {
//	          "path": "/ready",
//	          "port": 20000
//	        },
//	        "initialDelaySeconds": 10,
//	        "periodSeconds": 10,
//	        "timeoutSeconds": 1,
//	        "successThreshold": 1,
//	        "failureThreshold": 3
//	      }
//
// Supported probe handlers: httpGet, tcpSocket, exec, grpc.
// Only one handler should be specified per probe.
//
// Note: Liveness and startup probes must have successThreshold=1 per Kubernetes requirements.
// The parser will automatically normalize this value if a different value is provided.
const (
	// AnnotationLivenessProbe configures the liveness probe for Gateway pods.
	// Value must be a JSON object conforming to the Kubernetes corev1.Probe specification.
	// Liveness probes check if the container process should be restarted.
	// Example: {"httpGet": {"path": "/ready", "port": 20000}, "periodSeconds": 10}
	AnnotationLivenessProbe = "consul.hashicorp.com/liveness-probe"

	// AnnotationReadinessProbe configures the readiness probe for Gateway pods.
	// Value must be a JSON object conforming to the Kubernetes corev1.Probe specification.
	// Readiness probes determine if the pod is ready to serve traffic.
	// Example: {"tcpSocket": {"port": 20000}, "initialDelaySeconds": 5}
	AnnotationReadinessProbe = "consul.hashicorp.com/readiness-probe"

	// AnnotationStartupProbe configures the startup probe for Gateway pods.
	// Value must be a JSON object conforming to the Kubernetes corev1.Probe specification.
	// Startup probes control container startup and can delay liveness checks.
	// Example: {"exec": {"command": ["cat", "/tmp/healthy"]}, "failureThreshold": 30}
	AnnotationStartupProbe = "consul.hashicorp.com/startup-probe"
)

// ProbesConfig groups the three standard Kubernetes probes
type ProbesConfig struct {
	Liveness  *corev1.Probe
	Readiness *corev1.Probe
	Startup   *corev1.Probe
}

// ProbesFromGateway extracts and parses probe configurations from Gateway annotations.
// Returns nil if no probe annotations are present.
// Returns error if annotation JSON is malformed or probe configuration is invalid.
func ProbesFromGateway(gateway *gwv1beta1.Gateway) (*ProbesConfig, error) {
	if gateway.Annotations == nil {
		return nil, nil
	}

	probes := &ProbesConfig{}
	hasAnyProbe := false

	if livenessJSON, ok := gateway.Annotations[AnnotationLivenessProbe]; ok && livenessJSON != "" {
		probe, err := parseProbe(livenessJSON, "liveness")
		if err != nil {
			return nil, err
		}
		probes.Liveness = probe
		hasAnyProbe = true
	}

	if readinessJSON, ok := gateway.Annotations[AnnotationReadinessProbe]; ok && readinessJSON != "" {
		probe, err := parseProbe(readinessJSON, "readiness")
		if err != nil {
			return nil, err
		}
		probes.Readiness = probe
		hasAnyProbe = true
	}

	if startupJSON, ok := gateway.Annotations[AnnotationStartupProbe]; ok && startupJSON != "" {
		probe, err := parseProbe(startupJSON, "startup")
		if err != nil {
			return nil, err
		}
		probes.Startup = probe
		hasAnyProbe = true
	}

	if !hasAnyProbe {
		return nil, nil
	}

	return probes, nil
}

// parseProbe unmarshals JSON probe configuration, validates it, and sanitizes it.
func parseProbe(probeJSON, probeType string) (*corev1.Probe, error) {
	if probeJSON == "" {
		return nil, nil
	}

	probe := &corev1.Probe{}
	if err := json.Unmarshal([]byte(probeJSON), probe); err != nil {
		return nil, fmt.Errorf("invalid %s probe JSON: %w", probeType, err)
	}

	// Sanitize: remove empty handlers
	if probe.HTTPGet != nil && probe.HTTPGet.Path == "" && probe.HTTPGet.Port.IntValue() == 0 && probe.HTTPGet.Port.StrVal == "" {
		probe.HTTPGet = nil
	}
	if probe.TCPSocket != nil && probe.TCPSocket.Port.IntValue() == 0 && probe.TCPSocket.Port.StrVal == "" {
		probe.TCPSocket = nil
	}
	if probe.Exec != nil && len(probe.Exec.Command) == 0 {
		probe.Exec = nil
	}

	// Validate: exactly one handler must be specified
	handlerCount := 0
	if probe.HTTPGet != nil {
		handlerCount++
	}
	if probe.TCPSocket != nil {
		handlerCount++
	}
	if probe.Exec != nil {
		handlerCount++
	}
	if probe.GRPC != nil {
		handlerCount++
	}

	if handlerCount == 0 {
		return nil, fmt.Errorf("%s probe must have at least one handler (httpGet, tcpSocket, exec, or grpc)", probeType)
	}
	if handlerCount > 1 {
		return nil, fmt.Errorf("%s probe must have exactly one handler, found %d", probeType, handlerCount)
	}

	// Sanitize: Kubernetes requires liveness and startup probes to have successThreshold == 1
	if probeType == "liveness" || probeType == "startup" {
		if probe.SuccessThreshold != 0 && probe.SuccessThreshold != 1 {
			probe.SuccessThreshold = 1
		}
	}

	return probe, nil
}
