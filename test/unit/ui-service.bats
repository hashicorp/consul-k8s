#!/usr/bin/env bats

load _helpers

@test "ui/Service: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ui/Service: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      --set 'ui.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ui/Service: disable with server.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ui/Service: disable with ui.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      --set 'ui.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ui/Service: disable with ui.service.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      --set 'ui.service.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ui/Service: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ui/Service: disable with global.enabled and server.enabled on" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ui/Service: no type by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.type' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ui/Service: specified type" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      --set 'ui.service.type=LoadBalancer' \
      . | tee /dev/stderr |
      yq -r '.spec.type' | tee /dev/stderr)
  [ "${actual}" = "LoadBalancer" ]
}

#--------------------------------------------------------------------
# annotations

@test "ui/Service: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ui/Service: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      --set 'ui.service.annotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

#--------------------------------------------------------------------
# additionalSpec

@test "ui/Service: no additionalSpec by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.loadBalancerIP' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "ui/Service: additionalSpec can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ui-service.yaml  \
      --set 'ui.service.additionalSpec=loadBalancerIP: 1.2.3.4' \
      . | tee /dev/stderr |
      yq -r '.spec.loadBalancerIP' | tee /dev/stderr)
  [ "${actual}" = "1.2.3.4" ]
}
