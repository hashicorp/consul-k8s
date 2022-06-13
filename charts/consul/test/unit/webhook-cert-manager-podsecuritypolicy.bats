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

@test "webhookCertManager/PodSecurityPolicy: enabled with connectInject.enabled=true, controller.enabled=false and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-podsecuritypolicy.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/PodSecurityPolicy: enabled with connectInject.enabled=true, controller.enabled=true and global.enablePodSecurityPolicies=true" {
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

#--------------------------------------------------------------------
# Vault

@test "webhookCertManager/PodSecurityPolicy: disabled when the following are configured - global.secretsBackend.vault.enabled, global.secretsBackend.vault.enabled, global.secretsBackend.vault.connectInjectRole, global.secretsBackend.vault.connectInject.tlsCert.secretName, global.secretsBackend.vault.connectInject.caCert.secretName, global.secretsBackend.vault.controllerRole, global.secretsBackend.vault.controller.tlsCert.secretName, and .global.secretsBackend.vault.controller.caCert.secretName" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-podsecuritypolicy.yaml  \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'controller.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.connectInjectRole=inject-ca-role' \
      --set 'global.secretsBackend.vault.connectInject.tlsCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.connectInject.caCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.controllerRole=test' \
      --set 'global.secretsBackend.vault.controller.caCert.secretName=foo/ca' \
      --set 'global.secretsBackend.vault.controller.tlsCert.secretName=foo/tls' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test2' \
      .
}
