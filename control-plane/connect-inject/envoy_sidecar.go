package connectinject

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/shlex"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func (w *ConnectWebhook) envoySidecar(namespace corev1.Namespace, pod corev1.Pod, mpi multiPortInfo) (corev1.Container, error) {
	resources, err := w.envoySidecarResources(pod)
	if err != nil {
		return corev1.Container{}, err
	}

	multiPort := mpi.serviceName != ""
	cmd, err := w.getContainerSidecarCommand(pod, mpi.serviceName, mpi.serviceIndex)
	if err != nil {
		return corev1.Container{}, err
	}

	containerName := envoySidecarContainer
	if multiPort {
		containerName = fmt.Sprintf("%s-%s", envoySidecarContainer, mpi.serviceName)
	}

	container := corev1.Container{
		Name:  containerName,
		Image: w.ImageEnvoy,
		Env: []corev1.EnvVar{
			{
				Name: "HOST_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
				},
			},
		},
		Resources: resources,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command: cmd,
	}

	tproxyEnabled, err := transparentProxyEnabled(namespace, pod, w.EnableTransparentProxy)
	if err != nil {
		return corev1.Container{}, err
	}

	// If not running in transparent proxy mode and in an OpenShift environment,
	// skip setting the security context and let OpenShift set it for us.
	// When transparent proxy is enabled, then Envoy needs to run as our specific user
	// so that traffic redirection will work.
	if tproxyEnabled || !w.EnableOpenShift {
		if pod.Spec.SecurityContext != nil {
			// User container and Envoy container cannot have the same UID.
			if pod.Spec.SecurityContext.RunAsUser != nil && *pod.Spec.SecurityContext.RunAsUser == envoyUserAndGroupID {
				return corev1.Container{}, fmt.Errorf("pod security context cannot have the same uid as envoy: %v", envoyUserAndGroupID)
			}
		}
		// Ensure that none of the user's containers have the same UID as Envoy. At this point in injection the handler
		// has only injected init containers so all containers defined in pod.Spec.Containers are from the user.
		for _, c := range pod.Spec.Containers {
			// User container and Envoy container cannot have the same UID.
			if c.SecurityContext != nil && c.SecurityContext.RunAsUser != nil && *c.SecurityContext.RunAsUser == envoyUserAndGroupID && c.Image != w.ImageEnvoy {
				return corev1.Container{}, fmt.Errorf("container %q has runAsUser set to the same uid %q as envoy which is not allowed", c.Name, envoyUserAndGroupID)
			}
		}
		container.SecurityContext = &corev1.SecurityContext{
			RunAsUser:              pointerToInt64(envoyUserAndGroupID),
			RunAsGroup:             pointerToInt64(envoyUserAndGroupID),
			RunAsNonRoot:           pointerToBool(true),
			ReadOnlyRootFilesystem: pointerToBool(true),
		}
	}

	return container, nil
}
func (w *ConnectWebhook) getContainerSidecarCommand(pod corev1.Pod, multiPortSvcName string, multiPortSvcIdx int) ([]string, error) {
	bootstrapFile := "/consul/connect-inject/envoy-bootstrap.yaml"
	if multiPortSvcName != "" {
		bootstrapFile = fmt.Sprintf("/consul/connect-inject/envoy-bootstrap-%s.yaml", multiPortSvcName)
	}
	cmd := []string{
		"envoy",
		"--config-path", bootstrapFile,
	}
	if multiPortSvcName != "" {
		// --base-id is needed so multiple Envoy proxies can run on the same host.
		cmd = append(cmd, "--base-id", fmt.Sprintf("%d", multiPortSvcIdx))
	}

	extraArgs, annotationSet := pod.Annotations[annotationEnvoyExtraArgs]

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
			cmd = append(cmd, t)
		}
	}
	return cmd, nil
}

func (w *ConnectWebhook) envoySidecarResources(pod corev1.Pod) (corev1.ResourceRequirements, error) {
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
	// We want it to not show up in the pod spec at all if if it's not explicitly
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
