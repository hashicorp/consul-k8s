// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package webhook

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/google/shlex"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
)

const (
	consulDataplaneDNSBindHost     = "127.0.0.1"
	ipv6ConsulDataplaneDNSBindHost = "::1"
	consulDataplaneDNSBindPort     = 8600
)

func (w *MeshWebhook) consulDataplaneSidecar(namespace corev1.Namespace, pod corev1.Pod, mpi multiPortInfo) (corev1.Container, error) {
	resources, err := w.sidecarResources(pod)
	if err != nil {
		return corev1.Container{}, err
	}

	// Extract the service account token's volume mount.
	var bearerTokenFile string
	var saTokenVolumeMount corev1.VolumeMount
	if w.AuthMethod != "" {
		saTokenVolumeMount, bearerTokenFile, err = findServiceAccountVolumeMount(pod, mpi.serviceName)
		if err != nil {
			return corev1.Container{}, err
		}
	}

	multiPort := mpi.serviceName != ""
	args, err := w.getContainerSidecarArgs(namespace, mpi, bearerTokenFile, pod)
	if err != nil {
		return corev1.Container{}, err
	}

	containerName := sidecarContainer
	if multiPort {
		containerName = fmt.Sprintf("%s-%s", sidecarContainer, mpi.serviceName)
	}

	var readinessProbe *corev1.Probe
	if useProxyHealthCheck(pod) {
		// If using the proxy health check for a service, configure an HTTP handler
		// that queries the '/ready' endpoint of the proxy.
		readinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Port: intstr.FromInt(constants.ProxyDefaultHealthPort + mpi.serviceIndex),
					Path: "/ready",
				},
			},
			InitialDelaySeconds: 1,
		}
	} else {
		readinessProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(constants.ProxyDefaultInboundPort + mpi.serviceIndex),
				},
			},
			InitialDelaySeconds: 1,
		}
	}

	// Configure optional probes on the proxy to force restart it in failure scenarios.
	var startupProbe, livenessProbe *corev1.Probe
	startupSeconds := w.getStartupFailureSeconds(pod)
	livenessSeconds := w.getLivenessFailureSeconds(pod)
	if startupSeconds > 0 {
		startupProbe = &corev1.Probe{
			// Use the same handler as the readiness probe.
			ProbeHandler:     readinessProbe.ProbeHandler,
			PeriodSeconds:    1,
			FailureThreshold: startupSeconds,
		}
	}
	if livenessSeconds > 0 {
		livenessProbe = &corev1.Probe{
			// Use the same handler as the readiness probe.
			ProbeHandler:     readinessProbe.ProbeHandler,
			PeriodSeconds:    1,
			FailureThreshold: livenessSeconds,
		}
	}

	container := corev1.Container{
		Name:            containerName,
		Image:           w.ImageConsulDataplane,
		ImagePullPolicy: corev1.PullPolicy(w.GlobalImagePullPolicy),
		Resources:       resources,
		// We need to set tmp dir to an ephemeral volume that we're mounting so that
		// consul-dataplane can write files to it. Otherwise, it wouldn't be able to
		// because we set file system to be read-only.
		Env: []corev1.EnvVar{
			{
				Name:  "TMPDIR",
				Value: "/consul/connect-inject",
			},
			{
				Name: "NODE_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "spec.nodeName",
					},
				},
			},
			{
				Name:  "DP_SERVICE_NODE_NAME",
				Value: "$(NODE_NAME)-virtual",
			},
			// The pod name isn't known currently, so we must rely on the environment variable to fill it in rather than using args.
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			},
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.namespace"},
				},
			},
			{
				Name: "POD_UID",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.uid"},
				},
			},
			{
				Name:  "DP_CREDENTIAL_LOGIN_META",
				Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)",
			},
			// This entry exists to support certain versions of consul dataplane, where environment variable entries
			// utilize this numbered notation to indicate individual KV pairs in a map.
			{
				Name:  "DP_CREDENTIAL_LOGIN_META1",
				Value: "pod=$(POD_NAMESPACE)/$(POD_NAME)",
			},
			{
				Name:  "DP_CREDENTIAL_LOGIN_META2",
				Value: "pod-uid=$(POD_UID)",
			},
			{
				Name: "HOST_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
				},
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Args:           args,
		ReadinessProbe: readinessProbe,
		StartupProbe:   startupProbe,
		LivenessProbe:  livenessProbe,
	}

	if w.AuthMethod != "" {
		container.VolumeMounts = append(container.VolumeMounts, saTokenVolumeMount)
	}

	if useProxyHealthCheck(pod) {
		// Configure the Readiness Address for the proxy's health check to be the Pod IP.
		container.Env = append(container.Env, corev1.EnvVar{
			Name: "DP_ENVOY_READY_BIND_ADDRESS",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.podIP"},
			},
		})
		// Configure the port on which the readiness probe will query the proxy for its health.
		container.Ports = append(container.Ports, corev1.ContainerPort{
			Name:          fmt.Sprintf("%s-%d", "proxy-health", mpi.serviceIndex),
			ContainerPort: int32(constants.ProxyDefaultHealthPort + mpi.serviceIndex),
		})
	}

	// Add any extra VolumeMounts.
	if userVolMount, ok := pod.Annotations[constants.AnnotationConsulSidecarUserVolumeMount]; ok {
		var volumeMounts []corev1.VolumeMount
		err := json.Unmarshal([]byte(userVolMount), &volumeMounts)
		if err != nil {
			return corev1.Container{}, err
		}
		container.VolumeMounts = append(container.VolumeMounts, volumeMounts...)
	}

	// Container Ports
	metricsPorts, err := w.getMetricsPorts(pod)
	if err != nil {
		return corev1.Container{}, err
	}
	if metricsPorts != nil {
		container.Ports = append(container.Ports, metricsPorts...)
	}

	tproxyEnabled, err := common.TransparentProxyEnabled(namespace, pod, w.EnableTransparentProxy)
	if err != nil {
		return corev1.Container{}, err
	}

	// Default values for non-Openshift environments.
	uid := int64(sidecarUserAndGroupID)
	group := int64(sidecarUserAndGroupID)

	// If not running in transparent proxy mode and in an OpenShift environment,
	// skip setting the security context and let OpenShift set it for us.
	// When transparent proxy is enabled, then consul-dataplane needs to run as our specific user
	// so that traffic redirection will work.
	if tproxyEnabled || !w.EnableOpenShift {
		if !w.EnableOpenShift {
			if pod.Spec.SecurityContext != nil {
				// User container and consul-dataplane container cannot have the same UID.
				if pod.Spec.SecurityContext.RunAsUser != nil && *pod.Spec.SecurityContext.RunAsUser == sidecarUserAndGroupID {
					return corev1.Container{}, fmt.Errorf("pod's security context cannot have the same UID as consul-dataplane: %v", sidecarUserAndGroupID)
				}
			}

			// Ensure that none of the user's containers have the same UID as consul-dataplane. At this point in injection the meshWebhook
			// has only injected init containers so all containers defined in pod.Spec.Containers are from the user.
			for _, c := range pod.Spec.Containers {
				// User container and consul-dataplane container cannot have the same UID.
				if c.SecurityContext != nil && c.SecurityContext.RunAsUser != nil &&
					*c.SecurityContext.RunAsUser == sidecarUserAndGroupID &&
					c.Image != w.ImageConsulDataplane {
					return corev1.Container{}, fmt.Errorf("container %q has runAsUser set to the same UID \"%d\" as consul-dataplane which is not allowed", c.Name, sidecarUserAndGroupID)
				}
			}
		}
	}

	if w.EnableOpenShift {
		// Transparent proxy is set in OpenShift. There is an annotation on the namespace that tells us what
		// the user and group ids should be for the sidecar.
		var err error
		uid, err = common.GetDataplaneUID(namespace, pod, w.ImageConsulDataplane, w.ImageConsulK8S)
		if err != nil {
			return corev1.Container{}, err
		}
		group, err = common.GetDataplaneGroupID(namespace, pod, w.ImageConsulDataplane, w.ImageConsulK8S)
		if err != nil {
			return corev1.Container{}, err
		}
	}

	container.SecurityContext = &corev1.SecurityContext{
		RunAsUser:                ptr.To(uid),
		RunAsGroup:               ptr.To(group),
		RunAsNonRoot:             ptr.To(true),
		AllowPrivilegeEscalation: ptr.To(false),
		SeccompProfile: &corev1.SeccompProfile{
			Type: corev1.SeccompProfileTypeRuntimeDefault,
		},
		Capabilities: &corev1.Capabilities{
			Drop: []corev1.Capability{"ALL"},
		},
		ReadOnlyRootFilesystem: ptr.To(true),
	}
	enableConsulDataplaneAsSidecar, err := w.LifecycleConfig.EnableConsulDataplaneAsSidecar(pod)
	if err != nil {
		return corev1.Container{}, err
	}
	if enableConsulDataplaneAsSidecar {
		restartPolicy := corev1.ContainerRestartPolicyAlways
		container.RestartPolicy = &restartPolicy

		// Configure the startup probe to check the sidecar proxy health.
		container.StartupProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{
						// Absolute path to the binary from the Dockerfile
						"/usr/local/bin/consul-dataplane",
						// Built-in subcommand to check Envoy health
						"-check-proxy-health",
					},
				},
			},
			InitialDelaySeconds: w.getSidecarProbeCheckInitialDelaySeconds(pod),
			PeriodSeconds:       w.getSidecarProbePeriodSeconds(pod),
			FailureThreshold:    w.getSidecarProbeFailureThreshold(pod),
			TimeoutSeconds:      w.getSidecarProbeTimeoutSeconds(pod),
		}
	}
	return container, nil
}

