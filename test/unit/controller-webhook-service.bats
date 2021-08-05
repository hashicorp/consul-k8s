#!/usr/bin/env bats

load _helpers

@test "controllerWebhook/Service: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/controller-webhook-service.yaml  \
      .
}

@test "controllerWebhook/Service: enabled with controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-webhook-service.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
