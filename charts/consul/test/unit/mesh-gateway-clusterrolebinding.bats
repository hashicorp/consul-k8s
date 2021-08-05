#!/usr/bin/env bats

load _helpers

@test "meshGateway/ClusterRoleBinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/mesh-gateway-clusterrolebinding.yaml  \
      .
}

@test "meshGateway/ClusterRoleBinding: enabled with meshGateway, connectInject enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-clusterrolebinding.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/ClusterRoleBinding: subject name is correct" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-clusterrolebinding.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.subjects[0].name' | tee /dev/stderr)
  [ "${actual}" = "RELEASE-NAME-consul-mesh-gateway" ]
}

