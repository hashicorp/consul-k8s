#!/usr/bin/env bats

load _helpers

@test "server/ConfigMap: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-config-configmap.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ConfigMap: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-config-configmap.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ConfigMap: disable with server.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-config-configmap.yaml  \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-config-configmap.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: extraConfig is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-config-configmap.yaml  \
      --set 'server.extraConfig="{\"hello\": \"world\"}"' \
      . | tee /dev/stderr |
      yq '.data["extra-from-values.json"] | match("world") | length' | tee /dev/stderr)
  [ ! -z "${actual}" ]
}

#--------------------------------------------------------------------
# global.bootstrapACLs

@test "server/ConfigMap: creates acl config with .global.bootstrapACLs enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-config-configmap.yaml  \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq '.data["acl-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# connectInject.centralConfig

@test "server/ConfigMap: centralConfig is disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-config-configmap.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.data["central-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: centralConfig can be enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-config-configmap.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=true' \
      . | tee /dev/stderr |
      yq '.data["central-config.json"] | contains("enable_central_service_config")' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ConfigMap: proxyDefaults disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-config-configmap.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=true' \
      . | tee /dev/stderr |
      yq '.data["proxy-defaults-config.json"] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ConfigMap: proxyDefaults can be enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-config-configmap.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.centralConfig.enabled=true' \
      --set 'connectInject.centralConfig.proxyDefaults="{\"hello\": \"world\"}"' \
      . | tee /dev/stderr |
      yq '.data["proxy-defaults-config.json"] | match("world") | length' | tee /dev/stderr)
  [ ! -z "${actual}" ]
}
