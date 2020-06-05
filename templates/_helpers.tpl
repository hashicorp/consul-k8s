{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to
this (by the DNS naming spec). Supports the legacy fullnameOverride setting
as well as the global.name setting.
*/}}
{{- define "consul.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else if .Values.global.name -}}
{{- .Values.global.name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "consul.chart" -}}
{{- printf "%s-helm" .Chart.Name | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Expand the name of the chart.
*/}}
{{- define "consul.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Compute the maximum number of unavailable replicas for the PodDisruptionBudget.
This defaults to (n/2)-1 where n is the number of members of the server cluster.
Special case of replica equaling 3 and allowing a minor disruption of 1 otherwise
use the integer value
Add a special case for replicas=1, where it should default to 0 as well.
*/}}
{{- define "consul.pdb.maxUnavailable" -}}
{{- if eq (int .Values.server.replicas) 1 -}}
{{ 0 }}
{{- else if .Values.server.disruptionBudget.maxUnavailable -}}
{{ .Values.server.disruptionBudget.maxUnavailable -}}
{{- else -}}
{{- if eq (int .Values.server.replicas) 3 -}}
{{- 1 -}}
{{- else -}}
{{- sub (div (int .Values.server.replicas) 2) 1 -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Inject extra environment vars in the format key:value, if populated
*/}}
{{- define "consul.extraEnvironmentVars" -}}
{{- if .extraEnvironmentVars -}}
{{- range $key, $value := .extraEnvironmentVars }}
- name: {{ $key }}
  value: {{ $value | quote }}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Get Consul client CA to use when auto-encrypt is enabled.
This template is for an init container.
*/}}
{{- define "consul.getAutoEncryptClientCA" -}}
- name: get-auto-encrypt-client-ca
  image: {{ .Values.global.imageK8S }}
  command:
    - "/bin/sh"
    - "-ec"
    - |
      consul-k8s get-consul-client-ca \
        -output-file=/consul/tls/client/ca/tls.crt \
        {{- if .Values.externalServers.enabled }}
        {{- if and .Values.externalServers.enabled (not .Values.externalServers.hosts) }}{{ fail "externalServers.hosts must be set if externalServers.enabled is true" }}{{ end -}}
        -server-addr={{ quote (first .Values.externalServers.hosts) }} \
        -server-port={{ .Values.externalServers.httpsPort }} \
        {{- if .Values.externalServers.tlsServerName }}
        -tls-server-name={{ .Values.externalServers.tlsServerName }} \
        {{- end }}
        {{- if not .Values.externalServers.useSystemRoots }}
        -ca-file=/consul/tls/ca/tls.crt
        {{- end }}
        {{- else }}
        -server-addr={{ template "consul.fullname" . }}-server \
        -server-port=8501 \
        -ca-file=/consul/tls/ca/tls.crt
        {{- end }}
  volumeMounts:
    {{- if not (and .Values.externalServers.enabled .Values.externalServers.useSystemRoots) }}
    - name: consul-ca-cert
      mountPath: /consul/tls/ca
    {{- end }}
    - name: consul-auto-encrypt-ca-cert
      mountPath: /consul/tls/client/ca
  resources:
    requests:
      memory: "25Mi"
      cpu: "50m"
    limits:
      memory: "25Mi"
      cpu: "50m"
{{- end -}}
