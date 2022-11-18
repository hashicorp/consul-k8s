#!/usr/bin/env bats

load _helpers

@test "webhookCertManager/ServiceAccount: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-serviceaccount.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/ServiceAccount: enabled with connectInject.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-serviceaccount.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "webhookCertManager/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/webhook-cert-manager-serviceaccount.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.imagePullSecrets[0].name=my-secret' \
      --set 'global.imagePullSecrets[1].name=my-secret2' \
      . | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[0].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret" ]

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[1].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret2" ]
}

#--------------------------------------------------------------------
# Vault

@test "webhookCertManager/ServiceAccount: disabled when the following are configured - global.secretsBackend.vault.enabled, global.secretsBackend.vault.connectInjectRole, global.secretsBackend.vault.connectInject.tlsCert.secretName, and global.secretsBackend.vault.connectInject.caCert.secretName" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-serviceaccount.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=test' \
      --set 'global.secretsBackend.vault.consulServerRole=foo' \
      --set 'global.secretsBackend.vault.consulCARole=carole' \
      --set 'global.secretsBackend.vault.connectInjectRole=inject-ca-role' \
      --set 'global.secretsBackend.vault.connectInject.tlsCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.connectInject.caCert.secretName=pki/issue/connect-webhook-cert-dc1' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=bar' \
      --set 'global.secretsBackend.vault.consulCARole=test2' \
      .
}
