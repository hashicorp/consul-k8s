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

{{- define "consul.vaultSecretTemplate" -}}
 |
            {{ "{{" }}- with secret "{{ .secretName }}" -{{ "}}" }}
            {{ "{{" }}- {{ printf ".Data.data.%s" .secretKey }} -{{ "}}" }}
            {{ "{{" }}- end -{{ "}}" }}
{{- end -}}

{{- define "consul.serverTLSCATemplate" -}}
 |
            {{ "{{" }}- with secret "{{ .Values.global.tls.caCert.secretName }}" -{{ "}}" }}
            {{ "{{" }}- .Data.certificate -{{ "}}" }}
            {{ "{{" }}- end -{{ "}}" }}
{{- end -}}

{{- define "consul.serverTLSCertTemplate" -}}
 |
            {{ "{{" }}- with secret "{{ .Values.server.serverCert.secretName }}" "{{ printf "common_name=server.%s.%s" .Values.global.datacenter .Values.global.domain }}"
            "alt_names={{ include "consul.serverTLSAltNames" . }}" "ip_sans=127.0.0.1{{ include "consul.serverAdditionalIPSANs" . }}" -{{ "}}" }}
            {{ "{{" }}- .Data.certificate -{{ "}}" }}
            {{ "{{" }}- if .Data.ca_chain -{{ "}}" }}
            {{ "{{" }}- $lastintermediatecertindex := len .Data.ca_chain | subtract 1 -{{ "}}" }}
            {{ "{{" }} range $index, $cacert := .Data.ca_chain {{ "}}" }}
            {{ "{{" }} if (lt $index $lastintermediatecertindex) {{ "}}" }}
            {{ "{{" }} $cacert {{ "}}" }}
            {{ "{{" }} end {{ "}}" }}
            {{ "{{" }} end {{ "}}" }}
            {{ "{{" }}- end -{{ "}}" }}
            {{ "{{" }}- end -{{ "}}" }}
{{- end -}}

{{- define "consul.serverTLSKeyTemplate" -}}
 |
            {{ "{{" }}- with secret "{{ .Values.server.serverCert.secretName }}" "{{ printf "common_name=server.%s.%s" .Values.global.datacenter .Values.global.domain }}"
            "alt_names={{ include "consul.serverTLSAltNames" . }}" "ip_sans=127.0.0.1{{ include "consul.serverAdditionalIPSANs" . }}" -{{ "}}" }}
            {{ "{{" }}- .Data.private_key -{{ "}}" }}
            {{ "{{" }}- end -{{ "}}" }}
{{- end -}}

{{- define "consul.serverTLSAltNames" -}}
{{- $name := include "consul.fullname" . -}}
{{- $ns := .Release.Namespace -}}
{{ printf "localhost,%s-server,*.%s-server,*.%s-server.%s,%s-server.%s,*.%s-server.%s.svc,%s-server.%s.svc,*.server.%s.%s" $name $name $name $ns $name $ns $name $ns $name $ns (.Values.global.datacenter ) (.Values.global.domain) }}{{ include "consul.serverAdditionalDNSSANs" . }}
{{- end -}}

{{- define "consul.serverAdditionalDNSSANs" -}}
{{- if .Values.global.tls -}}{{- if .Values.global.tls.serverAdditionalDNSSANs -}}{{- range $san := .Values.global.tls.serverAdditionalDNSSANs }},{{ $san }} {{- end -}}{{- end -}}{{- end -}}
{{- end -}}

{{- define "consul.serverAdditionalIPSANs" -}}
{{- if .Values.global.tls -}}{{- if .Values.global.tls.serverAdditionalIPSANs -}}{{- range $ipsan := .Values.global.tls.serverAdditionalIPSANs }},{{ $ipsan }} {{- end -}}{{- end -}}{{- end -}}
{{- end -}}

{{- define "consul.vaultReplicationTokenTemplate" -}}
|
          {{ "{{" }}- with secret "{{ .Values.global.acls.replicationToken.secretName }}" -{{ "}}" }}
          {{ "{{" }}- {{ printf ".Data.data.%s" .Values.global.acls.replicationToken.secretKey }} -{{ "}}" }}
          {{ "{{" }}- end -{{ "}}" }}
{{- end -}}

{{- define "consul.vaultReplicationTokenConfigTemplate" -}}
|
          {{ "{{" }}- with secret "{{ .Values.global.acls.replicationToken.secretName }}" -{{ "}}" }}
          acl { tokens { agent = "{{ "{{" }}- {{ printf ".Data.data.%s" .Values.global.acls.replicationToken.secretKey }} -{{ "}}" }}", replication = "{{ "{{" }}- {{ printf ".Data.data.%s" .Values.global.acls.replicationToken.secretKey }} -{{ "}}" }}" }}
          {{ "{{" }}- end -{{ "}}" }}
{{- end -}}

