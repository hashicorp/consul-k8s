package connectinject

import (
	"bytes"
	"strings"
	"text/template"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

type sidecarContainerCommandData struct {
	AuthMethod      string
	ConsulNamespace string
	Resources corev1.ResourceRequirements
}

func (h *Handler) envoySidecar(pod *corev1.Pod, k8sNamespace string) (corev1.Container, error) {
	templateData := sidecarContainerCommandData{
		AuthMethod:      h.AuthMethod,
		ConsulNamespace: h.consulNamespace(k8sNamespace),
	}

	if h.Resources {
		templateData.Resources = corev1.ResourceRequirements{
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(h.CPULimit),
				corev1.ResourceMemory: resource.MustParse(h.MemoryLimit),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(h.CPULimit),
				corev1.ResourceMemory: resource.MustParse(h.MemoryLimit),
			},
		}
	}

	// Render the command
	var buf bytes.Buffer
	tpl := template.Must(template.New("root").Parse(strings.TrimSpace(
		sidecarPreStopCommandTpl)))
	err := tpl.Execute(&buf, &templateData)
	if err != nil {
		return corev1.Container{}, err
	}

	container := corev1.Container{
		Name:  "consul-connect-envoy-sidecar",
		Image: h.ImageEnvoy,
		Env: []corev1.EnvVar{
			{
				Name: "HOST_IP",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
				},
			},
		},
		Resources: templateData.Resources,
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
		Command: []string{
			"envoy",
			"--max-obj-name-len", "256",
			"--config-path", "/consul/connect-inject/envoy-bootstrap.yaml",
		},
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
&& /consul/connect-inject/consul logout \
  -token-file="/consul/connect-inject/acl-token"
{{- end}}
`