func (w *MeshWebhook) getContainerSidecarArgs(namespace corev1.Namespace, mpi multiPortInfo, bearerTokenFile string, pod corev1.Pod) ([]string, error) {
	proxyIDFileName := "/consul/connect-inject/proxyid"
	if mpi.serviceName != "" {
		proxyIDFileName = fmt.Sprintf("/consul/connect-inject/proxyid-%s", mpi.serviceName)
	}

	envoyConcurrency := w.DefaultEnvoyProxyConcurrency

	// Check to see if the user has overriden concurrency via an annotation.
	if envoyConcurrencyAnnotation, ok := pod.Annotations[constants.AnnotationEnvoyProxyConcurrency]; ok {
		val, err := strconv.ParseUint(envoyConcurrencyAnnotation, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("unable to parse annotation %q: %w", constants.AnnotationEnvoyProxyConcurrency, err)
		}
		envoyConcurrency = int(val)
	}
	envoyAdminBindAddress := constants.Getv4orv6Str("127.0.0.1", "::1")
	consulDNSBindAddress := constants.Getv4orv6Str(consulDataplaneDNSBindHost, ipv6ConsulDataplaneDNSBindHost)
	consulDPBindAddress := constants.Getv4orv6Str("127.0.0.1", "::1")
	xdsBindAddress := constants.Getv4orv6Str("127.0.0.1", "::1")
	args := []string{
		"-addresses", w.ConsulAddress,
		"-envoy-admin-bind-address=" + envoyAdminBindAddress,
		"-consul-dns-bind-addr=" + consulDNSBindAddress,
		"-xds-bind-addr=" + xdsBindAddress,
		"-grpc-port=" + strconv.Itoa(w.ConsulConfig.GRPCPort),
		"-proxy-service-id-path=" + proxyIDFileName,
		"-log-level=" + w.LogLevel,
		"-log-json=" + strconv.FormatBool(w.LogJSON),
		"-envoy-concurrency=" + strconv.Itoa(envoyConcurrency),
		"-graceful-addr=" + consulDPBindAddress,
	}

	if w.SkipServerWatch {
		args = append(args, "-server-watch-disabled=true")
	}

	if w.AuthMethod != "" {
		args = append(args,
			"-credential-type=login",
			"-login-auth-method="+w.AuthMethod,
			"-login-bearer-token-path="+bearerTokenFile,
			// We don't know the pod name at this time, so we must use environment variables to populate the login-meta instead.
		)
		if w.EnableNamespaces {
			if w.EnableK8SNSMirroring {
				args = append(args, "-login-namespace=default")
			} else {
				args = append(args, "-login-namespace="+w.consulNamespace(namespace.Name))
			}
		}
		if w.ConsulPartition != "" {
			args = append(args, "-login-partition="+w.ConsulPartition)
		}
	}
	if w.EnableNamespaces {
		args = append(args, "-service-namespace="+w.consulNamespace(namespace.Name))
	}
	if w.ConsulPartition != "" {
		args = append(args, "-service-partition="+w.ConsulPartition)
	}
	if w.TLSEnabled {
		if w.ConsulTLSServerName != "" {
			args = append(args, "-tls-server-name="+w.ConsulTLSServerName)
		}
		if w.ConsulCACert != "" {
			args = append(args, "-ca-certs="+constants.LegacyConsulCAFile)
		}
	} else {
		args = append(args, "-tls-disabled")
	}

	// Configure the readiness port on the dataplane sidecar if proxy health checks are enabled.
	if useProxyHealthCheck(pod) {
		args = append(args, fmt.Sprintf("%s=%d", "-envoy-ready-bind-port", constants.ProxyDefaultHealthPort+mpi.serviceIndex))
	}

	if mpi.serviceName != "" {
		args = append(args, fmt.Sprintf("-envoy-admin-bind-port=%d", 19000+mpi.serviceIndex))
	}

	// The consul-dataplane HTTP listener always starts for graceful shutdown. To avoid port conflicts, the
	// graceful port always needs to be set
	gracefulPort, err := w.LifecycleConfig.GracefulPort(pod)
	if err != nil {
		return nil, fmt.Errorf("unable to determine proxy lifecycle graceful port: %w", err)
	}

	// To avoid conflicts
	if mpi.serviceName != "" {
		gracefulPort = gracefulPort + mpi.serviceIndex
	}

	args = append(args, fmt.Sprintf("-graceful-port=%d", gracefulPort))

	enableProxyLifecycle, err := w.LifecycleConfig.EnableProxyLifecycle(pod)
	if err != nil {
		return nil, fmt.Errorf("unable to determine if proxy lifecycle management is enabled: %w", err)
	}
	if enableProxyLifecycle {
		shutdownDrainListeners, err := w.LifecycleConfig.EnableShutdownDrainListeners(pod)
		if err != nil {
			return nil, fmt.Errorf("unable to determine if proxy lifecycle shutdown listener draining is enabled: %w", err)
		}
		if shutdownDrainListeners {
			args = append(args, "-shutdown-drain-listeners")
		}

		shutdownGracePeriodSeconds, err := w.LifecycleConfig.ShutdownGracePeriodSeconds(pod)
		if err != nil {
			return nil, fmt.Errorf("unable to determine proxy lifecycle shutdown grace period: %w", err)
		}
		args = append(args, fmt.Sprintf("-shutdown-grace-period-seconds=%d", shutdownGracePeriodSeconds))

		gracefulShutdownPath := w.LifecycleConfig.GracefulShutdownPath(pod)
		args = append(args, fmt.Sprintf("-graceful-shutdown-path=%s", gracefulShutdownPath))

		startupGracePeriodSeconds, err := w.LifecycleConfig.StartupGracePeriodSeconds(pod)
		if err != nil {
			return nil, fmt.Errorf("unable to determine proxy lifecycle startup grace period: %w", err)
		}
		args = append(args, fmt.Sprintf("-startup-grace-period-seconds=%d", startupGracePeriodSeconds))

		gracefulStartupPath := w.LifecycleConfig.GracefulStartupPath(pod)
		args = append(args, fmt.Sprintf("-graceful-startup-path=%s", gracefulStartupPath))
	}

	// Set a default scrape path that can be overwritten by the annotation.
	prometheusScrapePath := w.MetricsConfig.PrometheusScrapePath(pod)
	args = append(args, "-telemetry-prom-scrape-path="+prometheusScrapePath)

	metricsServer, err := w.MetricsConfig.ShouldRunMergedMetricsServer(pod)
	if err != nil {
		return nil, fmt.Errorf("unable to determine if merged metrics is enabled: %w", err)
	}
	if metricsServer {
		mergedMetricsPort, err := w.MetricsConfig.MergedMetricsPort(pod)
		if err != nil {
			return nil, fmt.Errorf("unable to determine if merged metrics port: %w", err)
		}
		args = append(args, "-telemetry-prom-merge-port="+mergedMetricsPort)

		serviceMetricsPath := w.MetricsConfig.ServiceMetricsPath(pod)
		serviceMetricsPort, err := w.MetricsConfig.ServiceMetricsPort(pod)
		if err != nil {
			return nil, fmt.Errorf("unable to determine if service metrics port: %w", err)
		}

		if serviceMetricsPath != "" && serviceMetricsPort != "" {
			addr := constants.Getv4orv6Str("127.0.0.1", "::1")
			addr = net.JoinHostPort(addr, serviceMetricsPort)
			args = append(args, "-telemetry-prom-service-metrics-url="+fmt.Sprintf("http://%s%s", addr, serviceMetricsPath))
		}

		// Pull the TLS config from the relevant annotations.
		var prometheusCAFile string
		if raw, ok := pod.Annotations[constants.AnnotationPrometheusCAFile]; ok && raw != "" {
			prometheusCAFile = raw
		}

		var prometheusCAPath string
		if raw, ok := pod.Annotations[constants.AnnotationPrometheusCAPath]; ok && raw != "" {
			prometheusCAPath = raw
		}

		var prometheusCertFile string
		if raw, ok := pod.Annotations[constants.AnnotationPrometheusCertFile]; ok && raw != "" {
			prometheusCertFile = raw
		}

		var prometheusKeyFile string
		if raw, ok := pod.Annotations[constants.AnnotationPrometheusKeyFile]; ok && raw != "" {
			prometheusKeyFile = raw
		}

		// Validate required Prometheus TLS config is present if set.
		if prometheusCAFile != "" || prometheusCAPath != "" || prometheusCertFile != "" || prometheusKeyFile != "" {
			if prometheusCAFile == "" && prometheusCAPath == "" {
				return nil, fmt.Errorf("must set one of %q or %q when providing prometheus TLS config", constants.AnnotationPrometheusCAFile, constants.AnnotationPrometheusCAPath)
			}
			if prometheusCertFile == "" {
				return nil, fmt.Errorf("must set %q when providing prometheus TLS config", constants.AnnotationPrometheusCertFile)
			}
			if prometheusKeyFile == "" {
				return nil, fmt.Errorf("must set %q when providing prometheus TLS config", constants.AnnotationPrometheusKeyFile)
			}
			// TLS config has been validated, add them to the consul-dataplane cmd args
			args = append(args, "-telemetry-prom-ca-certs-file="+prometheusCAFile,
				"-telemetry-prom-ca-certs-path="+prometheusCAPath,
				"-telemetry-prom-cert-file="+prometheusCertFile,
				"-telemetry-prom-key-file="+prometheusKeyFile)
		}
	}

	// If Consul DNS is enabled, we want to configure consul-dataplane to be the DNS proxy
	// for Consul DNS in the pod.
	dnsEnabled, err := consulDNSEnabled(namespace, pod, w.EnableConsulDNS, w.EnableTransparentProxy)
	if err != nil {
		return nil, err
	}
	if dnsEnabled {
		args = append(args, "-consul-dns-bind-port="+strconv.Itoa(consulDataplaneDNSBindPort))
	}

	var envoyExtraArgs []string
	extraArgs, annotationSet := pod.Annotations[constants.AnnotationEnvoyExtraArgs]
	// --base-id is an envoy arg rather than consul-dataplane, and so we need to make sure we're passing it
	// last separated by the --.
	if mpi.serviceName != "" {
		// --base-id is needed so multiple Envoy proxies can run on the same host.
		envoyExtraArgs = append(envoyExtraArgs, "--base-id", fmt.Sprintf("%d", mpi.serviceIndex))
	}

	if annotationSet || w.EnvoyExtraArgs != "" {
		extraArgsToUse := w.EnvoyExtraArgs

		// Prefer args set by pod annotation over the flag to the consul-k8s binary (h.EnvoyExtraArgs).
		if annotationSet {
			extraArgsToUse = extraArgs
		}

		// Split string into tokens.
		// e.g. "--foo bar --boo baz" --> ["--foo", "bar", "--boo", "baz"]
		tokens, err := shlex.Split(extraArgsToUse)
		if err != nil {
			return []string{}, err
		}
		for _, t := range tokens {
			if strings.Contains(t, " ") {
				t = strconv.Quote(t)
			}
			envoyExtraArgs = append(envoyExtraArgs, t)
		}
	}
	if envoyExtraArgs != nil {
		args = append(args, "--")
		args = append(args, envoyExtraArgs...)
	}
	return args, nil
}

