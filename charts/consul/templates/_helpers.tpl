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


{{- define "consul.restrictedSecurityContext" -}}
{{- if not .Values.global.enablePodSecurityPolicies -}}
{{/*
To be compatible with the 'restricted' Pod Security Standards profile, we
should set this securityContext on containers whenever possible.

In OpenShift < 4.11 the restricted SCC disallows setting most of these fields,
so we do not set any for simplicity (and because that's how it was configured
prior to adding restricted PSA support here). In OpenShift >= 4.11, the new
restricted-v2 SCC allows setting these in the securityContext, and by setting
them we avoid PSA warnings that are enabled by default.

We use the K8s version as a proxy for the OpenShift version because there is a
1:1 mapping of versions. OpenShift 4.11 corresponds to K8s 1.24.x.
*/}}
{{- if (or (not .Values.global.openshift.enabled) (and (ge .Capabilities.KubeVersion.Major "1") (ge .Capabilities.KubeVersion.Minor "24"))) -}}
securityContext:
  allowPrivilegeEscalation: false
  capabilities:
    drop:
    - ALL
    add:
    - NET_BIND_SERVICE
  runAsNonRoot: true
  seccompProfile:
    type: RuntimeDefault
{{- end -}}
{{- if not .Values.global.openshift.enabled -}}
{{/*
We must set runAsUser or else the root user will be used in some cases and
containers will fail to start due to runAsNonRoot above (e.g.
tls-init-cleanup). On OpenShift, runAsUser is set automatically. We pick user 100
because it is a non-root user id that exists in the consul, consul-dataplane,
and consul-k8s-control-plane images.
*/}}
  runAsUser: 100
{{- end -}}
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
              cp /consul/tmp/extra-config/extra-from-values.json /consul/extra-config/extra-from-values.json
              [ -n "${HOST_IP}" ] && sed -Ei "s|HOST_IP|${HOST_IP?}|g" /consul/extra-config/extra-from-values.json
              [ -n "${POD_IP}" ] && sed -Ei "s|POD_IP|${POD_IP?}|g" /consul/extra-config/extra-from-values.json
              [ -n "${HOSTNAME}" ] && sed -Ei "s|HOSTNAME|${HOSTNAME?}|g" /consul/extra-config/extra-from-values.json
{{- end -}}

{{/*
Cleanup server.extraConfig entries to avoid conflicting entries:
    - server.enableAgentDebug:
      - `enable_debug` should not exist in extraConfig
    - metrics.disableAgentHostName:
      - if global.metrics.enabled and global.metrics.enableAgentMetrics are enabled, `disable_hostname` should not exist in extraConfig
    - metrics.enableHostMetrics:
      - if global.metrics.enabled and global.metrics.enableAgentMetrics are enabled, `enable_host_metrics` should not exist in extraConfig
    - metrics.prefixFilter
      - if global.metrics.enabled and global.metrics.enableAgentMetrics are enabled, `prefix_filter` should not exist in extraConfig
    - metrics.datadog.enabled:
      - if global.metrics.datadog.enabled and global.metrics.datadog.dogstatsd.enabled, `dogstatsd_tags` and `dogstatsd_addr` should not exist in extraConfig

Usage: {{ template "consul.validateExtraConfig" . }}
*/}}
{{- define "consul.validateExtraConfig" -}}
{{- if (contains "enable_debug" .Values.server.extraConfig) }}{{ fail "The enable_debug key is present in extra-from-values.json. Use server.enableAgentDebug to set this value." }}{{- end }}
{{- if (contains "disable_hostname" .Values.server.extraConfig) }}{{ fail "The disable_hostname key is present in extra-from-values.json. Use global.metrics.disableAgentHostName to set this value." }}{{- end }}
{{- if (contains "enable_host_metrics" .Values.server.extraConfig) }}{{ fail "The enable_host_metrics key is present in extra-from-values.json. Use global.metrics.enableHostMetrics to set this value." }}{{- end }}
{{- if (contains "prefix_filter" .Values.server.extraConfig) }}{{ fail "The prefix_filter key is present in extra-from-values.json. Use global.metrics.prefix_filter to set this value." }}{{- end }}
{{- if (and .Values.global.metrics.enabled .Values.global.metrics.enableAgentMetrics) }}{{- if (and .Values.global.metrics.datadog.dogstatsd.enabled) }}{{- if (contains "dogstatsd_tags" .Values.server.extraConfig) }}{{ fail "The dogstatsd_tags key is present in extra-from-values.json. Use global.metrics.datadog.dogstatsd.dogstatsdTags to set this value." }}{{- end }}{{- end }}{{- if (and .Values.global.metrics.datadog.dogstatsd.enabled) }}{{- if (contains "dogstatsd_addr" .Values.server.extraConfig) }}{{ fail "The dogstatsd_addr key is present in extra-from-values.json. Use global.metrics.datadog.dogstatsd.dogstatsd_addr to set this value." }}{{- end }}{{- end }}{{- end }}
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