{{- define "consul.vaultBootstrapTokenConfigTemplate" -}}
|
          {{ "{{" }}- with secret "{{ .Values.global.acls.bootstrapToken.secretName }}" -{{ "}}" }}
          acl { tokens { initial_management = "{{ "{{" }}- {{ printf ".Data.data.%s" .Values.global.acls.bootstrapToken.secretKey }} -{{ "}}" }}" }}
          {{ "{{" }}- end -{{ "}}" }}
{{- end -}}

{{/*
Sets up the extra-from-values config file passed to consul and then uses sed to do any necessary
substitution for HOST_IP/POD_IP/HOSTNAME. Useful for dogstats telemetry. The output file
is passed to consul as a -config-file param on command line.
*/}}
{{- define "consul.extraconfig" -}}
              mkdir -p /consul/extra-config
              cp /consul/config/extra-from-values.json /consul/extra-config/extra-from-values.json
              [ -n "${HOST_IP}" ] && sed -Ei "s|HOST_IP|${HOST_IP?}|g" /consul/extra-config/extra-from-values.json
              [ -n "${POD_IP}" ] && sed -Ei "s|POD_IP|${POD_IP?}|g" /consul/extra-config/extra-from-values.json
              [ -n "${HOSTNAME}" ] && sed -Ei "s|HOSTNAME|${HOSTNAME?}|g" /consul/extra-config/extra-from-values.json
{{- end -}}

{{/*
Sets up a list of recusor flags for Consul agents by iterating over the IPs of every nameserver
in /etc/resolv.conf and concatenating them into a string of arguments that can be passed directly
to the consul agent command.
*/}}
{{- define "consul.recursors" -}}
              recursor_flags=""
              for ip in $(cat /etc/resolv.conf | grep nameserver | cut -d' ' -f2)
              do
                 recursor_flags="$recursor_flags -recursor=$ip"
              done
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
      consul-k8s-control-plane get-consul-client-ca \
        -output-file=/consul/tls/client/ca/tls.crt \
        -consul-api-timeout={{ .Values.global.consulAPITimeout }} \
        {{- if .Values.externalServers.enabled }}
        {{- if and .Values.externalServers.enabled (not .Values.externalServers.hosts) }}{{ fail "externalServers.hosts must be set if externalServers.enabled is true" }}{{ end -}}
        -server-addr={{ quote (first .Values.externalServers.hosts) }} \
        -server-port={{ .Values.externalServers.httpsPort }} \
        {{- if .Values.externalServers.tlsServerName }}
        -tls-server-name={{ .Values.externalServers.tlsServerName }} \
        {{- end }}
        {{- else }}
        -server-addr={{ template "consul.fullname" . }}-server \
        -server-port=8501 \
        {{- end }}
        {{- if or (not .Values.externalServers.enabled) (and .Values.externalServers.enabled (not .Values.externalServers.useSystemRoots)) }}
        {{- if .Values.global.secretsBackend.vault.enabled }}
        -ca-file=/vault/secrets/serverca.crt
        {{- else }}
        -ca-file=/consul/tls/ca/tls.crt
        {{- end }}
        {{- end }}
  volumeMounts:
    {{- if not (and .Values.externalServers.enabled .Values.externalServers.useSystemRoots) }}
    {{- if not .Values.global.secretsBackend.vault.enabled }}
    - name: consul-ca-cert
      mountPath: /consul/tls/ca
    {{- end }}
    {{- end }}
    - name: consul-auto-encrypt-ca-cert
      mountPath: /consul/tls/client/ca
  resources:
    requests:
      memory: "50Mi"
      cpu: "50m"
    limits:
      memory: "50Mi"
      cpu: "50m"
{{- end -}}

{{/*
Fails when a reserved name is passed in. This should be used to test against
Consul namespaces and partition names.
This template accepts an array that contains two elements. The first element
is the name that's being checked and the second is the name of the values.yaml
key that's setting the name.

Usage: {{ template "consul.reservedNamesFailer" (list .Values.key "key") }}

*/}}
{{- define "consul.reservedNamesFailer" -}}
{{- $name := index . 0 -}}
{{- $key := index . 1 -}}
{{- if or (eq "system" $name) (eq "universal" $name) (eq "operator" $name) (eq "root" $name) }}
{{- fail (cat "The name" $name "set for key" $key "is reserved by Consul for future use." ) }}
{{- end }}
{{- end -}}
