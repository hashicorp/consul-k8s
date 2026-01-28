#!/usr/bin/env bats

load _helpers

@test "server/ConfigMap: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ConfigMap: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ConfigMap: disable with server.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.enabled=false' \
      .
}

@test "server/ConfigMap: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.enabled=false' \
      .
}

#--------------------------------------------------------------------
# retry-join

@test "server/ConfigMap: retry join gets populated" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.replicas=3' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .retry_join[0] | tee /dev/stderr)

  [ "${actual}" = "release-name-consul-server.default.svc:8301" ]
}

#--------------------------------------------------------------------
# grpc

@test "server/ConfigMap: if tls is disabled, grpc port is set and grpc_tls port is disabled" {
  cd `chart_dir`
  local configmap=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | tee /dev/stderr)

  local actual=$(echo $configmap | jq -r .ports.grpc |  tee /dev/stderr)
  [ "${actual}" = "8502" ]
  local actual=$(echo $configmap | jq -r .ports.grpc_tls |  tee /dev/stderr)
  [ "${actual}" = "-1" ]
}

@test "server/ConfigMap: if tls is enabled, grpc_tls port is set and grpc port is disabled" {
  cd `chart_dir`
  local configmap=$(helm template \
      --set 'global.tls.enabled=true' \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | tee /dev/stderr)

  local actual=$(echo $configmap | jq -r .ports.grpc_tls |  tee /dev/stderr)
  [ "${actual}" = "8502" ]
  local actual=$(echo $configmap | jq -r .ports.grpc |  tee /dev/stderr)
  [ "${actual}" = "-1" ]
}

#--------------------------------------------------------------------
# serflan

@test "server/ConfigMap: server.ports.serflan.port is set to 8301 by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .ports.serf_lan | tee /dev/stderr)

  [ "${actual}" = "8301" ]
}

@test "server/ConfigMap: server.ports.serflan.port can be customized" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.ports.serflan.port=9301' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .ports.serf_lan | tee /dev/stderr)

  [ "${actual}" = "9301" ]
}

@test "server/ConfigMap: retry join uses server.ports.serflan.port" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.replicas=3' \
      --set 'server.ports.serflan.port=9301' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .retry_join[0] | tee /dev/stderr)

  [ "${actual}" = "release-name-consul-server.default.svc:9301" ]
}

@test "server/ConfigMap: recursors can be set by global.recursors" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.recursors[0]=1.1.1.1' \
      --set 'global.recursors[1]=2.2.2.2' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -c .recursors | tee /dev/stderr)
  [ "${actual}" = '["1.1.1.1","2.2.2.2"]' ]
}

#--------------------------------------------------------------------
# bootstrap_expect

@test "server/ConfigMap: bootstrap_expect defaults to replicas" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq .bootstrap_expect | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

@test "server/ConfigMap: bootstrap_expect can be set by server.bootstrapExpect" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.bootstrapExpect=5' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq .bootstrap_expect | tee /dev/stderr)
  [ "${actual}" = "5" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "server/ConfigMap: creates acl config with .global.acls.manageSystemACLs enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq '.data["acl-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# ui.enabled

@test "server/ConfigMap: creates ui config with .ui.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'ui.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"]' | jq -c .ui_config | tee /dev/stderr)
  [ "${actual}" = '{"enabled":true}' ]
}

@test "server/ConfigMap: does not create ui config with .ui.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'ui.enabled=false' \
      . | tee /dev/stderr |
      yq '.data["ui-config.json"] | length == 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ConfigMap: adds metrics ui config with .global.metrics.enabled=true and ui.metrics.enabled=-" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true' \
      --set 'ui.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"]' | jq -c .ui_config | tee /dev/stderr)
  [ "${actual}" = '{"metrics_provider":"prometheus","metrics_proxy":{"base_url":"http://prometheus-server"},"enabled":true}' ]
}

@test "server/ConfigMap: adds metrics ui config with .global.metrics.enabled=false and .ui.metrics.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'ui.metrics.enabled=true' \
      --set 'ui.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"]' | jq -c .ui_config | tee /dev/stderr)
  [ "${actual}" = '{"metrics_provider":"prometheus","metrics_proxy":{"base_url":"http://prometheus-server"},"enabled":true}' ]
}

@test "server/ConfigMap: adds metrics ui config with .global.metrics.enabled=true and .ui.metrics.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true' \
      --set 'ui.metrics.enabled=true' \
      --set 'ui.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"]' | jq -c .ui_config | tee /dev/stderr)
  [ "${actual}" = '{"metrics_provider":"prometheus","metrics_proxy":{"base_url":"http://prometheus-server"},"enabled":true}' ]
}

@test "server/ConfigMap: doesn't add metrics ui config with .global.metrics.enabled=true and .ui.metrics.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true' \
      --set 'ui.metrics.enabled=false' \
      --set 'ui.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"]' | jq -c .ui_config | tee /dev/stderr)
  [ "${actual}" = '{"enabled":true}' ]
}

