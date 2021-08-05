#!/usr/bin/env bats

load _helpers

@test "prometheus: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/prometheus.yaml  \
      .
}

@test "prometheus: enabled with prometheus.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/prometheus.yaml  \
      --set 'prometheus.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == *"true"* ]]
}
