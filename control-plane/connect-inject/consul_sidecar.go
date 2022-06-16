package connectinject

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// consulSidecar starts the consul-sidecar command to only run
// the metrics merging server when metrics merging feature is enabled.
// It always disables service registration because for connect we no longer
// need to keep services registered as this is handled in the endpoints-controller.
func (w *MeshWebhook) consulSidecar(pod corev1.Pod) (corev1.Container, error) {
	metricsPorts, err := w.MetricsConfig.mergedMetricsServerConfiguration(pod)
	if err != nil {
		return corev1.Container{}, err
	}

	resources, err := w.consulSidecarResources(pod)
	if err != nil {
		return corev1.Container{}, err
	}

	command := []string{
		"consul-k8s-control-plane",
		"consul-sidecar",
		"-enable-service-registration=false",
		"-enable-metrics-merging=true",
		fmt.Sprintf("-merged-metrics-port=%s", metricsPorts.mergedPort),
		fmt.Sprintf("-service-metrics-port=%s", metricsPorts.servicePort),
		fmt.Sprintf("-service-metrics-path=%s", metricsPorts.servicePath),
		fmt.Sprintf("-log-level=%s", w.LogLevel),
		fmt.Sprintf("-log-json=%t", w.LogJSON),
	}

	return corev1.Container{
		Name:  "consul-sidecar",
		Image: w.ImageConsulK8S,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command:   command,
		Resources: resources,
	}, nil
}

func (w *MeshWebhook) consulSidecarResources(pod corev1.Pod) (corev1.ResourceRequirements, error) {
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
	if anno, ok := pod.Annotations[annotationConsulSidecarCPULimit]; ok {
		cpuLimit, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", annotationConsulSidecarCPULimit, anno, err)
		}
		resources.Limits[corev1.ResourceCPU] = cpuLimit
	} else if w.DefaultConsulSidecarResources.Limits[corev1.ResourceCPU] != zeroQuantity {
		resources.Limits[corev1.ResourceCPU] = w.DefaultConsulSidecarResources.Limits[corev1.ResourceCPU]
	}

	// CPU Request.
	if anno, ok := pod.Annotations[annotationConsulSidecarCPURequest]; ok {
		cpuRequest, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", annotationConsulSidecarCPURequest, anno, err)
		}
		resources.Requests[corev1.ResourceCPU] = cpuRequest
	} else if w.DefaultConsulSidecarResources.Requests[corev1.ResourceCPU] != zeroQuantity {
		resources.Requests[corev1.ResourceCPU] = w.DefaultConsulSidecarResources.Requests[corev1.ResourceCPU]
	}

	// Memory Limit.
	if anno, ok := pod.Annotations[annotationConsulSidecarMemoryLimit]; ok {
		memoryLimit, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", annotationConsulSidecarMemoryLimit, anno, err)
		}
		resources.Limits[corev1.ResourceMemory] = memoryLimit
	} else if w.DefaultConsulSidecarResources.Limits[corev1.ResourceMemory] != zeroQuantity {
		resources.Limits[corev1.ResourceMemory] = w.DefaultConsulSidecarResources.Limits[corev1.ResourceMemory]
	}

	// Memory Request.
	if anno, ok := pod.Annotations[annotationConsulSidecarMemoryRequest]; ok {
		memoryRequest, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", annotationConsulSidecarMemoryRequest, anno, err)
		}
		resources.Requests[corev1.ResourceMemory] = memoryRequest
	} else if w.DefaultConsulSidecarResources.Requests[corev1.ResourceMemory] != zeroQuantity {
		resources.Requests[corev1.ResourceMemory] = w.DefaultConsulSidecarResources.Requests[corev1.ResourceMemory]
	}

	return resources, nil
}
