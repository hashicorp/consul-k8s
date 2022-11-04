#!/usr/bin/env bats

load _helpers

@test "tlsInitCleanup/Job: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      .
}

@test "tlsInitCleanup/Job: disabled with global.tls.enabled=true and server.serverCert.secretName!=null" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.caCert.secretName=test' \
      --set 'server.serverCert.secretName=test' \
      .
}

@test "tlsInitCleanup/Job: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.enabled=false' \
      .
}

@test "tlsInitCleanup/Job: enabled with global.tls.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "tlsInitCleanup/Job: disabled when server.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=false' \
      .
}

@test "tlsInitCleanup/Job: enabled when global.tls.enabled=true and server.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault

@test "tlsInitCleanup/Job: disabled with global.secretsBackend.vault.enabled=true and global.tls.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.secretsBackend.vault.enabled=true' \
      --set 'global.secretsBackend.vault.consulClientRole=foo' \
      --set 'global.secretsBackend.vault.consulServerRole=test' \
      --set 'global.secretsBackend.vault.consulCARole=test' \
      --set 'global.tls.caCert.secretName=test' \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.enableAutoEncrypt=true' \
      .
}

#--------------------------------------------------------------------
# global.podSecurityStandards

@test "tlsInitCleanup/Job: podSecurityStandards default off" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.containers | map(select(.name == "tls-init-cleanup")) | .[0].securityContext | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInitCleanup/Job: global.podSecurityStandards are not set when global.openshift.enabled=true" {
  cd `chart_dir`
  local manifest=$(helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      --set 'global.podSecurityStandards.securityContext.bob=false' \
      --set 'global.podSecurityStandards.securityContext.alice=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr)

  local actual=$(echo "$manifest" | yq -r '.spec.template.spec.containers | map(select(.name == "tls-init-cleanup")) | .[0].securityContext')
  [ "${actual}" = "null" ]
}

@test "tlsInitCleanup/Job: global.podSecurityStandards can be set with tls and acls enabled" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.enabled=true' \
      --set 'global.podSecurityStandards.securityContext.bob=false' \
      --set 'global.podSecurityStandards.securityContext.alice=true' \
      . | tee /dev/stderr |
      yq '.spec.template.spec' | tee /dev/stderr)

  local actual=$(echo $object |
      yq -r '.containers | map(select(.name=="tls-init-cleanup")) | .[0].securityContext' | jq -r .bob)
  [ "${actual}" = "false" ]
  local actual=$(echo $object |
      yq -r '.containers | map(select(.name=="tls-init-cleanup")) | .[0].securityContext' | jq -r .alice)
  [ "${actual}" = "true" ]
}
