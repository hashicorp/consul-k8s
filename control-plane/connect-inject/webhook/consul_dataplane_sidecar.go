package webhook

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/shlex"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/common"
	"github.com/hashicorp/consul-k8s/control-plane/connect-inject/constants"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

const (
	consulDataplaneDNSBindHost = "127.0.0.1"
	consulDataplaneDNSBindPort = 8600
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

	probe := &corev1.Probe{
		Handler: corev1.Handler{
			TCPSocket: &corev1.TCPSocketAction{
				Port: intstr.FromInt(constants.ProxyDefaultInboundPort + mpi.serviceIndex),
			},
		},
		InitialDelaySeconds: 1,
	}
	container := corev1.Container{
		Name:      containerName,
		Image:     w.ImageConsulDataplane,
		Resources: resources,
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
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Args:           args,
		ReadinessProbe: probe,
		LivenessProbe:  probe,
	}

	if w.AuthMethod != "" {
		container.VolumeMounts = append(container.VolumeMounts, saTokenVolumeMount)
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

	var lifecycle corev1.Lifecycle

	preStop, err := w.envoySidecarGracefulShutdown(pod)
	if err == nil && preStop != nil {
		lifecycle.PreStop = preStop
	}

	postStart, err := w.envoySidecarHoldApplicationUntilProxyStarts(pod)
	if err == nil && postStart != nil {
		lifecycle.PostStart = postStart
	}

	container.Lifecycle = &lifecycle

	tproxyEnabled, err := common.TransparentProxyEnabled(namespace, pod, w.EnableTransparentProxy)
	if err != nil {
		return corev1.Container{}, err
	}

	// If not running in transparent proxy mode and in an OpenShift environment,
	// skip setting the security context and let OpenShift set it for us.
	// When transparent proxy is enabled, then consul-dataplane needs to run as our specific user
	// so that traffic redirection will work.
	if tproxyEnabled || !w.EnableOpenShift {
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
			if c.SecurityContext != nil && c.SecurityContext.RunAsUser != nil && *c.SecurityContext.RunAsUser == sidecarUserAndGroupID && c.Image != w.ImageConsulDataplane {
				return corev1.Container{}, fmt.Errorf("container %q has runAsUser set to the same UID \"%d\" as consul-dataplane which is not allowed", c.Name, sidecarUserAndGroupID)
			}
		}
		container.SecurityContext = &corev1.SecurityContext{
			RunAsUser:              pointer.Int64(sidecarUserAndGroupID),
			RunAsGroup:             pointer.Int64(sidecarUserAndGroupID),
			RunAsNonRoot:           pointer.Bool(true),
			ReadOnlyRootFilesystem: pointer.Bool(true),
		}
	}

	return container, nil
}

// Configures graceful shut down for the sidecar.
func (w *MeshWebhook) envoySidecarGracefulShutdown(pod corev1.Pod) (*corev1.Handler, error) {

	grace, err := strconv.ParseBool(pod.Annotations[constants.AnnotationSidecarProxyGracefulShutdown])

	if err != nil || !grace {
		return nil, err
	}

	preStop := &corev1.Handler{
		Exec: &corev1.ExecAction{
			Command: []string{
				"/bin/sh",
				"-c",
				"while [ $(netstat -plunt | grep tcp | grep -v envoy | grep -v consul-dataplane | wc -l) -ne 0 ]; do sleep 1; done",
			},
		},
	}

	return preStop, nil
}

// Ensures that the sidecar is the first container to start up.
func (w *MeshWebhook) envoySidecarHoldApplicationUntilProxyStarts(pod corev1.Pod) (*corev1.Handler, error) {

	hold, err := strconv.ParseBool(pod.Annotations[constants.AnnotationSidecarProxyHoldApplicationUntilProxyStarts])

	if err != nil || !hold {
		return nil, err
	}
	postStart := &corev1.Handler{
		Exec: &corev1.ExecAction{
			Command: []string{
				"/bin/sh",
				"-c",
				`total_time=0; until wget --spider localhost:19000;` +
					`do echo Waiting for Sidecar;` +
					`sleep 3; total_time=$(($total_time + 3)); echo $total_time;` +
					`if [ $total_time -gt 120 ]; then echo Sidecar not running, timeout reached. Exiting....; exit 1; fi; done;` +
					`echo Sidecar available`,
			},
		},
	}

	return postStart, err

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

	args := []string{
		"-addresses", w.ConsulAddress,
		"-grpc-port=" + strconv.Itoa(w.ConsulConfig.GRPCPort),
		"-proxy-service-id-path=" + proxyIDFileName,
		"-log-level=" + w.LogLevel,
		"-log-json=" + strconv.FormatBool(w.LogJSON),
		"-envoy-concurrency=" + strconv.Itoa(envoyConcurrency),
	}

	if w.SkipServerWatch {
		args = append(args, "-server-watch-disabled=true")
	}

	if w.AuthMethod != "" {
		args = append(args,
			"-credential-type=login",
			"-login-auth-method="+w.AuthMethod,
			"-login-bearer-token-path="+bearerTokenFile,
			"-login-meta="+fmt.Sprintf("pod=%s/%s", namespace.Name, pod.Name),
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
			args = append(args, "-ca-certs="+constants.ConsulCAFile)
		}
	} else {
		args = append(args, "-tls-disabled")
	}

	if mpi.serviceName != "" {
		args = append(args, fmt.Sprintf("-envoy-admin-bind-port=%d", 19000+mpi.serviceIndex))
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
			args = append(args, "-telemetry-prom-service-metrics-url="+fmt.Sprintf("http://127.0.0.1:%s%s", serviceMetricsPort, serviceMetricsPath))
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
	if w.EnableConsulDNS {
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
