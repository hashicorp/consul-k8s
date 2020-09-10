#!/usr/bin/env bats

load _helpers

@test "controller/Certificate: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/controller-certificate.yaml  \
      .
}

@test "controller/Certificate: enabled with controller.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-certificate.yaml  \
      --set 'controller.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
