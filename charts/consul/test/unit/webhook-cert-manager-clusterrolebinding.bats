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

#--------------------------------------------------------------------
# Vault

@test "webhookCertManager/ClusterRoleBinding: disabled when the following are configured - global.secretsBackend.vault.enabled, global.secretsBackend.vault.enabled, global.secretsBackend.vault.consulConnectInjectRole, global.secretsBackend.vault.connectInject.tlsCert.secretName, global.secretsBackend.vault.connectInject.caCert.secretName, global.secretsBackend.vault.consulControllerRole, global.secretsBackend.vault.controller.tlsCert.secretName, and .global.secretsBackend.vault.controller.caCert.secretName" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-clusterrolebinding.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.consulConnectInjectRole=inject-ca-role' \
      --set 'global.secretsBackend.vault.connectInject.tlsCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.connectInject.caCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.consulControllerRole=test' \
      --set 'global.secretsBackend.vault.controller.caCert.secretName=foo/ca' \
      --set 'global.secretsBackend.vault.controller.tlsCert.secretName=foo/tls' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test2' \
      .
}
