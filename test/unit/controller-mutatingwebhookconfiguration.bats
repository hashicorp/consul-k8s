#!/usr/bin/env bats

load _helpers

@test "controller/MutatingWebhookConfiguration: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/controller-mutatingwebhookconfiguration.yaml  \
      .
}

@test "controller/MutatingWebhookConfiguration: enabled with controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-mutatingwebhookconfiguration.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
