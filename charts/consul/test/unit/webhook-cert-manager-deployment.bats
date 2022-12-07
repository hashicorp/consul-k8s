#!/usr/bin/env bats

load _helpers

@test "webhookCertManager/Deployment: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-deployment.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/Deployment: enabled with connectInject.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/Deployment: no tolerations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.tolerations' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "webhookCertManager/Deployment: tolerations can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'webhookCertManager.tolerations=- key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.tolerations[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

@test "webhookCertManager/Deployment: no nodeSelector by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector' | tee /dev/stderr)
  [ "${actual}" = "null" ]
}

@test "webhookCertManager/Deployment: nodeSelector can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-deployment.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'webhookCertManager.nodeSelector=- key: value' \
      . | tee /dev/stderr |
      yq -r '.spec.template.spec.nodeSelector[0].key' | tee /dev/stderr)
  [ "${actual}" = "value" ]
}

#--------------------------------------------------------------------
# Vault

@test "webhookCertManager/Deployment: disabled when the following are configured - global.secretsBackend.vault.enabled, global.secretsBackend.vault.enabled, global.secretsBackend.vault.connectInjectRole, global.secretsBackend.vault.connectInject.tlsCert.secretName, and global.secretsBackend.vault.connectInject.caCert.secretName" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-deployment.yaml  \
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

#--------------------------------------------------------------------
# extraLabels

@test "webhookCertManager/Deployment: no extra labels defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-deployment.yaml  \
      . | tee /dev/stderr |
      yq -r '.spec.template.metadata.labels | del(."app") | del(."chart") | del(."release") | del(."component") | del(."heritage")' | tee /dev/stderr)
  [ "${actual}" = "{}" ]
}

@test "webhookCertManager/Deployment: extra global labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-deployment.yaml  \
      --set 'global.extraLabels.foo=bar' \
      . | tee /dev/stderr)
  local actualBar=$(echo "${actual}" | yq -r '.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualBar}" = "bar" ]
  local actualTemplateBar=$(echo "${actual}" | yq -r '.spec.template.metadata.labels.foo' | tee /dev/stderr)
  [ "${actualTemplateBar}" = "bar" ]
}

@test "webhookCertManager/Deployment: multiple global extra labels can be set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-deployment.yaml  \
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