@test "server/ConfigMap: doesn't add metrics ui config with .global.metrics.enabled=false and .ui.metrics.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=false' \
      --set 'ui.metrics.enabled=false' \
      --set 'ui.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"]' | jq -c .ui_config | tee /dev/stderr)
  [ "${actual}" = '{"enabled":true}' ]
}

@test "server/ConfigMap: updates ui config with .ui.metrics.provider" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'ui.enabled=true' \
      --set 'ui.metrics.enabled=true' \
      --set 'ui.metrics.provider=other' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"]' | yq -r '.ui_config.metrics_provider' | tee /dev/stderr)
  [ "${actual}" = "other" ]
}

@test "server/ConfigMap: updates ui config with .ui.metrics.baseURL" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'ui.enabled=true' \
      --set 'ui.metrics.enabled=true' \
      --set 'ui.metrics.baseURL=http://foo.bar' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"]' | yq -r '.ui_config.metrics_proxy.base_url' | tee /dev/stderr)
  [ "${actual}" = "http://foo.bar" ]
}

@test "server/ConfigMap: updates ui config with .ui.metrics.pathAllowlist" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'ui.enabled=true' \
      --set 'ui.metrics.enabled=true' \
      --set 'ui.metrics.pathAllowlist[0]=/consul/api/v1/query_range' \
      --set 'ui.metrics.pathAllowlist[1]=/consul/api/v1/query' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"]' | yq -r '.ui_config.metrics_proxy.path_allowlist' | tee /dev/stderr)
  [ "${actual}" = '["/consul/api/v1/query_range","/consul/api/v1/query"]' ]
}

#--------------------------------------------------------------------
# ui.dashboardURLTemplates.service

@test "server/ConfigMap: dashboard_url_templates not set by default" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"]' | jq .dashboard_url_templates | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "server/ConfigMap: ui.dashboardURLTemplates.service sets the template" {
  cd `chart_dir`

  local expected='-hcl='\''ui_config { dashboard_url_templates { service = \"http://localhost:3000/d/WkFEBmF7z/services?orgId=1&var-Service={{Service.Name}}\" } }'

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'ui.dashboardURLTemplates.service=http://localhost:3000/d/WkFEBmF7z/services?orgId=1&var-Service={{Service.Name}}' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"]' | jq -c .ui_config.dashboard_url_templates | tee /dev/stderr)

  [ "${actual}" = '{"service":"http://localhost:3000/d/WkFEBmF7z/services?orgId=1&var-Service={{Service.Name}}"}' ]
}

#--------------------------------------------------------------------
# connectInject.centralConfig [DEPRECATED]

@test "server/ConfigMap: centralConfig is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq '.data["central-config.json"] | contains("enable_central_service_config")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.acls.replicationToken

@test "server/ConfigMap: enable_token_replication is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.data["acl-config.json"]' | yq -r '.acl.enable_token_replication' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/ConfigMap: enable_token_replication is set when acls.replicationToken.secretKey and secretName are set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.replicationToken.secretName=name' \
      --set 'global.acls.replicationToken.secretKey=key' \
      . | tee /dev/stderr |
      yq -r '.data["acl-config.json"]' | yq -r '.acl.enable_token_replication' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault Connect CA

@test "server/ConfigMap: doesn't add connect CA config by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq '.data["additional-connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: doesn't add connect CA config when vault is enabled but vault address, root and int PKI paths are not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      . | tee /dev/stderr |
      yq '.data["additional-connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: doesn't add connect CA config when vault is enabled and vault address is set, but root and int PKI paths are not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      . | tee /dev/stderr |
      yq '.data["additional-connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: doesn't add connect CA config when vault is enabled and root pki path is set, but vault address and int PKI paths are not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      . | tee /dev/stderr |
      yq '.data["additional-connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: doesn't add connect CA config when vault is enabled and int path is set, but vault address and root PKI paths are not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      . | tee /dev/stderr |
      yq '.data["additional-connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: doesn't add connect CA config when vault is enabled and root and int paths are set, but vault address is not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      . | tee /dev/stderr |
      yq '.data["additional-connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: doesn't add connect CA config when vault is enabled and root path and address are set, but int path is not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      . | tee /dev/stderr |
      yq '.data["additional-connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: doesn't add connect CA config when vault is enabled and int path and address are set, but root path is not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.intPKIPath=int' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.intPKIPath=int' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      . | tee /dev/stderr |
      yq '.data["additional-connect-ca-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: adds connect CA config when vault is enabled and connect CA are configured" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"]' | tee /dev/stderr)
  [ "${actual}" = '"{\n  \"connect\": [\n    {\n      \"ca_config\": [\n        {\n          \"address\": \"example.com\",\n          \"intermediate_pki_path\": \"int\",\n          \"root_pki_path\": \"root\",\n          \"auth_method\": {\n            \"type\": \"kubernetes\",\n            \"mount_path\": \"kubernetes\",\n            \"params\": {\n              \"role\": \"foo\"\n            }\n          }\n        }\n      ],\n      \"ca_provider\": \"vault\"\n    }\n  ]\n}\n"' ]

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      . | tee /dev/stderr |
      yq '.data["additional-connect-ca-config.json"]' | tee /dev/stderr)
  [ "${actual}" = '"{}\n"' ]
}

