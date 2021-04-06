#!/usr/bin/env bats

load _helpers

@test "webhookCertManager/ClusterRoleBinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-clusterrolebinding.yaml  \
      .
}

@test "webhookCertManager/ClusterRoleBinding: enabled with controller.enabled=true and connectInject.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-clusterrolebinding.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/ClusterRoleBinding: enabled with connectInject.enabled=true and controller.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-clusterrolebinding.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/ClusterRoleBinding: enabled with connectInject.enabled=true and controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-clusterrolebinding.yaml  \
      --set 'controller.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
