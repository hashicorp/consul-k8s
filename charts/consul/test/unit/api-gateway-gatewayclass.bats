#!/usr/bin/env bats

load _helpers

@test "apiGateway/GatewayClass: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-gatewayclass.yaml  \
      .
}

@test "apiGateway/GatewayClass: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-gatewayclass.yaml  \
      --set 'global.enabled=false' \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/GatewayClass: disable with apiGateway.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-gatewayclass.yaml  \
      --set 'apiGateway.enabled=false' \
      .
}

@test "apiGateway/GatewayClass: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-gatewayclass.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "apiGateway/GatewayClass: disable with apiGateway.managedGatewayClass.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-gatewayclass.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      --set 'apiGateway.managedGatewayClass.enabled=false' \
      .
}