@test "server/ConfigMap: can set additional connect CA config" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      --set 'global.secretsBackend.vault.connectCA.additionalConfig="{\"hello\": \"world\"}"' \
      . | tee /dev/stderr |
      yq '.data["additional-connect-ca-config.json"]' | tee /dev/stderr)
  [ "${actual}" = '"{\"hello\": \"world\"}\n"' ]
}

@test "server/ConfigMap: can set auth method mount path" {
  cd `chart_dir`

  local caConfig=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      --set 'global.secretsBackend.vault.connectCA.authMethodPath=kubernetes2' \
      . | tee /dev/stderr |
      yq -r '.data["connect-ca-config.json"]' | tee /dev/stderr)

  local actual=$(echo $caConfig |  jq -r .connect[0].ca_config[0].auth_method.mount_path)
  [ "${actual}" = "kubernetes2" ]
}

@test "server/ConfigMap: doesn't set Vault CA cert in connect CA config by default" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | contains("\"ca_file\": \"/consul/vault-ca/tls.crt\"")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: doesn't set Vault CA cert in connect CA config when vault CA secret name is set but secret key is not" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      --set 'global.secretsBackend.vault.ca.secretName=ca' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | contains("\"ca_file\": \"/consul/vault-ca/tls.crt\"")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: doesn't set Vault CA cert in connect CA config when vault CA secret key is set but secret name is not" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | contains("\"ca_file\": \"/consul/vault-ca/tls.crt\"")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: doesn't set Vault CA cert in connect CA config when both vault CA secret name and key are set" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      --set 'global.secretsBackend.vault.ca.secretName=ca' \
      --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | contains("\"ca_file\": \"/consul/vault-ca/tls.crt\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ConfigMap: doesn't set Vault Namespace in connect CA config when global.secretsBackend.vault.vaultNamespace is blank in values.yaml" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      --set 'global.secretsBackend.vault.ca.secretName=ca' \
      --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | contains("namespace")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: set Vault Namespace in connect CA config when global.secretsBackend.vault.vaultNamespace is not blank in values.yaml" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      --set 'global.secretsBackend.vault.ca.secretName=ca' \
      --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
      --set 'global.secretsBackend.vault.vaultNamespace=vault-namespace' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | contains("\"namespace\": \"vault-namespace\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}


@test "server/ConfigMap: do not set Vault Namespace in connect CA config from global.secretsBackend.vault.vaultNamespace when also set in connectCA.additionalConfig" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      --set 'global.secretsBackend.vault.ca.secretName=ca' \
      --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
      --set 'global.secretsBackend.vault.vaultNamespace=vault-namespace' \
      --set 'global.secretsBackend.vault.connectCA.additionalConfig=\{\"connect\":\[\{\"ca_config\":\[\{\"namespace\": \"vns\"}\]\}\]\}' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | contains("\"namespace\": \"vault-namespace\"")' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: set Vault Namespace in connect CA config when global.secretsBackend.vault.vaultNamespace is not blank and connectCA.additionalConfig is blank" {
  cd `chart_dir`

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.connectCA.address=example.com' \
      --set 'global.secretsBackend.vault.connectCA.rootPKIPath=root' \
      --set 'global.secretsBackend.vault.connectCA.intermediatePKIPath=int' \
      --set 'global.secretsBackend.vault.ca.secretName=ca' \
      --set 'global.secretsBackend.vault.ca.secretKey=tls.crt' \
      --set 'global.secretsBackend.vault.vaultNamespace=vault-namespace' \
      . | tee /dev/stderr |
      yq '.data["connect-ca-config.json"] | contains("\"namespace\": \"vault-namespace\"")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ConfigMap: doesn't add federation config when global.federation.enabled is false (default)" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq '.data["federation-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: adds default federation config when global.federation.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml \
      --set 'global.federation.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["federation-config.json"]' | jq -c . | tee /dev/stderr)
  [ "${actual}" = '{"primary_datacenter":"","primary_gateways":[],"connect":{"enable_mesh_gateway_wan_federation":true}}' ]
}

@test "server/ConfigMap: can set primary dc and gateways when global.federation.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml \
      --set 'global.federation.enabled=true' \
      --set 'global.federation.primaryDatacenter=dc1' \
      --set 'global.federation.primaryGateways[0]=1.1.1.1:443' \
      --set 'global.federation.primaryGateways[1]=2.2.2.2:443' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["federation-config.json"]' | jq -c . | tee /dev/stderr)
  [ "${actual}" = '{"primary_datacenter":"dc1","primary_gateways":["1.1.1.1:443","2.2.2.2:443"],"connect":{"enable_mesh_gateway_wan_federation":true}}' ]
}