func (w *MeshWebhook) sidecarResources(pod corev1.Pod) (corev1.ResourceRequirements, error) {
	resources := corev1.ResourceRequirements{
		Limits:   corev1.ResourceList{},
		Requests: corev1.ResourceList{},
	}
	// zeroQuantity is used for comparison to see if a quantity was explicitly
	// set.
	var zeroQuantity resource.Quantity

	// NOTE: We only want to set the limit/request if the default or annotation
	// was explicitly set. If it's not explicitly set, it will be the zero value
	// which would show up in the pod spec as being explicitly set to zero if we
	// set that key, e.g. "cpu" to zero.
	// We want it to not show up in the pod spec at all if it's not explicitly
	// set so that users aren't wondering why it's set to 0 when they didn't specify
	// a request/limit. If they have explicitly set it to 0 then it will be set
	// to 0 in the pod spec because we're doing a comparison to the zero-valued
	// struct.

	// CPU Limit.
	if anno, ok := pod.Annotations[constants.AnnotationSidecarProxyCPULimit]; ok {
		cpuLimit, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", constants.AnnotationSidecarProxyCPULimit, anno, err)
		}
		resources.Limits[corev1.ResourceCPU] = cpuLimit
	} else if w.DefaultProxyCPULimit != zeroQuantity {
		resources.Limits[corev1.ResourceCPU] = w.DefaultProxyCPULimit
	}

	// CPU Request.
	if anno, ok := pod.Annotations[constants.AnnotationSidecarProxyCPURequest]; ok {
		cpuRequest, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", constants.AnnotationSidecarProxyCPURequest, anno, err)
		}
		resources.Requests[corev1.ResourceCPU] = cpuRequest
	} else if w.DefaultProxyCPURequest != zeroQuantity {
		resources.Requests[corev1.ResourceCPU] = w.DefaultProxyCPURequest
	}

	// Memory Limit.
	if anno, ok := pod.Annotations[constants.AnnotationSidecarProxyMemoryLimit]; ok {
		memoryLimit, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", constants.AnnotationSidecarProxyMemoryLimit, anno, err)
		}
		resources.Limits[corev1.ResourceMemory] = memoryLimit
	} else if w.DefaultProxyMemoryLimit != zeroQuantity {
		resources.Limits[corev1.ResourceMemory] = w.DefaultProxyMemoryLimit
	}

	// Memory Request.
	if anno, ok := pod.Annotations[constants.AnnotationSidecarProxyMemoryRequest]; ok {
		memoryRequest, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", constants.AnnotationSidecarProxyMemoryRequest, anno, err)
		}
		resources.Requests[corev1.ResourceMemory] = memoryRequest
	} else if w.DefaultProxyMemoryRequest != zeroQuantity {
		resources.Requests[corev1.ResourceMemory] = w.DefaultProxyMemoryRequest
	}

	return resources, nil
}

