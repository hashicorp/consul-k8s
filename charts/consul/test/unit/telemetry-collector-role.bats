#!/usr/bin/env bats

load _helpers

@test "telemetryCollector/Role: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/telemetry-collector-role.yaml  \
      .
}

@test "telemetryCollector/Role: enabled with telemetryCollector and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-role.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/Role: enabled with global.rbac.create false" {
  cd `chart_dir`
    assert_empty helm template \
      -s templates/telemetry-collector-role.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'global.rbac.create=false'  \
      .
}