{{/*
Fails if telemetryCollector.clientId or telemetryCollector.clientSecret exist and one of other secrets is nil or empty.
- telemetryCollector.cloud.clientId.secretName
- telemetryCollector.cloud.clientSecret.secretName
- global.cloud.resourceId.secretName

Usage: {{ template "consul.validateTelemetryCollectorCloud" . }}

*/}}
{{- define "consul.validateTelemetryCollectorCloud" -}}
{{- if (and .Values.telemetryCollector.cloud.clientId.secretName (and (not .Values.global.cloud.clientSecret.secretName) (not .Values.telemetryCollector.cloud.clientSecret.secretName))) }}
{{fail "When telemetryCollector.cloud.clientId.secretName is set, telemetryCollector.cloud.clientSecret.secretName must also be set."}}
{{- end }}
{{- if (and .Values.telemetryCollector.cloud.clientSecret.secretName (and (not .Values.global.cloud.clientId.secretName) (not .Values.telemetryCollector.cloud.clientId.secretName))) }}
{{fail "When telemetryCollector.cloud.clientSecret.secretName is set, telemetryCollector.cloud.clientId.secretName must also be set."}}
{{- end }}
{{- end }}

{{/**/}}

{{- define "consul.validateTelemetryCollectorCloudSecretKeys" -}}
{{- if or (and .Values.telemetryCollector.cloud.clientId.secretName (not .Values.telemetryCollector.cloud.clientId.secretKey)) (and .Values.telemetryCollector.cloud.clientId.secretKey (not .Values.telemetryCollector.cloud.clientId.secretName)) }}
{{fail "When either telemetryCollector.cloud.clientId.secretName or telemetryCollector.cloud.clientId.secretKey is defined, both must be set."}}
{{- end }}
{{- if or (and .Values.telemetryCollector.cloud.clientSecret.secretName (not .Values.telemetryCollector.cloud.clientSecret.secretKey)) (and .Values.telemetryCollector.cloud.clientSecret.secretKey (not .Values.telemetryCollector.cloud.clientSecret.secretName)) }}
{{fail "When either telemetryCollector.cloud.clientSecret.secretName or telemetryCollector.cloud.clientSecret.secretKey is defined, both must be set."}}
{{- end }}
{{- if or (and .Values.telemetryCollector.cloud.clientSecret.secretName .Values.telemetryCollector.cloud.clientSecret.secretKey .Values.telemetryCollector.cloud.clientId.secretName .Values.telemetryCollector.cloud.clientId.secretKey (not (or .Values.telemetryCollector.cloud.resourceId.secretName .Values.global.cloud.resourceId.secretName))) }}
{{fail "When telemetryCollector has clientId and clientSecret, telemetryCollector.cloud.resourceId.secretName or global.cloud.resourceId.secretName must be set"}}
{{- end }}
{{- if or (and .Values.telemetryCollector.cloud.clientSecret.secretName .Values.telemetryCollector.cloud.clientSecret.secretKey .Values.telemetryCollector.cloud.clientId.secretName .Values.telemetryCollector.cloud.clientId.secretKey (not (or .Values.telemetryCollector.cloud.resourceId.secretKey .Values.global.cloud.resourceId.secretKey))) }}
{{fail "When telemetryCollector has clientId and clientSecret, telemetryCollector.cloud.resourceId.secretKey or global.cloud.resourceId.secretKey must be set"}}
{{- end }}
{{- end -}}

