#!/usr/bin/env bats

load _helpers

@test "testRunner/Pod: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tests/test-runner.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "testRunner/Pod: disabled when tests.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tests/test-runner.yaml  \
      --set 'tests.enabled=false' \
      .
}
