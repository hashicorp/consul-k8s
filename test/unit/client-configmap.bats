#!/usr/bin/env bats

load _helpers

@test "client/ConfigMap: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ConfigMap: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-config-configmap.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ConfigMap: disable with client.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-config-configmap.yaml  \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/ConfigMap: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-config-configmap.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/ConfigMap: extraConfig is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-config-configmap.yaml  \
      --set 'client.extraConfig="{\"hello\": \"world\"}"' \
      . | tee /dev/stderr |
      yq '.data["extra-from-values.json"] | match("world") | length' | tee /dev/stderr)
  [ ! -z "${actual}" ]
}

#--------------------------------------------------------------------
# connectInject.centralConfig

@test "client/ConfigMap: centralConfig is disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-config-configmap.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.data["central-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/ConfigMap: centralConfig can be enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-config-configmap.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=true' \
      . | tee /dev/stderr |
      yq '.data["central-config.json"] | contains("enable_central_service_config")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
