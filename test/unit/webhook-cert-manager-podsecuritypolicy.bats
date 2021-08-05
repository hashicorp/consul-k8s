#!/usr/bin/env bats

load _helpers

@test "webhookCertManager/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-podsecuritypolicy.yaml  \
      .
}

@test "webhookCertManager/PodSecurityPolicy: disabled by default with controller enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-podsecuritypolicy.yaml  \
      --set 'controller.enabled=true' \
      .
}

@test "webhookCertManager/PodSecurityPolicy: enabled with controller.enabled=true, connectInject.enabled=false and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-podsecuritypolicy.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/Configmap: enabled with connectInject.enabled=true, controller.enabled=false and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-podsecuritypolicy.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/Configmap: enabled with connectInject.enabled=true, controller.enabled=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-podsecuritypolicy.yaml  \
      --set 'controller.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
