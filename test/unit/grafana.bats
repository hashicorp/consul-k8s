#!/usr/bin/env bats

load _helpers

@test "grafana: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/grafana.yaml  \
      .
}

@test "grafana: enabled with grafana.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/grafana.yaml  \
      --set 'grafana.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == *"true"* ]]
}
