package connectinject

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

func (w *MeshWebhook) consulDataplaneSidecar(namespace corev1.Namespace, pod corev1.Pod, mpi multiPortInfo) (corev1.Container, error) {
	resources, err := w.sidecarResources(pod)
	if err != nil {
		return corev1.Container{}, err
	}

	multiPort := mpi.serviceName != ""
	cmd, err := w.getContainerSidecarCommand(namespace, mpi)
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
				Port: intstr.FromInt(EnvoyInboundListenerPort + mpi.serviceIndex),
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
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command:        cmd,
		ReadinessProbe: probe,
		LivenessProbe:  probe,
	}

	// Add any extra VolumeMounts.
	if _, ok := pod.Annotations[annotationConsulSidecarUserVolumeMount]; ok {
		var volumeMount []corev1.VolumeMount
		err := json.Unmarshal([]byte(pod.Annotations[annotationConsulSidecarUserVolumeMount]), &volumeMount)
		if err != nil {
			return corev1.Container{}, err
		}
		container.VolumeMounts = append(container.VolumeMounts, volumeMount...)
	}

	tproxyEnabled, err := transparentProxyEnabled(namespace, pod, w.EnableTransparentProxy)
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

func (w *MeshWebhook) getContainerSidecarCommand(namespace corev1.Namespace, mpi multiPortInfo) ([]string, error) {
	proxyIDFileName := "/consul/connect-inject/proxyid"
	if mpi.serviceName != "" {
		proxyIDFileName = fmt.Sprintf("/consul/connect-inject/proxyid-%s", mpi.serviceName)
	}
	aclTokenFile := "/consul/connect-inject/acl-token"
	if mpi.serviceName != "" {
		aclTokenFile = fmt.Sprintf("/consul/connect-inject/acl-token-%s", mpi.serviceName)
	}
	cmd := []string{
		"consul-dataplane",
		"-addresses=" + w.ConsulAddress,
		"-grpc-port=" + strconv.Itoa(w.ConsulGRPCPort),
		"-proxy-service-id=" + fmt.Sprintf("$(cat %s)", proxyIDFileName),
		"-service-node-name=" + ConsulNodeName,
		"-log-level=" + w.LogLevel,
		"-log-json=" + strconv.FormatBool(w.LogJSON),
	}
	if w.AuthMethod != "" {
		cmd = append(cmd, "-static-token="+fmt.Sprintf("$(cat %s)", aclTokenFile))
	}
	if w.EnableNamespaces {
		cmd = append(cmd, "-service-namespace="+w.consulNamespace(namespace.Name))
	}
	if w.ConsulPartition != "" {
		cmd = append(cmd, "-service-partition="+w.ConsulPartition)
	}
	if w.TLSEnabled {
		cmd = append(cmd, "-tls-enabled", "-tls-server-name="+w.ConsulTLSServerName)
		if w.ConsulCACert != "" {
			cmd = append(cmd, "-tls-ca-certs-path=/consul/connect-inject/consul-ca.pem")
		}
	}

	if mpi.serviceName != "" {
		cmd = append(cmd, fmt.Sprintf("-envoy-admin-bind-port=%d", 19000+mpi.serviceIndex))
	}

	// todo (agentless): we need to enable this once we support passing extra flags to envoy through consul-dataplane
	//if multiPortSvcName != "" {
	//	// --base-id is needed so multiple Envoy proxies can run on the same host.
	//	cmd = append(cmd, "--base-id", fmt.Sprintf("%d", multiPortSvcIdx))
	//}
	// Check to see if the user has overriden concurrency via an annotation.
	//if pod.Annotations[annotationEnvoyProxyConcurrency] != "" {
	//	val, err := strconv.ParseInt(pod.Annotations[annotationEnvoyProxyConcurrency], 10, 64)
	//	if err != nil {
	//		return nil, fmt.Errorf("unable to parse annotation: %s", annotationEnvoyProxyConcurrency)
	//	}
	//	if val < 0 {
	//		return nil, fmt.Errorf("invalid envoy concurrency, must be >= 0: %s", pod.Annotations[annotationEnvoyProxyConcurrency])
	//	} else {
	//		cmd = append(cmd, "--concurrency", pod.Annotations[annotationEnvoyProxyConcurrency])
	//	}
	//} else {
	//	// Use the default concurrency.
	//	cmd = append(cmd, "--concurrency", fmt.Sprintf("%d", w.DefaultEnvoyProxyConcurrency))
	//}
	//
	//extraArgs, annotationSet := pod.Annotations[annotationEnvoyExtraArgs]
	//
	//if annotationSet || w.EnvoyExtraArgs != "" {
	//	extraArgsToUse := w.EnvoyExtraArgs
	//
	//	// Prefer args set by pod annotation over the flag to the consul-k8s binary (h.EnvoyExtraArgs).
	//	if annotationSet {
	//		extraArgsToUse = extraArgs
	//	}
	//
	//	// Split string into tokens.
	//	// e.g. "--foo bar --boo baz" --> ["--foo", "bar", "--boo", "baz"]
	//	tokens, err := shlex.Split(extraArgsToUse)
	//	if err != nil {
	//		return []string{}, err
	//	}
	//	for _, t := range tokens {
	//		if strings.Contains(t, " ") {
	//			t = strconv.Quote(t)
	//		}
	//		cmd = append(cmd, t)
	//	}
	//}
	cmd = append([]string{"/bin/sh", "-ec"}, strings.Join(cmd, " "))
	return cmd, nil
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
	if anno, ok := pod.Annotations[annotationSidecarProxyCPULimit]; ok {
		cpuLimit, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", annotationSidecarProxyCPULimit, anno, err)
		}
		resources.Limits[corev1.ResourceCPU] = cpuLimit
	} else if w.DefaultProxyCPULimit != zeroQuantity {
		resources.Limits[corev1.ResourceCPU] = w.DefaultProxyCPULimit
	}

	// CPU Request.
	if anno, ok := pod.Annotations[annotationSidecarProxyCPURequest]; ok {
		cpuRequest, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", annotationSidecarProxyCPURequest, anno, err)
		}
		resources.Requests[corev1.ResourceCPU] = cpuRequest
	} else if w.DefaultProxyCPURequest != zeroQuantity {
		resources.Requests[corev1.ResourceCPU] = w.DefaultProxyCPURequest
	}

	// Memory Limit.
	if anno, ok := pod.Annotations[annotationSidecarProxyMemoryLimit]; ok {
		memoryLimit, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", annotationSidecarProxyMemoryLimit, anno, err)
		}
		resources.Limits[corev1.ResourceMemory] = memoryLimit
	} else if w.DefaultProxyMemoryLimit != zeroQuantity {
		resources.Limits[corev1.ResourceMemory] = w.DefaultProxyMemoryLimit
	}

	// Memory Request.
	if anno, ok := pod.Annotations[annotationSidecarProxyMemoryRequest]; ok {
		memoryRequest, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", annotationSidecarProxyMemoryRequest, anno, err)
		}
		resources.Requests[corev1.ResourceMemory] = memoryRequest
	} else if w.DefaultProxyMemoryRequest != zeroQuantity {
		resources.Requests[corev1.ResourceMemory] = w.DefaultProxyMemoryRequest
	}

	return resources, nil
}
