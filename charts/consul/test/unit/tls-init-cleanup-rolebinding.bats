#!/usr/bin/env bats

load _helpers

@test "tlsInitCleanup/RoleBinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-cleanup-rolebinding.yaml  \
      .
}

@test "tlsInitCleanup/RoleBinding: disabled with global.tls.enabled=true and server.serverCert.secretName!=null" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-cleanup-rolebinding.yaml  \
      --set 'global.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=test' \
      --set 'server.serverCert.secretName=test' \
      .
}

@test "tlsInitCleanup/RoleBinding: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-cleanup-rolebinding.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.enabled=false' \
      .
}

@test "tlsInitCleanup/RoleBinding: enabled with global.tls.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-cleanup-rolebinding.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInitCleanup/RoleBinding: disabled when server.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-cleanup-rolebinding.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      .
}

@test "tlsInitCleanup/RoleBinding: enabled when global.tls.enabled=true and server.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-cleanup-rolebinding.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault

@test "tlsInitCleanup/RoleBinding: disabled with global.secretsBackend.vault.enabled=true and global.tls.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-cleanup-rolebinding.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.caCert.secretName=test' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      .
}