// useProxyHealthCheck returns true if the pod has the annotation 'consul.hashicorp.com/use-proxy-health-check'
// set to truthy values.
func useProxyHealthCheck(pod corev1.Pod) bool {
	if v, ok := pod.Annotations[constants.AnnotationUseProxyHealthCheck]; ok {
		useProxyHealthCheck, err := strconv.ParseBool(v)
		if err != nil {
			return false
		}
		return useProxyHealthCheck
	}
	return false
}

// getStartupFailureSeconds returns number of seconds configured by the annotation 'consul.hashicorp.com/sidecar-proxy-startup-failure-seconds'
// and indicates how long we should wait for the sidecar proxy to initialize before considering the pod unhealthy.
func (w *MeshWebhook) getStartupFailureSeconds(pod corev1.Pod) int32 {
	seconds := w.DefaultSidecarProxyStartupFailureSeconds
	if v, ok := pod.Annotations[constants.AnnotationSidecarProxyStartupFailureSeconds]; ok {
		seconds, _ = strconv.Atoi(v)
	}
	if seconds > 0 {
		return int32(seconds)
	}
	return 0
}

// getLivenessFailureSeconds returns number of seconds configured by the annotation 'consul.hashicorp.com/sidecar-proxy-liveness-failure-seconds'
// and indicates how long we should wait for the sidecar proxy to initialize before considering the pod unhealthy.
func (w *MeshWebhook) getLivenessFailureSeconds(pod corev1.Pod) int32 {
	seconds := w.DefaultSidecarProxyLivenessFailureSeconds
	if v, ok := pod.Annotations[constants.AnnotationSidecarProxyLivenessFailureSeconds]; ok {
		seconds, _ = strconv.Atoi(v)
	}
	if seconds > 0 {
		return int32(seconds)
	}
	return 0
}