{{/*
Fails if telemetryCollector.cloud.resourceId is set but differs from global.cloud.resourceId. This should never happen. Either one or both are set, but they should never differ.
If they differ, that implies we're configuring servers for one HCP Consul cluster but pushing envoy metrics for a different HCP Consul cluster. A user could set the same value
in two secrets (it's questionable whether resourceId should be a secret at all) but we won't know at this point, so we just check secret name+key.

Usage: {{ template "consul.validateTelemetryCollectorResourceId" . }}

*/}}
{{- define "consul.validateTelemetryCollectorResourceId" -}}
{{- if and (and .Values.telemetryCollector.cloud.resourceId.secretName .Values.global.cloud.resourceId.secretName) (not (eq .Values.telemetryCollector.cloud.resourceId.secretName .Values.global.cloud.resourceId.secretName)) }}
{{fail "When both global.cloud.resourceId.secretName and telemetryCollector.cloud.resourceId.secretName are set, they should be the same."}}
{{- end }}
{{- if and (and .Values.telemetryCollector.cloud.resourceId.secretKey .Values.global.cloud.resourceId.secretKey) (not (eq .Values.telemetryCollector.cloud.resourceId.secretKey .Values.global.cloud.resourceId.secretKey)) }}
{{fail "When both global.cloud.resourceId.secretKey and telemetryCollector.cloud.resourceId.secretKey are set, they should be the same."}}
{{- end }}
{{- end }}

{{/**/}}

{{/*
Validation for Consul Metrics configuration:

Fail if metrics.enabled=true and metrics.disableAgentHostName=true, but metrics.enableAgentMetrics=false
    - metrics.enabled = true
    - metrics.enableAgentMetrics = false
    - metrics.disableAgentHostName = true

Fail if metrics.enableAgentMetrics=true and metrics.disableAgentHostName=true, but metrics.enabled=false
    - metrics.enabled = false
    - metrics.enableAgentMetrics = true
    - metrics.disableAgentHostName = true

Fail if metrics.enabled=true and metrics.enableHostMetrics=true, but metrics.enableAgentMetrics=false
    - metrics.enabled = true
    - metrics.enableAgentMetrics = false
    - metrics.enableHostMetrics = true

Fail if metrics.enableAgentMetrics=true and metrics.enableHostMetrics=true, but metrics.enabled=false
    - metrics.enabled = false
    - metrics.enableAgentMetrics = true
    - metrics.enableHostMetrics = true

Usage: {{ template "consul.validateMetricsConfig" . }}

*/}}

{{- define "consul.validateMetricsConfig" -}}
{{- if and (not .Values.global.metrics.enableAgentMetrics) (and .Values.global.metrics.disableAgentHostName .Values.global.metrics.enabled )}}
{{fail "When enabling metrics (global.metrics.enabled) and disabling hostname emission from metrics (global.metrics.disableAgentHostName), global.metrics.enableAgentMetrics must be set to true"}}
{{- end }}
{{- if and (not .Values.global.metrics) (and .Values.global.metrics.disableAgentHostName .Values.global.metrics.enableAgentMetrics )}}
{{fail "When enabling Consul agent metrics (global.metrics.enableAgentMetrics) and disabling hostname emission from metrics (global.metrics.disableAgentHostName), global metrics enablement (global.metrics.enabled) must be set to true"}}
{{- end }}
{{- if and (not .Values.global.metrics.enableAgentMetrics) (and .Values.global.metrics.disableAgentHostName .Values.global.metrics.enabled )}}
{{fail "When disabling hostname emission from metrics (global.metrics.disableAgentHostName) and enabling global metrics (global.metrics.enabled), Consul agent metrics must be enabled(global.metrics.enableAgentMetrics=true)"}}
{{- end }}
{{- if and (not .Values.global.metrics.enabled) (and .Values.global.metrics.disableAgentHostName .Values.global.metrics.enableAgentMetrics)}}
{{fail "When enabling Consul agent metrics (global.metrics.enableAgentMetrics) and disabling hostname metrics emission (global.metrics.disableAgentHostName), global metrics must be enabled (global.metrics.enabled)."}}
{{- end }}
{{- end -}}

