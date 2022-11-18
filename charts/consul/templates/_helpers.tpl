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

{{- define "consul.vaultCATemplate" -}}
 |
            {{ "{{" }}- with secret "{{ .secretName }}" -{{ "}}" }}
            {{ "{{" }}- .Data.certificate -{{ "}}" }}
            {{ "{{" }}- end -{{ "}}" }}
{{- end -}}

{{- define "consul.serverTLSCATemplate" -}}
{{ include "consul.vaultCATemplate" .Values.global.tls.caCert }}
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

{{- define "consul.connectInjectWebhookTLSCertTemplate" -}}
 |
            {{ "{{" }}- with secret "{{ .Values.global.secretsBackend.vault.connectInject.tlsCert.secretName }}" "{{- $name := include "consul.fullname" . -}}{{ printf "common_name=%s-connect-injector" $name }}"
            "alt_names={{ include "consul.connectInjectorTLSAltNames" . }}" -{{ "}}" }}
            {{ "{{" }}- .Data.certificate -{{ "}}" }}
            {{ "{{" }}- end -{{ "}}" }}
{{- end -}}

{{- define "consul.connectInjectWebhookTLSKeyTemplate" -}}
 |
            {{ "{{" }}- with secret "{{ .Values.global.secretsBackend.vault.connectInject.tlsCert.secretName }}" "{{- $name := include "consul.fullname" . -}}{{ printf "common_name=%s-connect-injector" $name }}"
            "alt_names={{ include "consul.connectInjectorTLSAltNames" . }}" -{{ "}}" }}
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
{{- if .Values.global.tls -}}{{- if .Values.global.tls.serverAdditionalIPSANs -}}{{- range $san := .Values.global.tls.serverAdditionalIPSANs }},{{ $san }} {{- end -}}{{- end -}}{{- end -}}
{{- end -}}

{{- define "consul.connectInjectorTLSAltNames" -}}
{{- $name := include "consul.fullname" . -}}
{{- $ns := .Release.Namespace -}}
{{ printf "%s-connect-injector,%s-connect-injector.%s,%s-connect-injector.%s.svc,%s-connect-injector.%s.svc.cluster.local" $name $name $ns $name $ns $name $ns}}
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
              cp /consul/config/extra-from-values.json /consul/extra-config/extra-from-values.json
              [ -n "${HOST_IP}" ] && sed -Ei "s|HOST_IP|${HOST_IP?}|g" /consul/extra-config/extra-from-values.json
              [ -n "${POD_IP}" ] && sed -Ei "s|POD_IP|${POD_IP?}|g" /consul/extra-config/extra-from-values.json
              [ -n "${HOSTNAME}" ] && sed -Ei "s|HOSTNAME|${HOSTNAME?}|g" /consul/extra-config/extra-from-values.json
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

{{- define "consul.pdb.connectInject.maxUnavailable" -}}
{{- if eq (int .Values.connectInject.replicas) 1 -}}
{{ 0 }}
{{- else if .Values.connectInject.disruptionBudget.maxUnavailable -}}
{{ .Values.connectInject.disruptionBudget.maxUnavailable -}}
{{- else -}}
{{- if eq (int .Values.connectInject.replicas) 3 -}}
{{- 1 -}}
{{- else -}}
{{- sub (div (int .Values.connectInject.replicas) 2) 1 -}}
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
        {{- if .Values.global.cloud.enabled }}
        -tls-server-name=server.{{.Values.global.datacenter}}.{{.Values.global.domain}} \
        {{- end}}
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

{{/*
Fails when at least one but not all of the following have been set:
- global.secretsBackend.vault.connectInjectRole
- global.secretsBackend.vault.connectInject.tlsCert.secretName
- global.secretsBackend.vault.connectInject.caCert.secretName

The above values are needed in full to turn off web cert manager and allow
connect inject to manage its own webhook certs.

Usage: {{ template "consul.validateVaultWebhookCertConfiguration" . }}

*/}}
{{- define "consul.validateVaultWebhookCertConfiguration" -}}
{{- if or .Values.global.secretsBackend.vault.connectInjectRole .Values.global.secretsBackend.vault.connectInject.tlsCert.secretName .Values.global.secretsBackend.vault.connectInject.caCert.secretName}}
{{- if or (not .Values.global.secretsBackend.vault.connectInjectRole) (not .Values.global.secretsBackend.vault.connectInject.tlsCert.secretName) (not .Values.global.secretsBackend.vault.connectInject.caCert.secretName) }}
{{fail "When one of the following has been set, all must be set:  global.secretsBackend.vault.connectInjectRole, global.secretsBackend.vault.connectInject.tlsCert.secretName, global.secretsBackend.vault.connectInject.caCert.secretName"}}
{{ end }}
{{ end }}
{{- end -}}

{{/*
Consul server environment variables for consul-k8s commands.
*/}}
{{- define "consul.consulK8sConsulServerEnvVars" -}}
- name: CONSUL_ADDRESSES
  {{- if .Values.externalServers.enabled }}
  value: {{ .Values.externalServers.hosts | first }}
  {{- else }}
  value: {{ template "consul.fullname" . }}-server.{{ .Release.Namespace }}.svc
  {{- end }}
- name: CONSUL_GRPC_PORT
  {{- if .Values.externalServers.enabled }}
  value: "{{ .Values.externalServers.grpcPort }}"
  {{- else }}
  value: "8502"
  {{- end }}
- name: CONSUL_HTTP_PORT
  {{- if .Values.externalServers.enabled }}
  value: "{{ .Values.externalServers.httpsPort }}"
  {{- else if .Values.global.tls.enabled }}
  value: "8501"
  {{- else }}
  value: "8500"
  {{- end }}
