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

@test "server/ConfigMap: extraConfig is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'server.extraConfig="{\"hello\": \"world\"}"' \
      . | tee /dev/stderr |
      yq '.data["extra-from-values.json"] | match("world") | length' | tee /dev/stderr)
  [ ! -z "${actual}" ]
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
# global.metrics.enabled & ui.enabled

@test "server/ConfigMap: creates ui config with .ui.enabled=true and .global.metrics.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.metrics.enabled=true' \
      --set 'ui.enabled=true' \
      . | tee /dev/stderr |
      yq '.data["ui-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ConfigMap: creates ui config with .ui.enabled=true and .ui.metrics.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'ui.metrics.enabled=true' \
      --set 'ui.enabled=true' \
      . | tee /dev/stderr |
      yq '.data["ui-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ConfigMap: does not create ui config when .ui.enabled=false and .ui.metrics.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'ui.enabled=false' \
      --set 'ui.metrics.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: does not create ui config when .ui.enabled=true and .global.metrics.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'ui.enabled=true' \
      --set 'global.metrics.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: does not create ui config when .ui.enabled=true and .ui.metrics.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'ui.enabled=true' \
      --set 'ui.metrics.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.data["ui-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
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

@test "server/ConfigMap: enable_token_replication is not set when acls.replicationToken.secretName is set but secretKey is not" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.replicationToken.secretName=name' \
      . | tee /dev/stderr |
      yq -r '.data["acl-config.json"]' | yq -r '.acl.enable_token_replication' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "server/ConfigMap: enable_token_replication is not set when acls.replicationToken.secretKey is set but secretName is not" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-config-configmap.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.acls.replicationToken.secretKey=key' \
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
  [ "${actual}" = '"{\n  \"connect\": [\n    {\n      \"ca_config\": [\n        {\n          \"address\": \"example.com\",\n          \"intermediate_pki_path\": \"int\",\n          \"root_pki_path\": \"root\",\n          \"auth_method\": {\n            \"type\": \"kubernetes\",\n            \"params\": {\n              \"role\": \"foo\"\n            }\n          }\n        }\n      ],\n      \"ca_provider\": \"vault\"\n    }\n  ]\n}\n"' ]

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