{{/*
Validation for Consul Datadog Integration deployment:

Fail if Datadog integration enabled and Consul server agent telemetry is not enabled.
    - global.metrics.datadog.enabled=true
    - global.metrics.enableAgentMetrics=false || global.metrics.enabled=false

Fail if Consul OpenMetrics (Prometheus) and DogStatsD metrics are both enabled and configured.
    - global.metrics.datadog.dogstatsd.enabled (scrapes `/v1/agent/metrics?format=prometheus` via the `use_prometheus_endpoint` option)
    - global.metrics.datadog.openMetricsPrometheus.enabled (scrapes `/v1/agent/metrics?format=prometheus`)
    - see https://docs.datadoghq.com/integrations/consul/?tab=host#host for recommendation to not have both

Fail if Datadog OTLP forwarding is enabled and Consul Telemetry Collection is not enabled.
    - global.metrics.datadog.otlp.enabled=true
    - telemetryCollector.enabled=false

Fail if Consul Open Telemetry collector forwarding protocol is not one of either "http" or "grpc"
    - global.metrics.datadog.otlp.protocol!="http" || global.metrics.datadog.otlp.protocol!="grpc"

Usage: {{ template "consul.validateDatadogConfiguration" . }}

*/}}

{{- define "consul.validateDatadogConfiguration" -}}
{{- if and .Values.global.metrics.datadog.enabled (or (not .Values.global.metrics.enableAgentMetrics) (not .Values.global.metrics.enabled) )}}
{{fail "When enabling datadog metrics collection, the /v1/agent/metrics is required to be accessible, therefore global.metrics.enableAgentMetrics and global.metrics.enabled must be also be enabled."}}
{{- end }}
{{- if and .Values.global.metrics.datadog.dogstatsd.enabled .Values.global.metrics.datadog.openMetricsPrometheus.enabled }}
{{fail "You must have one of DogStatsD (global.metrics.datadog.dogstatsd.enabled) or OpenMetrics (global.metrics.datadog.openMetricsPrometheus.enabled) enabled, not both as this is an unsupported configuration." }}
{{- end }}
{{- if and .Values.global.metrics.datadog.otlp.enabled (not .Values.telemetryCollector.enabled) }}
{{fail "Cannot enable Datadog OTLP metrics collection (global.metrics.datadog.otlp.enabled) without consul-telemetry-collector. Ensure Consul OTLP collection is enabled (telemetryCollector.enabled) and configured." }}
{{- end }}
{{- if and (ne ( lower .Values.global.metrics.datadog.otlp.protocol) "http") (ne ( lower .Values.global.metrics.datadog.otlp.protocol) "grpc") }}
{{fail "Valid values for global.metrics.datadog.otlp.protocol must be one of either \"http\" or \"grpc\"." }}
{{- end }}
{{- end -}}

{{/*
Sets the dogstatsd_addr field of the agent configuration dependent on the
socket transport type being used:
  - "UDS" (Unix Domain Socket): prefixes "unix://" to URL and appends path to socket (i.e., unix:///var/run/datadog/dsd.socket)
  - "UDP" (User Datagram Protocol): adds no prefix and appends dogstatsd port number to hostname/IP (i.e., 172.20.180.10:8125)
- global.metrics.enableDatadogIntegration.dogstatsd configuration

Usage: {{ template "consul.dogstatsdAaddressInfo" . }}
*/}}

