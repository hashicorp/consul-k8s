// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package common

import (
	"fmt"
	"strconv"
	"strings"

	mapset "github.com/deckarep/golang-set"
	pbcatalog "github.com/hashicorp/consul/proto-public/pbcatalog/v2beta1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

// DetermineAndValidatePort behaves as follows:
// If the annotation exists, validate the port and return it.
// If the annotation does not exist, return the default port.
// If the privileged flag is true, it will allow the port to be in the
// privileged port range of 1-1023. Otherwise, it will only allow ports in the
// unprivileged range of 1024-65535.
func DetermineAndValidatePort(pod corev1.Pod, annotation string, defaultPort string, privileged bool) (string, error) {
	if raw, ok := pod.Annotations[annotation]; ok && raw != "" {
		port, err := PortValue(pod, raw)
		if err != nil {
			return "", fmt.Errorf("%s annotation value of %s is not a valid integer", annotation, raw)
		}

		if privileged && (port < 1 || port > 65535) {
			return "", fmt.Errorf("%s annotation value of %d is not in the valid port range 1-65535", annotation, port)
		} else if !privileged && (port < 1024 || port > 65535) {
			return "", fmt.Errorf("%s annotation value of %d is not in the unprivileged port range 1024-65535", annotation, port)
		}

		// If the annotation exists, return the validated port.
		return fmt.Sprint(port), nil
	}

	// If the annotation does not exist, return the default.
	if defaultPort != "" {
		port, err := PortValue(pod, defaultPort)
		if err != nil {
			return "", fmt.Errorf("%s is not a valid port on the pod %s", defaultPort, pod.Name)
		}
		return fmt.Sprint(port), nil
	}
	return "", nil
}

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

// WorkloadPortName returns the container port's name if it has one, and if not, constructs a name from the port number
// and adds a constant prefix. The port name must be 1-15 characters and must have at least 1 alpha character.
func WorkloadPortName(port *corev1.ContainerPort) string {
	name := port.Name
	var isNum bool
	if _, err := strconv.Atoi(name); err == nil {
		isNum = true
	}
	if name == "" || isNum {
		name = constants.UnnamedWorkloadPortNamePrefix + strconv.Itoa(int(port.ContainerPort))
	}
	return name
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

// ShouldIgnore ignores namespaces where we don't mesh-inject.
func ShouldIgnore(namespace string, denySet, allowSet mapset.Set) bool {
	// Ignores system namespaces.
	if namespace == metav1.NamespaceSystem || namespace == metav1.NamespacePublic || namespace == "local-path-storage" {
		return true
	}

	// Ignores deny list.
	if denySet.Contains(namespace) {
		return true
	}

	// Ignores if not in allow list or allow list is not *.
	if !allowSet.Contains("*") && !allowSet.Contains(namespace) {
		return true
	}

	return false
}

func ConsulNodeNameFromK8sNode(nodeName string) string {
	return fmt.Sprintf("%s-virtual", nodeName)
}

// ********************
// V2 Exclusive Common Code
// ********************

// ToProtoAny is a convenience function for converting proto.Message values to anypb.Any without error handling.
// This should _only_ be used in cases where a nil or valid proto.Message value is _guaranteed_, else it will panic.
// If the type of m is *anypb.Any, that value will be returned unmodified.
func ToProtoAny(m proto.Message) *anypb.Any {
	switch v := m.(type) {
	case nil:
		return nil
	case *anypb.Any:
		return v
	}
	a, err := anypb.New(m)
	if err != nil {
		panic(fmt.Errorf("unexpected error: failed to convert proto message to anypb.Any: %w", err))
	}
	return a
}

// GetPortProtocol matches the Kubernetes EndpointPort.AppProtocol or ServicePort.AppProtocol (*string) to a supported
// Consul catalog port protocol. If nil or unrecognized, the default of `PROTOCOL_UNSPECIFIED` is returned.
func GetPortProtocol(appProtocol *string) pbcatalog.Protocol {
	if appProtocol == nil {
		return pbcatalog.Protocol_PROTOCOL_UNSPECIFIED
	}
	switch *appProtocol {
	case "tcp":
		return pbcatalog.Protocol_PROTOCOL_TCP
	case "http":
		return pbcatalog.Protocol_PROTOCOL_HTTP
	case "http2":
		return pbcatalog.Protocol_PROTOCOL_HTTP2
	case "grpc":
		return pbcatalog.Protocol_PROTOCOL_GRPC
	}
	// If unrecognized or empty string, return default
	return pbcatalog.Protocol_PROTOCOL_UNSPECIFIED
}

// PortValueFromIntOrString returns the integer port value from the port that can be
// a named port, an integer string (e.g. "80"), or an integer. If the port is a named port,
// this function will attempt to find the value from the containers of the pod.
func PortValueFromIntOrString(pod corev1.Pod, port intstr.IntOrString) (uint32, error) {
	if port.Type == intstr.Int {
		return uint32(port.IntValue()), nil
	}

	// Otherwise, find named port or try to parse the string as an int.
	portVal, err := PortValue(pod, port.StrVal)
	if err != nil {
		return 0, err
	}
	return uint32(portVal), nil
}

// HasBeenMeshInjected checks the value of the status annotation and returns true if the Pod has been injected.
// Does not apply to V1 pods, which use a different key (`constants.KeyInjectStatus`).
func HasBeenMeshInjected(pod corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}
	if anno, ok := pod.Annotations[constants.KeyMeshInjectStatus]; ok && anno == constants.Injected {
		return true
	}
	return false
}

func IsGateway(pod corev1.Pod) bool {
	if pod.Annotations == nil {
		return false
	}
	if anno, ok := pod.Annotations[constants.AnnotationGatewayKind]; ok && anno != "" {
		return true
	}
	return false
}

// ConsulNamespaceIsNotFound checks the gRPC error code and message to determine
// if a namespace does not exist. If the namespace exists this function returns false, true otherwise.
func ConsulNamespaceIsNotFound(err error) bool {
	if err == nil {
		return false
	}
	s, ok := status.FromError(err)
	if !ok {
		return false
	}
	if codes.InvalidArgument == s.Code() && strings.Contains(s.Message(), "namespace not found") {
		return true
	}
	return false
}