#--------------------------------------------------------------------
# TLS

@test "server/ConfigMap: sets correct default configuration when global.tls.enabled" {
  cd `chart_dir`
  local config=$(helm template \
      -s templates/server-config-configmap.yaml \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["tls-config.json"]' | tee /dev/stderr)

  local actual
  actual=$(echo $config | jq -r .tls.defaults.ca_file | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]

  actual=$(echo $config | jq -r .tls.defaults.cert_file | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/server/tls.crt" ]

  actual=$(echo $config | jq -r .tls.defaults.key_file | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/server/tls.key" ]

  actual=$(echo $config | jq -r .tls.internal_rpc.verify_incoming | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $config | jq -r .tls.defaults.verify_outgoing | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $config | jq -r .tls.internal_rpc.verify_server_hostname | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $config | jq -c .ports | tee /dev/stderr)
  [ "${actual}" = '{"http":-1,"https":8501}' ]
}

@test "server/ConfigMap: sets correct default configuration when global.tls.enabled and global.peering.enabled" {
  cd `chart_dir`
  local config=$(helm template \
      -s templates/server-config-configmap.yaml \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.peering.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["tls-config.json"]' | tee /dev/stderr)

  local actual
  actual=$(echo $config | jq -r .tls.defaults.ca_file | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/ca/tls.crt" ]

  actual=$(echo $config | jq -r .tls.defaults.cert_file | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/server/tls.crt" ]

  actual=$(echo $config | jq -r .tls.defaults.key_file | tee /dev/stderr)
  [ "${actual}" = "/consul/tls/server/tls.key" ]

  actual=$(echo $config | jq -r .tls.internal_rpc.verify_incoming | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $config | jq -r .tls.defaults.verify_outgoing | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $config | jq -r .tls.internal_rpc.verify_server_hostname | tee /dev/stderr)
  [ "${actual}" = "true" ]

  actual=$(echo $config | jq -c .ports | tee /dev/stderr)
  [ "${actual}" = '{"http":-1,"https":8501}' ]
}

@test "server/ConfigMap: doesn't set verify_* configuration to true when global.tls.enabled and global.tls.verify is false" {
  cd `chart_dir`
  local config=$(helm template \
      -s templates/server-config-configmap.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.verify=false' \
      . | tee /dev/stderr |
      yq -r '.data["tls-config.json"]' | tee /dev/stderr)

  local actual
  actual=$(echo $config | jq -r .verify_incoming_rpc | tee /dev/stderr)
  [ "${actual}" = "null" ]

  actual=$(echo $config | jq -r .verify_outgoing | tee /dev/stderr)
  [ "${actual}" = "null" ]

  actual=$(echo $config | jq -r .verify_server_hostname | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/ConfigMap: doesn't set verify_* configuration to true when global.tls.enabled and global.peering.enabled and global.tls.verify is false" {
  cd `chart_dir`
  local config=$(helm template \
      -s templates/server-config-configmap.yaml \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'global.peering.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.tls.verify=false' \
      . | tee /dev/stderr |
      yq -r '.data["tls-config.json"]' | tee /dev/stderr)

  local actual
  actual=$(echo $config | jq -r .tls.internal_rpc | tee /dev/stderr)
  [ "${actual}" = "null" ]

  actual=$(echo $config | jq -r .tls.defaults.verify_outgoing | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/ConfigMap: HTTP port is not set in when httpsOnly is false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.httpsOnly=false' \
      . | tee /dev/stderr |
      yq -r '.data["tls-config.json"]' | jq -c .ports | tee /dev/stderr)
  [ "${actual}" = '{"https":8501}' ]
}

#--------------------------------------------------------------------
# global.tls.enableAutoEncrypt

@test "server/ConfigMap: enables auto-encrypt for the servers when global.tls.enableAutoEncrypt is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      . | tee /dev/stderr |
      yq -r '.data["tls-config.json"]' | jq -c .auto_encrypt | tee /dev/stderr)
  [ "${actual}" = '{"allow_tls":true}' ]
}

#--------------------------------------------------------------------
# TLS + Vault

@test "server/ConfigMap: sets TLS file paths to point to vault secrets when Vault is enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-config-configmap.yaml  \
    --set 'global.tls.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.datacenter=dc2' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
    --set 'server.serverCert.secretName=pki_int/issue/test' \
    . | tee /dev/stderr |
    yq -r '.data["tls-config.json"]' | tee /dev/stderr)

  local actual=$(echo $object | jq -r .tls.defaults.ca_file | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/serverca.crt" ]

  local actual=$(echo $object | jq -r .tls.defaults.cert_file | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/servercert.crt" ]

  local actual=$(echo $object | jq -r .tls.defaults.key_file | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/servercert.key" ]
}

@test "server/ConfigMap: sets TLS file paths to point to vault secrets when Vault is enabled and global.peering.enabled" {
  cd `chart_dir`
  local object=$(helm template \
    -s templates/server-config-configmap.yaml  \
    --set 'global.tls.enabled=true' \
    --set 'meshGateway.enabled=true' \
    --set 'global.peering.enabled=true' \
    --set 'connectInject.enabled=true' \
    --set 'global.tls.enableAutoEncrypt=true' \
    --set 'global.datacenter=dc2' \
    --set 'global.secretsBackend.vault.enabled=true' \
    --set 'global.secretsBackend.vault.consulClientRole=test' \
    --set 'global.secretsBackend.vault.consulServerRole=foo' \
    --set 'global.secretsBackend.vault.consulCARole=test' \
    --set 'global.tls.caCert.secretName=pki_int/cert/ca' \
    --set 'server.serverCert.secretName=pki_int/issue/test' \
    . | tee /dev/stderr |
    yq -r '.data["tls-config.json"]' | tee /dev/stderr)

  local actual=$(echo $object | jq -r .tls.defaults.ca_file | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/serverca.crt" ]

  local actual=$(echo $object | jq -r .tls.defaults.cert_file | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/servercert.crt" ]

  local actual=$(echo $object | jq -r .tls.defaults.key_file | tee /dev/stderr)
  [ "${actual}" = "/vault/secrets/servercert.key" ]
}

@test "server/ConfigMap: when global.metrics.enableAgentMetrics=true, sets telemetry config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.prometheus_retention_time | tee /dev/stderr)

  [ "${actual}" = "1m" ]
}

@test "server/ConfigMap: when global.metrics.enableAgentMetrics=true and global.metrics.agentMetricsRetentionTime is set, sets telemetry config with updated retention time" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.agentMetricsRetentionTime=5m'  \
      . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.prometheus_retention_time | tee /dev/stderr)

  [ "${actual}" = "5m" ]
}

#--------------------------------------------------------------------
# auto_reload_config

@test "server/ConfigMap: auto reload config is set to true when Vault is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.secretsBackend.vault.enabled=true'  \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .auto_reload_config | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "server/ConfigMap: auto reload config is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .auto_reload_config | tee /dev/stderr)

  [ "${actual}" = null ]
}

#--------------------------------------------------------------------
# peering

@test "server/ConfigMap: peering configuration is unspecified by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .peering | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "server/ConfigMap: peering configuration is set by if global.peering.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.peering.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .peering.enabled | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# server.limits.requestLimits

@test "server/ConfigMap: server.limits.requestLimits.mode is disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .limits.request_limits.mode | tee /dev/stderr)

  [ "${actual}" = "disabled" ]
}

@test "server/ConfigMap: server.limits.requestLimits.mode accepts disabled, permissive, and enforce" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.limits.requestLimits.mode=disabled' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .limits.request_limits.mode | tee /dev/stderr)

  [ "${actual}" = "disabled" ]

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.limits.requestLimits.mode=permissive' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .limits.request_limits.mode | tee /dev/stderr)

  [ "${actual}" = "permissive" ]

  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.limits.requestLimits.mode=enforce' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .limits.request_limits.mode | tee /dev/stderr)

  [ "${actual}" = "enforce" ]
}

@test "server/ConfigMap: server.limits.requestLimits.mode errors with value other than disabled, permissive, and enforce" {
  cd `chart_dir`
  run helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.limits.requestLimits.mode=notvalid' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "server.limits.requestLimits.mode must be one of the following values: disabled, permissive, and enforce" ]]
}

@test "server/ConfigMap: server.limits.request_limits.read_rate is -1 by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .limits.request_limits.read_rate | tee /dev/stderr)

  [ "${actual}" = "-1" ]
}

@test "server/ConfigMap: server.limits.request_limits.read_rate is set properly when specified " {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.limits.requestLimits.readRate=100' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .limits.request_limits.read_rate | tee /dev/stderr)

  [ "${actual}" = "100" ]
}

@test "server/ConfigMap: server.limits.request_limits.write_rate is -1 by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .limits.request_limits.write_rate | tee /dev/stderr)

  [ "${actual}" = "-1" ]
}