{{- define "consul.dogstatsdAaddressInfo" -}}
{{- if (and .Values.global.metrics.datadog.enabled .Values.global.metrics.datadog.dogstatsd.enabled) }}
        "dogstatsd_addr": "{{- if eq .Values.global.metrics.datadog.dogstatsd.socketTransportType "UDS" }}unix://{{ .Values.global.metrics.datadog.dogstatsd.dogstatsdAddr }}{{- else }}{{ .Values.global.metrics.datadog.dogstatsd.dogstatsdAddr | trimAll "\"" }}{{- if ne ( .Values.global.metrics.datadog.dogstatsd.dogstatsdPort | int ) 0 }}:{{ .Values.global.metrics.datadog.dogstatsd.dogstatsdPort | toString }}{{- end }}{{- end }}",{{- end }}
{{- end -}}

{{/*
Configures the metrics prefixing that's required to either allow or dissallow certaing RPC or gRPC server calls:

Usage: {{ template "consul.prefixFilter" . }}
*/}}
{{- define "consul.prefixFilter" -}}
{{- $allowList := .Values.global.metrics.prefixFilter.allowList }}
{{- $blockList := .Values.global.metrics.prefixFilter.blockList }}
{{- if and (not (empty $allowList)) (not (empty $blockList)) }}
        "prefix_filter": [{{- range $index, $value := concat $allowList $blockList -}}
    "{{- if (has $value $allowList) }}{{ printf "+%s" ($value | trimAll "\"") }}{{- else }}{{ printf "-%s" ($value | trimAll "\"") }}{{- end }}"{{- if lt $index (sub (len (concat $allowList $blockList)) 1) -}},{{- end -}}
  {{- end -}}],
{{- else if not (empty $allowList) }}
        "prefix_filter": [{{- range $index, $value := $allowList -}}
    "{{ printf "+%s" ($value | trimAll "\"") }}"{{- if lt $index (sub (len $allowList) 1) -}},{{- end -}}
  {{- end -}}],
{{- else if not (empty $blockList) }}
        "prefix_filter": [{{- range $index, $value := $blockList -}}
    "{{ printf "-%s" ($value | trimAll "\"") }}"{{- if lt $index (sub (len $blockList) 1) -}},{{- end -}}
  {{- end -}}],
{{- end }}
{{- end -}}

{{/*
Retrieves the global consul/consul-enterprise version string for use with labels or tags.
Requirements for valid labels:
 -  a valid label must be an empty string or consist of
    =>  alphanumeric characters
    =>  '-', '_' or '.'
    =>  must start and end with an alphanumeric character
        (e.g. 'MyValue',  or 'my_value',  or '12345', regex used for validation is
        '(([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9])?')

Usage: {{ template "consul.versionInfo" }}
*/}}
{{- define "consul.versionInfo" -}}
{{- $imageVersion := regexSplit ":" .Values.global.image -1 }}
{{- $versionInfo := printf "%s" (index $imageVersion 1 ) | trimSuffix "\"" }}
{{- $sanitizedVersion := "" }}
{{- $pattern := "^([A-Za-z0-9][-A-Za-z0-9_.]*[A-Za-z0-9])?$" }}
{{- if not (regexMatch $pattern $versionInfo) -}}
    {{- $sanitizedVersion = regexReplaceAll "[^A-Za-z0-9-_.]|sha256" $versionInfo "" }}
    {{- $sanitizedVersion = printf "%s" (trimSuffix "-" (trimPrefix "-" $sanitizedVersion)) -}}
{{- else }}
    {{- $sanitizedVersion = $versionInfo }}
{{- end -}}
{{- printf "%s" $sanitizedVersion | quote }}
{{- end -}}