- name: CONSUL_DATACENTER
  value: {{ .Values.global.datacenter }}
- name: CONSUL_API_TIMEOUT
  value: {{ .Values.global.consulAPITimeout }}
{{- if .Values.global.adminPartitions.enabled }}
- name: CONSUL_PARTITION
  value: {{ .Values.global.adminPartitions.name }}
{{- if .Values.global.acls.manageSystemACLs }}
- name: CONSUL_LOGIN_PARTITION
  value: {{ .Values.global.adminPartitions.name }}
{{- end }}
{{- end }}
{{- if .Values.global.tls.enabled }}
- name: CONSUL_USE_TLS
  value: "true"
{{- if (not (and .Values.externalServers.enabled .Values.externalServers.useSystemRoots)) }}
- name: CONSUL_CACERT_FILE
  {{- if .Values.global.secretsBackend.vault.enabled }}
  value: "/vault/secrets/serverca.crt"
  {{- else }}
  value: "/consul/tls/ca/tls.crt"
  {{- end }}
{{- end }}
{{- if and .Values.externalServers.enabled .Values.externalServers.tlsServerName }}
- name: CONSUL_TLS_SERVER_NAME
  value: {{ .Values.externalServers.tlsServerName }}
{{- else if .Values.global.cloud.enabled }}
- name: CONSUL_TLS_SERVER_NAME
  value: server.{{ .Values.global.datacenter}}.{{ .Values.global.domain}}
{{- end }}
{{- end }}
{{- if and .Values.externalServers.enabled .Values.externalServers.skipServerWatch }}
- name: CONSUL_SKIP_SERVER_WATCH
  value: "true" 
{{- end }}
{{- end -}}

{{/*
Fails global.cloud.enabled is true and one of the following secrets is nil or empty.
- global.cloud.resourceId.secretName
- global.cloud.clientId.secretName
- global.cloud.clientSecret.secretName

Usage: {{ template "consul.validateRequiredCloudSecretsExist" . }}

*/}}
{{- define "consul.validateRequiredCloudSecretsExist" -}}
{{- if (and .Values.global.cloud.enabled (or (not .Values.global.cloud.resourceId.secretName) (not .Values.global.cloud.clientId.secretName) (not .Values.global.cloud.clientSecret.secretName))) }}
{{fail "When global.cloud.enabled is true, global.cloud.resourceId.secretName, global.cloud.clientId.secretName, and global.cloud.clientSecret.secretName must also be set."}}
{{- end }}
{{- end -}}

{{/*
Fails global.cloud.enabled is true and one of the following secrets has either an empty secretName or secretKey.
- global.cloud.resourceId.secretName / secretKey
- global.cloud.clientId.secretName / secretKey
- global.cloud.clientSecret.secretName / secretKey
- global.cloud.authUrl.secretName / secretKey
- global.cloud.apiHost.secretName / secretKey
- global.cloud.scadaAddress.secretName / secretKey
Usage: {{ template "consul.validateCloudSecretKeys" . }}

*/}}
{{- define "consul.validateCloudSecretKeys" -}}
{{- if and .Values.global.cloud.enabled }} 
{{- if or (and .Values.global.cloud.resourceId.secretName (not .Values.global.cloud.resourceId.secretKey)) (and .Values.global.cloud.resourceId.secretKey (not .Values.global.cloud.resourceId.secretName)) }}
{{fail "When either global.cloud.resourceId.secretName or global.cloud.resourceId.secretKey is defined, both must be set."}}
{{- end }}
{{- if or (and .Values.global.cloud.clientId.secretName (not .Values.global.cloud.clientId.secretKey)) (and .Values.global.cloud.clientId.secretKey (not .Values.global.cloud.clientId.secretName)) }}
{{fail "When either global.cloud.clientId.secretName or global.cloud.clientId.secretKey is defined, both must be set."}}
{{- end }}
{{- if or (and .Values.global.cloud.clientSecret.secretName (not .Values.global.cloud.clientSecret.secretKey)) (and .Values.global.cloud.clientSecret.secretKey (not .Values.global.cloud.clientSecret.secretName)) }}
{{fail "When either global.cloud.clientSecret.secretName or global.cloud.clientSecret.secretKey is defined, both must be set."}}
{{- end }}
{{- if or (and .Values.global.cloud.authUrl.secretName (not .Values.global.cloud.authUrl.secretKey)) (and .Values.global.cloud.authUrl.secretKey (not .Values.global.cloud.authUrl.secretName)) }}
{{fail "When either global.cloud.authUrl.secretName or global.cloud.authUrl.secretKey is defined, both must be set."}}
{{- end }}
{{- if or (and .Values.global.cloud.apiHost.secretName (not .Values.global.cloud.apiHost.secretKey)) (and .Values.global.cloud.apiHost.secretKey (not .Values.global.cloud.apiHost.secretName)) }}
{{fail "When either global.cloud.apiHost.secretName or global.cloud.apiHost.secretKey is defined, both must be set."}}
{{- end }}
{{- if or (and .Values.global.cloud.scadaAddress.secretName (not .Values.global.cloud.scadaAddress.secretKey)) (and .Values.global.cloud.scadaAddress.secretKey (not .Values.global.cloud.scadaAddress.secretName)) }}
{{fail "When either global.cloud.scadaAddress.secretName or global.cloud.scadaAddress.secretKey is defined, both must be set."}}
{{- end }}
{{- end }}
{{- end -}}