func (w *MeshWebhook) getSidecarProbeCheckInitialDelaySeconds(pod corev1.Pod) int32 {
	seconds := w.DefaultSidecarProbeCheckInitialDelaySeconds
	if v, ok := pod.Annotations[constants.AnnotationSidecarInitialProbeCheckDelaySeconds]; ok {
		seconds, _ = strconv.Atoi(v)
	}
	if seconds > 0 {
		return int32(seconds)
	}
	return 0
}

// getMetricsPorts creates container ports for exposing services such as prometheus.
// Prometheus in particular needs a named port for use with the operator.
// https://github.com/hashicorp/consul-k8s/pull/1440
func (w *MeshWebhook) getMetricsPorts(pod corev1.Pod) ([]corev1.ContainerPort, error) {
	enableMetrics, err := w.MetricsConfig.EnableMetrics(pod)
	if err != nil {
		return nil, fmt.Errorf("error determining if metrics are enabled: %w", err)
	}
	if !enableMetrics {
		return nil, nil
	}

	prometheusScrapePort, err := w.MetricsConfig.PrometheusScrapePort(pod)
	if err != nil {
		return nil, fmt.Errorf("error parsing prometheus port from pod: %w", err)
	}
	if prometheusScrapePort == "" {
		return nil, nil
	}

	port, err := strconv.Atoi(prometheusScrapePort)
	if err != nil {
		return nil, fmt.Errorf("error parsing prometheus port from pod: %w", err)
	}

	return []corev1.ContainerPort{
		{
			Name:          "prometheus",
			ContainerPort: int32(port),
			Protocol:      corev1.ProtocolTCP,
		},
	}, nil
}

func (w *MeshWebhook) getSidecarProbePeriodSeconds(pod corev1.Pod) int32 {
	seconds := w.DefaultSidecarProbePeriodSeconds
	if v, ok := pod.Annotations[constants.AnnotationSidecarProbePeriodSeconds]; ok {
		seconds, _ = strconv.Atoi(v)
	}
	if seconds > 0 {
		return int32(seconds)
	}
	return 0
}
func (w *MeshWebhook) getSidecarProbeFailureThreshold(pod corev1.Pod) int32 {
	threshold := w.DefaultSidecarProbeFailureThreshold
	if v, ok := pod.Annotations[constants.AnnotationSidecarProbeFailureThreshold]; ok {
		threshold, _ = strconv.Atoi(v)
	}
	if threshold > 0 {
		return int32(threshold)
	}
	return 0
}
func (w *MeshWebhook) getSidecarProbeTimeoutSeconds(pod corev1.Pod) int32 {
	seconds := w.DefaultSidecarProbeCheckTimeoutSeconds
	if v, ok := pod.Annotations[constants.AnnotationSidecarProbeCheckTimeoutSeconds]; ok {
		seconds, _ = strconv.Atoi(v)
	}
	if seconds > 0 {
		return int32(seconds)
	}
	return 0
}
