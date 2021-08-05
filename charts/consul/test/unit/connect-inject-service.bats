#!/usr/bin/env bats

load _helpers

@test "connectInject/Service: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-service.yaml  \
      .
}

@test "connectInject/Service: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-service.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/Service: disable with connectInject.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-service.yaml  \
      --set 'connectInject.enabled=false' \
      .
}

@test "connectInject/Service: disable with global.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-service.yaml  \
      --set 'global.enabled=false' \
      .
}
