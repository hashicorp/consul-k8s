#!/usr/bin/env bats

load _helpers

@test "apiGateway/ClusterRoleBinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-clusterrolebinding.yaml  \
      .
}

@test "apiGateway/ClusterRoleBinding: enabled with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-clusterrolebinding.yaml  \
      --set 'global.enabled=false' \
      --set 'apiGateway.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/ClusterRoleBinding: disabled with connectInject.enabled false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-clusterrolebinding.yaml  \
      --set 'apiGateway.enabled=false' \
      .
}
