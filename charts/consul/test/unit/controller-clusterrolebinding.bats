#!/usr/bin/env bats

load _helpers

@test "controller/ClusterRoleBinding: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-clusterrolebinding.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "controller/ClusterRoleBinding: enabled with controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-clusterrolebinding.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
