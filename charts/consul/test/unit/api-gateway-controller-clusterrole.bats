#!/usr/bin/env bats

load _helpers

@test "apiGateway/ClusterRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-clusterrole.yaml  \
      .
}

@test "apiGateway/ClusterRole: enabled with apiGateway.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-clusterrole.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
