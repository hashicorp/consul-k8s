#!/usr/bin/env bats

load _helpers

@test "client/ConfigMap: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ConfigMap: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ConfigMap: disable with client.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/client-config-configmap.yaml  \
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
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.data["central-config.json"] | contains("enable_central_service_config")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# connectInject.healthChecks

@test "client/ConfigMap: check_update_interval is not set by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq '.data["config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/ConfigMap: check_update_interval is set when health checks enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/client-config-configmap.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.healthChecks.enabled=true' \
      . | tee /dev/stderr |
      yq '.data["config.json"] | contains("check_update_interval")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
