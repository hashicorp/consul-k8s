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
# extraLabels

@test "tlsInit/Job: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "tlsInit/Job: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "tlsInit/Job: multiple extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.extraLabels.foo=bar' \
      --set 'global.extraLabels.baz=qux' \
      . | tee /dev/stderr)
  local actualFoo=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  local actualBaz=$(echo "${actual}" | yq -r '.metadata.labels.baz' | tee /dev/stderr)
  [ "${actualFoo}" = "bar" ]
  [ "${actualBaz}" = "qux" ]
  local actualTemplateFoo=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  local actualTemplateBaz=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.baz' | tee /dev/stderr)
  [ "${actualTemplateFoo}" = "bar" ]
  [ "${actualTemplateBaz}" = "qux" ]
}

#--------------------------------------------------------------------
# server.containerSecurityContext.tlsInit

@test "tlsInitCleanup/Job: securityContext is set when server.containerSecurityContext.tlsInit is set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'server.containerSecurityContext.tlsInit.runAsUser=100' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.securityContext.runAsUser' | tee /dev/stderr)

  [ "${actual}" = "100" ]
}


#--------------------------------------------------------------------
# annotations

@test "tlsInitCleanup/Job: no annotations defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations |
      del(."consul.hashicorp.com/connect-inject") |
      del(."consul.hashicorp.com/mesh-inject") |
      del(."consul.hashicorp.com/config-checksum")' |
      tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "tlsInitCleanup/Job: annotations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-cleanup-job.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.tls.annotations=foo: bar' \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