@test "server/ConfigMap: server.limits.request_limits.write_rate is set properly when specified " {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.limits.requestLimits.writeRate=100' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .limits.request_limits.write_rate | tee /dev/stderr)

  [ "${actual}" = "100" ]
}

#--------------------------------------------------------------------
# server.auditLogs

@test "server/ConfigMap: server.auditLogs is disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.auditLogs.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.data["audit-logging.json"]' | jq -r .audit | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "server/ConfigMap: server.auditLogs is enabled but ACLs are disabled" {
  cd `chart_dir`
  run helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.auditLogs.enabled=true' \
      --set 'server.auditLogs.sinks[0].name=MySink' \
      --set 'server.auditLogs.sinks[0].type=file' \
      --set 'server.auditLogs.sinks[0].format=json' \
      --set 'server.auditLogs.sinks[0].delivery_guarantee=best-effort' \
      --set 'server.auditLogs.sinks[0].rotate_duration=24h' \
      --set 'server.auditLogs.sinks[0].path=/tmp/audit.json' \
      .

  [ "$status" -eq 1 ]
  [[ "$output" =~ "ACLs must be enabled inorder to configure audit logs" ]]
}

@test "server/ConfigMap: server.auditLogs is enabled without sink inputs" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.auditLogs.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.data["audit-logging.json"]' | jq -r .audit.sink | tee /dev/stderr)

  [ "${actual}" = "{}" ]
}

