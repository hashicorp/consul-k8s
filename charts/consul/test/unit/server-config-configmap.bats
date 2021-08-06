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
