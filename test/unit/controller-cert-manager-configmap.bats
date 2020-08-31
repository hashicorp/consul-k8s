#!/usr/bin/env bats

load _helpers

@test "controller-cert-manager/Configmap: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/controller-cert-manager-configmap.yaml  \
      .
}

@test "controller-cert-manager/Configmap: enabled with controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-cert-manager-configmap.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