@test "server/ConfigMap: server.auditLogs is enabled with 1 sink input object" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.auditLogs.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.auditLogs.sinks[0].name=MySink' \
      --set 'server.auditLogs.sinks[0].type=file' \
      --set 'server.auditLogs.sinks[0].format=json' \
      --set 'server.auditLogs.sinks[0].delivery_guarantee=best-effort' \
      --set 'server.auditLogs.sinks[0].rotate_duration=24h' \
      --set 'server.auditLogs.sinks[0].rotate_max_files=20' \
      --set 'server.auditLogs.sinks[0].rotate_bytes=12455355' \
      --set 'server.auditLogs.sinks[0].path=/tmp/audit.json' \
      . | tee /dev/stderr |
      yq -r '.data["audit-logging.json"]' | tee /dev/stderr)

  local actual=$(echo $object |  jq -r .audit.sink.MySink.path | tee /dev/stderr)
  [ "${actual}" = "/tmp/audit.json" ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink.delivery_guarantee | tee /dev/stderr)
  [ "${actual}" = "best-effort" ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink.rotate_duration | tee /dev/stderr)
  [ "${actual}" = "24h" ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink.rotate_max_files | tee /dev/stderr)
  [ ${actual} = 20 ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink.rotate_bytes | tee /dev/stderr)
  [ ${actual} = 12455355 ]
}

@test "server/ConfigMap: server.auditLogs is enabled with 1 sink input object and it does not contain the name attribute" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.auditLogs.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.auditLogs.sinks[0].name=MySink' \
      --set 'server.auditLogs.sinks[0].type=file' \
      --set 'server.auditLogs.sinks[0].format=json' \
      --set 'server.auditLogs.sinks[0].delivery_guarantee=best-effort' \
      --set 'server.auditLogs.sinks[0].rotate_duration=24h' \
      --set 'server.auditLogs.sinks[0].rotate_max_files=20' \
      --set 'server.auditLogs.sinks[0].rotate_bytes=12455355' \
      --set 'server.auditLogs.sinks[0].path=/tmp/audit.json' \
      . | tee /dev/stderr |
      yq -r '.data["audit-logging.json"]' | jq -r .audit.sink.name | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "server/ConfigMap: server.auditLogs is enabled with multiple sink input objects" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.auditLogs.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.auditLogs.sinks[0].name=MySink1' \
      --set 'server.auditLogs.sinks[0].type=file' \
      --set 'server.auditLogs.sinks[0].format=json' \
      --set 'server.auditLogs.sinks[0].delivery_guarantee=best-effort' \
      --set 'server.auditLogs.sinks[0].rotate_duration=24h' \
      --set 'server.auditLogs.sinks[0].rotate_max_files=15' \
      --set 'server.auditLogs.sinks[0].rotate_bytes=12445' \
      --set 'server.auditLogs.sinks[0].path=/tmp/audit.json' \
      --set 'server.auditLogs.sinks[1].name=MySink2' \
      --set 'server.auditLogs.sinks[1].type=file' \
      --set 'server.auditLogs.sinks[1].format=json' \
      --set 'server.auditLogs.sinks[1].delivery_guarantee=best-effort' \
      --set 'server.auditLogs.sinks[1].rotate_duration=24h' \
      --set 'server.auditLogs.sinks[1].rotate_max_files=25' \
      --set 'server.auditLogs.sinks[1].rotate_bytes=152445' \
      --set 'server.auditLogs.sinks[1].path=/tmp/audit-2.json' \
      --set 'server.auditLogs.sinks[2].name=MySink3' \
      --set 'server.auditLogs.sinks[2].type=file' \
      --set 'server.auditLogs.sinks[2].format=json' \
      --set 'server.auditLogs.sinks[2].delivery_guarantee=best-effort' \
      --set 'server.auditLogs.sinks[2].rotate_max_files=20' \
      --set 'server.auditLogs.sinks[2].rotate_duration=18h' \
      --set 'server.auditLogs.sinks[2].rotate_bytes=12445' \
      --set 'server.auditLogs.sinks[2].path=/tmp/audit-3.json' \
      . | tee /dev/stderr |
      yq -r '.data["audit-logging.json"]' | tee /dev/stderr)

  local actual=$(echo $object |  jq -r .audit.sink.MySink1.path | tee /dev/stderr)
  [ "${actual}" = "/tmp/audit.json" ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink3.path | tee /dev/stderr)
  [ "${actual}" = "/tmp/audit-3.json" ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink2.path | tee /dev/stderr)
  [ "${actual}" = "/tmp/audit-2.json" ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink1.name | tee /dev/stderr)
  [ "${actual}" = "null" ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink1.rotate_max_files | tee /dev/stderr)
  [ ${actual} = 15 ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink3.delivery_guarantee | tee /dev/stderr)
  [ "${actual}" = "best-effort" ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink2.rotate_duration | tee /dev/stderr)
  [ "${actual}" = "24h" ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink2.rotate_bytes | tee /dev/stderr)
  [ ${actual} = 152445 ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink1.format | tee /dev/stderr)
  [ "${actual}" = "json" ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink3.type | tee /dev/stderr)
  [ "${actual}" = "file" ]

  local actual=$(echo $object |  jq -r .audit.sink.MySink3.rotate_max_files | tee /dev/stderr)
  [ ${actual} = 20 ]
}

