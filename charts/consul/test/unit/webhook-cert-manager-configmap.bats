#!/usr/bin/env bats

load _helpers

@test "webhookCertManager/Configmap: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-configmap.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/Configmap: enabled with controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-configmap.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/Configmap: enabled with connectInject.enabled=true and controller.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-configmap.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/Configmap: enabled with connectInject.enabled=true and controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/webhook-cert-manager-configmap.yaml  \
      --set 'controller.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/Configmap: configuration has only controller webhook with controller.enabled=true" {
  cd `chart_dir`
  local cfg=$(helm template \
      -s templates/webhook-cert-manager-configmap.yaml  \
      --set 'controller.enabled=true' \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq -r '.data["webhook-config.json"]' | tee /dev/stderr)

  local actual=$(echo $cfg | jq '. | length == 1')
  [ "${actual}" = "true" ]

  local actual=$(echo $cfg | jq '.[0].name | contains("controller")')
  [ "${actual}" = "true" ]
}

@test "webhookCertManager/Configmap: configuration has only controller webhook with connectInject.enabled=true" {
  cd `chart_dir`
  local cfg=$(helm template \
      -s templates/webhook-cert-manager-configmap.yaml  \
      --set 'controller.enabled=false' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["webhook-config.json"]' | tee /dev/stderr)

  local actual=$(echo $cfg | jq '. | length == 1')
  [ "${actual}" = "true" ]

  local actual=$(echo $cfg | jq '.[0].name | contains("controller")')
  [ "${actual}" = "false" ]
}

@test "webhookCertManager/Configmap: configuration contains both controller and connectInject webhook with connectInject.enabled=true and controller.enabled=true" {
  cd `chart_dir`
  local cfg=$(helm template \
      -s templates/webhook-cert-manager-configmap.yaml  \
      --set 'controller.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.data["webhook-config.json"]' | tee /dev/stderr)


  local actual=$(echo $cfg | jq '. | length == 2')
  [ "${actual}" = "true" ]

  local actual=$(echo $cfg | jq '.[0].name | contains("connect-injector")')
  [ "${actual}" = "true" ]

  local actual=$(echo $cfg | jq '.[1].name | contains("controller")')
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# Vault

@test "webhookCertManager/Configmap: disabled when the following are configured - global.secretsBackend.vault.enabled, global.secretsBackend.vault.enabled, global.secretsBackend.vault.connectInjectRole, global.secretsBackend.vault.connectInject.tlsCert.secretName, global.secretsBackend.vault.connectInject.caCert.secretName, global.secretsBackend.vault.controllerRole, global.secretsBackend.vault.controller.tlsCert.secretName, and .global.secretsBackend.vault.controller.caCert.secretName" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/webhook-cert-manager-configmap.yaml  \
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
