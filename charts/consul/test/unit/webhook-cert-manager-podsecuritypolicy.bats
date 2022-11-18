#!/usr/bin/env bats

load _helpers

@test "webhookCertManager/PodSecurityPolicy: disabled by default with connect disabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-podsecuritypolicy.yaml  \
      --set 'connectInject.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      .
}

@test "webhookCertManager/PodSecurityPolicy: disabled by default with PSP disabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-podsecuritypolicy.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=false' \
      .
}

@test "webhookCertManager/PodSecurityPolicy: enabled with connectInject.enabled=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-podsecuritypolicy.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}


#--------------------------------------------------------------------
# Vault

@test "webhookCertManager/PodSecurityPolicy: disabled when the following are configured - global.secretsBackend.vault.enabled, global.secretsBackend.vault.enabled, global.secretsBackend.vault.connectInjectRole, global.secretsBackend.vault.connectInject.tlsCert.secretName, and global.secretsBackend.vault.connectInject.caCert.secretName" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-podsecuritypolicy.yaml  \
      --set 'global.enablePodSecurityPolicies=true' \
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
