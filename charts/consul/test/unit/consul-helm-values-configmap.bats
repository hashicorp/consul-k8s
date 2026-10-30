#!/usr/bin/env bats

load _helpers

@test "helmValuesConfigMap: ai.enabled defaults to false" {
  cd `chart_dir`
  local actual=$(helm template -s templates/consul-helm-values-configmap.yaml \
      --set connectInject.enabled=true --set terminatingGateways.enabled=true \
      . | tee /dev/stderr | yq -r '.data["values.json"]' | jq -r '.ai.enabled' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "helmValuesConfigMap: ai.enabled is passed to the controller" {
  cd `chart_dir`
  local actual=$(helm template -s templates/consul-helm-values-configmap.yaml \
      --set connectInject.enabled=true --set terminatingGateways.enabled=true \
      --set ai.enabled=true \
      . | tee /dev/stderr | yq -r '.data["values.json"]' | jq -r '.ai.enabled' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# terminatingGateways.defaults.annotations

@test "helmValuesConfigMap: terminatingGateways.defaults.annotations defaults to an empty object" {
  cd `chart_dir`
  local actual=$(helm template -s templates/consul-helm-values-configmap.yaml \
      --set connectInject.enabled=true --set terminatingGateways.enabled=true \
      . | tee /dev/stderr | yq -r '.data["values.json"]' | jq -c '.terminatingGateways.defaults.annotations' | tee /dev/stderr)
  [ "${actual}" = '{}' ]
}

@test "helmValuesConfigMap: terminatingGateways.defaults.annotations string is rendered as an object" {
  cd `chart_dir`
  local actual=$(helm template -s templates/consul-helm-values-configmap.yaml \
      --set connectInject.enabled=true --set terminatingGateways.enabled=true \
      --set 'terminatingGateways.defaults.annotations=foo: bar' \
      . | tee /dev/stderr | yq -r '.data["values.json"]' | jq -c '.terminatingGateways.defaults.annotations' | tee /dev/stderr)
  [ "${actual}" = '{"foo":"bar"}' ]
}

@test "helmValuesConfigMap: terminatingGateways.defaults.annotations templates are rendered" {
  cd `chart_dir`
  local actual=$(helm template -s templates/consul-helm-values-configmap.yaml \
      --set connectInject.enabled=true --set terminatingGateways.enabled=true \
      --set 'terminatingGateways.defaults.annotations=release: "{{ .Release.Name }}"' \
      . | tee /dev/stderr | yq -r '.data["values.json"]' | jq -r '.terminatingGateways.defaults.annotations.release' | tee /dev/stderr)
  [ "${actual}" = "release-name" ]
}

@test "helmValuesConfigMap: terminatingGateways.defaults.annotations fails if not a YAML map" {
  cd `chart_dir`
  run helm template -s templates/consul-helm-values-configmap.yaml \
      --set connectInject.enabled=true --set terminatingGateways.enabled=true \
      --set 'terminatingGateways.defaults.annotations=not-a-map' \
      .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "terminatingGateways.defaults.annotations must be a YAML map of annotations" ]]
}

#--------------------------------------------------------------------
# global.secretsBackend.vault

@test "helmValuesConfigMap: global.secretsBackend.vault.agentAnnotations defaults to an empty object" {
  cd `chart_dir`
  local actual=$(helm template -s templates/consul-helm-values-configmap.yaml \
      --set connectInject.enabled=true --set terminatingGateways.enabled=true \
      . | tee /dev/stderr | yq -r '.data["values.json"]' | jq -c '.global.secretsBackend.vault.agentAnnotations' | tee /dev/stderr)
  [ "${actual}" = '{}' ]
}

@test "helmValuesConfigMap: global.secretsBackend.vault.agentAnnotations string is rendered as an object" {
  cd `chart_dir`
  local actual=$(helm template -s templates/consul-helm-values-configmap.yaml \
      --set connectInject.enabled=true --set terminatingGateways.enabled=true \
      --set 'global.secretsBackend.vault.agentAnnotations=vault.hashicorp.com/agent-pre-populate-only: "true"' \
      . | tee /dev/stderr | yq -r '.data["values.json"]' | jq -c '.global.secretsBackend.vault.agentAnnotations' | tee /dev/stderr)
  [ "${actual}" = '{"vault.hashicorp.com/agent-pre-populate-only":"true"}' ]
}

@test "helmValuesConfigMap: global.secretsBackend.vault.vaultNamespace defaults to empty" {
  cd `chart_dir`
  local actual=$(helm template -s templates/consul-helm-values-configmap.yaml \
      --set connectInject.enabled=true --set terminatingGateways.enabled=true \
      . | tee /dev/stderr | yq -r '.data["values.json"]' | jq -r '.global.secretsBackend.vault.vaultNamespace' | tee /dev/stderr)
  [ "${actual}" = "" ]
}

@test "helmValuesConfigMap: global.secretsBackend.vault.vaultNamespace is passed to the controller" {
  cd `chart_dir`
  local actual=$(helm template -s templates/consul-helm-values-configmap.yaml \
      --set connectInject.enabled=true --set terminatingGateways.enabled=true \
      --set global.secretsBackend.vault.vaultNamespace=team-a \
      . | tee /dev/stderr | yq -r '.data["values.json"]' | jq -r '.global.secretsBackend.vault.vaultNamespace' | tee /dev/stderr)
  [ "${actual}" = "team-a" ]
}
