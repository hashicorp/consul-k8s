#!/usr/bin/env bats

load _helpers

@test "controllerWebhook/Service: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-webhook-service.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
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
