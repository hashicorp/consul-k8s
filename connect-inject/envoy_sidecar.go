package connectinject

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"text/template"

	"github.com/google/shlex"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type sidecarContainerCommandData struct {
	AuthMethod      string
	ConsulNamespace string
}

func (h *Handler) envoySidecar(pod corev1.Pod, k8sNamespace string) (corev1.Container, error) {
	templateData := sidecarContainerCommandData{
		AuthMethod:      h.AuthMethod,
		ConsulNamespace: h.consulNamespace(k8sNamespace),
	}

	// Render the command
	var buf bytes.Buffer
	tpl := template.Must(template.New("root").Parse(strings.TrimSpace(
		sidecarPreStopCommandTpl)))
	err := tpl.Execute(&buf, &templateData)
	if err != nil {
		return corev1.Container{}, err
	}

	resources, err := h.envoySidecarResources(pod)
	if err != nil {
		return corev1.Container{}, err
	}

	cmd, err := h.getContainerSidecarCommand(pod)
	if err != nil {
		return corev1.Container{}, err
	}

	container := corev1.Container{
		Name:  "envoy-sidecar",
		Image: h.ImageEnvoy,
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
		Lifecycle: &corev1.Lifecycle{
			PreStop: &corev1.Handler{
				Exec: &corev1.ExecAction{
					Command: []string{
						"/bin/sh",
						"-ec",
						buf.String(),
					},
				},
			},
		},
		Command: cmd,
	}
	if h.ConsulCACert != "" {
		caCertEnvVar := corev1.EnvVar{
			Name:  "CONSUL_CACERT",
			Value: "/consul/connect-inject/consul-ca.pem",
		}
		consulAddrEnvVar := corev1.EnvVar{
			Name:  "CONSUL_HTTP_ADDR",
			Value: "https://$(HOST_IP):8501",
		}
		container.Env = append(container.Env, caCertEnvVar, consulAddrEnvVar)
	} else {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  "CONSUL_HTTP_ADDR",
			Value: "$(HOST_IP):8500",
		})
	}
	return container, nil
}
func (h *Handler) getContainerSidecarCommand(pod corev1.Pod) ([]string, error) {
	cmd := []string{
		"envoy",
		"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
	}

	extraArgs, annotationSet := pod.Annotations[annotationEnvoyExtraArgs]

	if annotationSet || h.EnvoyExtraArgs != "" {

		extraArgsToUse := h.EnvoyExtraArgs

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

func (h *Handler) envoySidecarResources(pod corev1.Pod) (corev1.ResourceRequirements, error) {
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
	} else if h.DefaultProxyCPULimit != zeroQuantity {
		resources.Limits[corev1.ResourceCPU] = h.DefaultProxyCPULimit
	}

	// CPU Request.
	if anno, ok := pod.Annotations[annotationSidecarProxyCPURequest]; ok {
		cpuRequest, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", annotationSidecarProxyCPURequest, anno, err)
		}
		resources.Requests[corev1.ResourceCPU] = cpuRequest
	} else if h.DefaultProxyCPURequest != zeroQuantity {
		resources.Requests[corev1.ResourceCPU] = h.DefaultProxyCPURequest
	}

	// Memory Limit.
	if anno, ok := pod.Annotations[annotationSidecarProxyMemoryLimit]; ok {
		memoryLimit, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", annotationSidecarProxyMemoryLimit, anno, err)
		}
		resources.Limits[corev1.ResourceMemory] = memoryLimit
	} else if h.DefaultProxyMemoryLimit != zeroQuantity {
		resources.Limits[corev1.ResourceMemory] = h.DefaultProxyMemoryLimit
	}

	// Memory Request.
	if anno, ok := pod.Annotations[annotationSidecarProxyMemoryRequest]; ok {
		memoryRequest, err := resource.ParseQuantity(anno)
		if err != nil {
			return corev1.ResourceRequirements{}, fmt.Errorf("parsing annotation %s:%q: %s", annotationSidecarProxyMemoryRequest, anno, err)
		}
		resources.Requests[corev1.ResourceMemory] = memoryRequest
	} else if h.DefaultProxyMemoryRequest != zeroQuantity {
		resources.Requests[corev1.ResourceMemory] = h.DefaultProxyMemoryRequest
	}

	return resources, nil
}

const sidecarPreStopCommandTpl = `
/consul/connect-inject/consul services deregister \
  {{- if .AuthMethod }}
  -token-file="/consul/connect-inject/acl-token" \
  {{- end }}
  {{- if .ConsulNamespace }}
  -namespace="{{ .ConsulNamespace }}" \
  {{- end }}
  /consul/connect-inject/service.hcl

{{- if .AuthMethod }}
/consul/connect-inject/consul logout \
  -token-file="/consul/connect-inject/acl-token"
{{- end}}
`
