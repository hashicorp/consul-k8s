#!/usr/bin/env bats

load _helpers

@test "tlsInit/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-podsecuritypolicy.yaml  \
      .
}

@test "tlsInit/PodSecurityPolicy: disabled with global.tls.enabled=true and server.serverCert.secretName!=null" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-podsecuritypolicy.yaml  \
      --set 'global.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=test' \
      --set 'server.serverCert.secretName=test' \
      .
}

@test "tlsInit/PodSecurityPolicy: disabled by default with TLS enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-podsecuritypolicy.yaml  \
      --set 'global.tls.enabled=true' \
      .
}

@test "tlsInit/PodSecurityPolicy: disabled with TLS disabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-podsecuritypolicy.yaml  \
      --set 'global.tls.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      .
}

@test "tlsInit/PodSecurityPolicy: enabled with TLS enabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-podsecuritypolicy.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault

@test "tlsInit/PodSecurityPolicy: disabled with global.secretsBackend.vault.enabled=true and global.tls.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-podsecuritypolicy.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.caCert.secretName=test' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      .
}