@test "server/ConfigMap: server.logLevel is empty" {
  cd `chart_dir`
  local configmap=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .log_level | tee /dev/stderr)

  [ "${configmap}" = "null" ]
}

@test "server/ConfigMap: server.logLevel is non empty" {
  cd `chart_dir`
  local configmap=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.logLevel=debug' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .log_level | tee /dev/stderr)

  [ "${configmap}" = "DEBUG" ]
}

#--------------------------------------------------------------------
# Datadog

@test "server/ConfigMap: when global.metrics.datadog.enabled=true, sets default telemetry.dogstatsd_addr config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true'  \
      --set 'global.metrics.datadog.dogstatsd.enabled=true'  \
      . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.dogstatsd_addr | tee /dev/stderr)

  [ "${actual}" = "unix:///var/run/datadog/dsd.socket" ]
}

@test "server/ConfigMap: when global.metrics.datadog.enabled=true, sets non-default telemetry.dogstatsd_addr config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true'  \
      --set 'global.metrics.datadog.dogstatsd.enabled=true'  \
      --set 'global.metrics.datadog.dogstatsd.socketTransportType="UDP"'  \
      --set 'global.metrics.datadog.dogstatsd.dogstatsdAddr="datadog-agent.default.svc.cluster.local"'  \
      . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.dogstatsd_addr | tee /dev/stderr)

  [ "${actual}" = "datadog-agent.default.svc.cluster.local" ]
}

@test "server/ConfigMap: when global.metrics.datadog.enabled=true, sets non-default namespace telemetry.dogstatsd_addr with non-default port config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true'  \
      --set 'global.metrics.datadog.dogstatsd.enabled=true'  \
      --set 'global.metrics.datadog.dogstatsd.socketTransportType="UDP"'  \
      --set 'global.metrics.datadog.dogstatsd.dogstatsdAddr="127.0.0.1"'  \
      --set 'global.metrics.datadog.dogstatsd.dogstatsdPort=8000'  \
      . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.dogstatsd_addr | tee /dev/stderr)

  [ "${actual}" = "127.0.0.1:8000" ]
}

@test "server/ConfigMap: when global.metrics.datadog.enabled=true, sets default telemetry.dogstatsd_tags config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true' \
      --set 'global.metrics.datadog.enabled=true'  \
      --set 'global.metrics.datadog.dogstatsd.enabled=true'  \
      . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.dogstatsd_tags | jq -r '[ .[] ]| join (" ")' | tee /dev/stderr)

  [ "${actual}" = "source:consul consul_service:consul-server" ]
}

@test "server/ConfigMap: when global.metrics.datadog.enabled=true, sets non-default telemetry.dogstatsd_tags config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.datadog.enabled=true'  \
      --set 'global.metrics.datadog.dogstatsd.enabled=true'  \
      --set 'global.metrics.datadog.dogstatsd.dogstatsdTags'='[\"source:consul-dataplane\"\,\"service:consul-server-connection-manager\"]' \
      . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.dogstatsd_tags | jq -r '[ .[] ]| join (" ")' | tee /dev/stderr)

  [ "${actual}" = "source:consul-dataplane service:consul-server-connection-manager" ]
}

#--------------------------------------------------------------------
# Consul Agent Metrics Prefix Filtering

@test "server/ConfigMap: when global.metrics.prefixFilter default, empty telemetry.prefix_filter string list" {
  cd `chart_dir`
  local actual=$(helm template \
  -s templates/server-config-configmap.yaml \
  --set 'global.metrics.enabled=true' \
  --set 'global.metrics.enableAgentMetrics=true' \
  . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.prefix_filter | jq -r '[ .[] ]| join (" ")' | tee /dev/stderr)

  [ "${actual}" = "" ]
}

