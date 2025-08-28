// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package lifecycle

import (
	"fmt"
	"strconv"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	corev1 "k8s.io/api/core/v1"
)

// Config represents configuration common to connect-inject components related to proxy lifecycle management.
type Config struct {
	DefaultEnableProxyLifecycle           bool
	DefaultEnableShutdownDrainListeners   bool
	DefaultShutdownGracePeriodSeconds     int
	DefaultStartupGracePeriodSeconds      int
	DefaultGracefulPort                   string
	DefaultGracefulShutdownPath           string
	DefaultGracefulStartupPath            string
	DefaultEnableConsulDataplaneAsSidecar bool
}

// EnableProxyLifecycle returns whether proxy lifecycle management is enabled either via the default value in the meshWebhook, or if it's been
// overridden via the annotation.
func (lc Config) EnableProxyLifecycle(pod corev1.Pod) (bool, error) {
	enabled := lc.DefaultEnableProxyLifecycle
	if raw, ok := pod.Annotations[constants.AnnotationEnableSidecarProxyLifecycle]; ok && raw != "" {
		enableProxyLifecycle, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s annotation value of %s was invalid: %s", constants.AnnotationEnableSidecarProxyLifecycle, raw, err)
		}
		enabled = enableProxyLifecycle
	}
	return enabled, nil
}

// EnableShutdownDrainListeners returns whether proxy listener draining during shutdown is enabled either via the default value in the meshWebhook, or if it's been
// overridden via the annotation.
func (lc Config) EnableShutdownDrainListeners(pod corev1.Pod) (bool, error) {
	enabled := lc.DefaultEnableShutdownDrainListeners
	if raw, ok := pod.Annotations[constants.AnnotationEnableSidecarProxyLifecycleShutdownDrainListeners]; ok && raw != "" {
		enableShutdownDrainListeners, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s annotation value of %s was invalid: %s", constants.AnnotationEnableSidecarProxyLifecycleShutdownDrainListeners, raw, err)
		}
		enabled = enableShutdownDrainListeners
	}
	return enabled, nil
}

// ShutdownGracePeriodSeconds returns how long the sidecar proxy should wait before shutdown, either via the default value in the meshWebhook, or if it's been
// overridden via the annotation.
func (lc Config) ShutdownGracePeriodSeconds(pod corev1.Pod) (int, error) {
	shutdownGracePeriodSeconds := lc.DefaultShutdownGracePeriodSeconds
	if shutdownGracePeriodSecondsAnnotation, ok := pod.Annotations[constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds]; ok {
		val, err := strconv.ParseUint(shutdownGracePeriodSecondsAnnotation, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("unable to parse annotation %q: %w", constants.AnnotationSidecarProxyLifecycleShutdownGracePeriodSeconds, err)
		}
		shutdownGracePeriodSeconds = int(val)
	}
	return shutdownGracePeriodSeconds, nil
}

// StartupGracePeriodSeconds returns how long to block application startup waiting for the sidecar proxy to be ready, either via the default value in the meshWebhook, or if it's been
// overridden via the annotation.
func (lc Config) StartupGracePeriodSeconds(pod corev1.Pod) (int, error) {
	startupGracePeriodSeconds := lc.DefaultStartupGracePeriodSeconds
	if startupGracePeriodSecondsAnnotation, ok := pod.Annotations[constants.AnnotationSidecarProxyLifecycleStartupGracePeriodSeconds]; ok {
		val, err := strconv.ParseUint(startupGracePeriodSecondsAnnotation, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("unable to parse annotation %q: %w", constants.AnnotationSidecarProxyLifecycleStartupGracePeriodSeconds, err)
		}
		startupGracePeriodSeconds = int(val)
	}
	return startupGracePeriodSeconds, nil
}

// GracefulPort returns the port on which consul-dataplane should serve the proxy lifecycle management HTTP endpoints, either via the default value in the meshWebhook, or
// if it's been overridden via the annotation. It also validates the port is in the unprivileged port range.
func (lc Config) GracefulPort(pod corev1.Pod) (int, error) {
	anno, err := common.DetermineAndValidatePort(pod, constants.AnnotationSidecarProxyLifecycleGracefulPort, lc.DefaultGracefulPort, false)
	if err != nil {
		return 0, err
	}

	if anno == "" {
		return constants.DefaultGracefulPort, nil
	}

	port, _ := strconv.Atoi(anno)

	return port, nil
}

// GracefulShutdownPath returns the path on which consul-dataplane should serve the graceful shutdown HTTP endpoint, either via the default value in the meshWebhook, or
// if it's been overridden via the annotation.
func (lc Config) GracefulShutdownPath(pod corev1.Pod) string {
	if raw, ok := pod.Annotations[constants.AnnotationSidecarProxyLifecycleGracefulShutdownPath]; ok && raw != "" {
		return raw
	}

	if lc.DefaultGracefulShutdownPath == "" {
		return constants.DefaultGracefulShutdownPath
	}

	return lc.DefaultGracefulShutdownPath
}

// GracefulStartupPath returns the path on which consul-dataplane should serve the graceful startup HTTP endpoint, either via the default value in the meshWebhook, or
// if it's been overridden via the annotation.
func (lc Config) GracefulStartupPath(pod corev1.Pod) string {
	if raw, ok := pod.Annotations[constants.AnnotationSidecarProxyLifecycleGracefulStartupPath]; ok && raw != "" {
		return raw
	}

	if lc.DefaultGracefulStartupPath == "" {
		return constants.DefaultGracefulStartupPath
	}

	return lc.DefaultGracefulStartupPath
}

// EnableConsulDataplaneAsSidecar returns whether register consul-dataplane as sidecar in kubernetes is enabled either via the default value in the meshWebhook, or if it's been
// overridden via the annotation.
func (lc Config) EnableConsulDataplaneAsSidecar(pod corev1.Pod) (bool, error) {
	enabled := lc.DefaultEnableConsulDataplaneAsSidecar
	if raw, ok := pod.Annotations[constants.AnnotationEnableConsulDataplaneAsSidecar]; ok && raw != "" {
		enableConsulDataplaneAsSidecar, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s annotation value of %s was invalid: %s", constants.AnnotationEnableConsulDataplaneAsSidecar, raw, err)
		}
		enabled = enableConsulDataplaneAsSidecar
	}
	return enabled, nil
}
