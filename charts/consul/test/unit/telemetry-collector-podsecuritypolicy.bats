#!/usr/bin/env bats

load _helpers

@test "telemetryCollector/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/telemetry-collector-podsecuritypolicy.yaml  \
      .
}

@test "telemetryCollector/PodSecurityPolicy: enabled with telemetryCollector and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-podsecuritypolicy.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}