#!/usr/bin/env bats

load _helpers

@test "client/ConfigMap: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ConfigMap: disable with client.enabled false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'client.enabled=true' \
      --set 'client.enabled=false' \
      .
}

@test "client/ConfigMap: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "client/ConfigMap: extraConfig is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'client.enabled=true' \
      --set 'client.extraConfig="{\"hello\": \"world\"}"' \
      . | tee /dev/stderr |
      yq '.data["extra-from-values.json"] | match("world") | length > 1' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# connectInject.centralConfig [DEPRECATED]

@test "client/ConfigMap: centralConfig is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.data["central-config.json"] | contains("enable_central_service_config")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ConfigMap: check_update_interval is set when connect is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.data["config.json"] | contains("check_update_interval")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# auto_reload_config

@test "client/ConfigMap: auto reload config is set to true when Vault is enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'client.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true'  \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      . | tee /dev/stderr |
      yq -r '.data["client.json"]' | jq -r .auto_reload_config | tee /dev/stderr)

  [ "${actual}" = "true" ]
}

@test "client/ConfigMap: auto reload config is config is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["client.json"]' | jq -r .auto_reload_config | tee /dev/stderr)

  [ "${actual}" = null ]
}

#--------------------------------------------------------------------
# logLevel

@test "client/ConfigMap: client.logLevel is empty" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["log-level.json"]' | jq -r .log_level | tee /dev/stderr)

  [ "${actual}" = "null" ]
}

@test "client/ConfigMap: client.logLevel is non empty" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'client.enabled=true' \
      --set 'client.logLevel=DEBUG' \
      . | tee /dev/stderr |
      yq -r '.data["log-level.json"]' | jq -r .log_level | tee /dev/stderr)

  [ "${actual}" = "DEBUG" ]
}
