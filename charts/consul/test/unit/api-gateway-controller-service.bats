#!/usr/bin/env bats

load _helpers

@test "apiGateway/Service: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-service.yaml  \
      .
}

@test "apiGateway/Service: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-service.yaml  \
      --set 'global.enabled=false' \
      --set 'apiGateway.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/Service: disable with apiGateway.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-service.yaml  \
      --set 'apiGateway.enabled=false' \
      .
}

@test "apiGateway/Service: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-service.yaml  \
      --set 'global.enabled=false' \
      .
}
