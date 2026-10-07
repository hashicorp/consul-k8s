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