@test "server/ConfigMap: when global.metrics.prefixFilter.allowList, sets correctly prepended telemetry.prefix_filter string list" {
  cd `chart_dir`
  local actual=$(helm template \
  -s templates/server-config-configmap.yaml \
  --set 'global.metrics.enabled=true' \
  --set 'global.metrics.enableAgentMetrics=true' \
  --set 'global.metrics.prefixFilter.allowList'={'"consul.rpc.server.call"'\,'"consul.grpc.server.call"'} \
  . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.prefix_filter | jq -r '[ .[] ]| join (" ")' | tee /dev/stderr)

  [ "${actual}" = "+consul.rpc.server.call +consul.grpc.server.call" ]
}

@test "server/ConfigMap: when global.metrics.prefixFilter.blockList, sets correctly prepended telemetry.prefix_filter string list" {
  cd `chart_dir`
  local actual=$(helm template \
  -s templates/server-config-configmap.yaml \
  --set 'global.metrics.enabled=true' \
  --set 'global.metrics.enableAgentMetrics=true' \
  --set 'global.metrics.prefixFilter.blockList'={'"consul.rpc.server.call"'\,'"consul.grpc.server.call"'} \
  . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.prefix_filter | jq -r '[ .[] ]| join (" ")' | tee /dev/stderr)

  [ "${actual}" = "-consul.rpc.server.call -consul.grpc.server.call" ]
}

@test "server/ConfigMap: when global.metrics.prefixFilter.blockList and allowList, sets correctly prepended telemetry.prefix_filter string list" {
  cd `chart_dir`
  local actual=$(helm template \
  -s templates/server-config-configmap.yaml \
  --set 'global.metrics.enabled=true' \
  --set 'global.metrics.enableAgentMetrics=true' \
  --set 'global.metrics.prefixFilter.allowList'={'"consul.rpc.server.call"'\,'"consul.http.GET"'} \
  --set 'global.metrics.prefixFilter.blockList'={'"consul.http"'\,'"consul.raft.apply"'} \
  . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.prefix_filter | jq -r '[ .[] ]| join (" ")' | tee /dev/stderr)

  [ "${actual}" = "+consul.rpc.server.call +consul.http.GET -consul.http -consul.raft.apply" ]
}

#--------------------------------------------------------------------
# Consul Agent Debug (PPROF)

@test "server/ConfigMap: global.server.enableAgentDebug default, sets default enable_debug = false in server agent config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .enable_debug | tee /dev/stderr)

  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: when global.server.enableAgentDebug=true, sets enable_debug = true in server agent config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.enableAgentDebug=true'  \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .enable_debug | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Consul Agent Telemetry Host Metrics

@test "server/ConfigMap: when global.metrics.enableHostMetrics is default, telemetry.enable_host_metrics = false in agent config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.enable_host_metrics | tee /dev/stderr)

  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: when global.metrics.enableHostMetrics=true, sets telemetry.enable_host_metrics = true in agent config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.enableHostMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.enable_host_metrics | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Consul Agent Telemetry Hostname Disable

@test "server/ConfigMap: when global.metrics.disableAgentHostName is default, telemetry.disableAgentHostName = false in agent config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.enable_host_metrics | tee /dev/stderr)

  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: when global.metrics.disableAgentHostName=true, sets telemetry.disableAgentHostName = true in agent config" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true'  \
      --set 'global.metrics.enableAgentMetrics=true'  \
      --set 'global.metrics.enableHostMetrics=true'  \
      . | tee /dev/stderr |
      yq -r '.data["telemetry-config.json"]' | jq -r .telemetry.enable_host_metrics | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# server.autopilot.min_quorum

@test "server/ConfigMap: autopilot.min_quorum=1 when replicas=1" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.replicas=1' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .autopilot.min_quorum | tee /dev/stderr)

  [ "${actual}" = "1" ]
}

@test "server/ConfigMap: autopilot.min_quorum=2 when replicas=2" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.replicas=2' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .autopilot.min_quorum | tee /dev/stderr)

  [ "${actual}" = "2" ]
}

@test "server/ConfigMap: autopilot.min_quorum=2 when replicas=3" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.replicas=3' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .autopilot.min_quorum | tee /dev/stderr)

  [ "${actual}" = "2" ]
}

@test "server/ConfigMap: autopilot.min_quorum=3 when replicas=4" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.replicas=4' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .autopilot.min_quorum | tee /dev/stderr)

  [ "${actual}" = "3" ]
}

@test "server/ConfigMap: autopilot.min_quorum=3 when replicas=5" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.replicas=5' \
      . | tee /dev/stderr |
      yq -r '.data["server.json"]' | jq -r .autopilot.min_quorum | tee /dev/stderr)

  [ "${actual}" = "3" ]
}
