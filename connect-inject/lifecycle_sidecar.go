package connectinject

import (
	"bytes"
	"strings"
	"text/template"
	"time"

	corev1 "k8s.io/api/core/v1"
)

const defaultSyncPeriod = 10 // in seconds

type lifecycleContainerCommandData struct {
	AuthMethod      string
	SyncPeriodInSec int
}

func (h *Handler) lifecycleSidecar(pod *corev1.Pod) (corev1.Container, error) {
	// Check that the sync period is valid if provided
	var syncPeriodInSec int
	if period, ok := pod.Annotations[annotationSyncPeriod]; ok {
		syncPeriodDuration, err := time.ParseDuration(period)
		if err != nil {
			return corev1.Container{}, err
		}

		syncPeriodInSec = int(syncPeriodDuration.Seconds())
	}

	// Set the sync period to the default value if it's zero
	if syncPeriodInSec <= 0 {
		syncPeriodInSec = defaultSyncPeriod
	}

	data := lifecycleContainerCommandData{
		AuthMethod:      h.AuthMethod,
		SyncPeriodInSec: syncPeriodInSec,
	}

	envVariables := []corev1.EnvVar{
		{
			Name: "HOST_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{FieldPath: "status.hostIP"},
			},
		},
	}

	if h.ConsulCACert != "" {
		envVariables = append(envVariables,
			// Kubernetes will interpolate HOST_IP when creating this environment
			// variable.
			corev1.EnvVar{
				Name:  "CONSUL_HTTP_ADDR",
				Value: "https://$(HOST_IP):8501",
			},
			corev1.EnvVar{
				Name:  "CONSUL_CACERT",
				Value: "/consul/connect-inject/consul-ca.pem",
			},
		)
	} else {
		envVariables = append(envVariables,
			// Kubernetes will interpolate HOST_IP when creating this environment
			// variable.
			corev1.EnvVar{
				Name:  "CONSUL_HTTP_ADDR",
				Value: "$(HOST_IP):8500",
			})
	}

	// Render the command
	var buf bytes.Buffer
	tpl := template.Must(template.New("root").Parse(strings.TrimSpace(
		lifecycleContainerCommandTpl)))
	err := tpl.Execute(&buf, &data)
	if err != nil {
		return corev1.Container{}, err
	}

	return corev1.Container{
		Name:  "consul-connect-lifecycle-sidecar",
		Image: h.ImageConsul,
		Env:   envVariables,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      volumeName,
				MountPath: "/consul/connect-inject",
			},
		},
		Command: []string{"/bin/sh", "-ec", buf.String()},
	}, nil
}

const lifecycleContainerCommandTpl = `
while true;
do /bin/consul services register \
  {{- if .AuthMethod }}
  -token-file="/consul/connect-inject/acl-token" \
  {{- end }}
  /consul/connect-inject/service.hcl;
sleep {{ .SyncPeriodInSec }};
done;
`
