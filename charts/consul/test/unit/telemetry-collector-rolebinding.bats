#!/usr/bin/env bats

load _helpers

@test "telemetryCollector/RoleBinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/telemetry-collector-rolebinding.yaml  \
      .
}

@test "telemetryCollector/RoleBinding: enabled with telemetryCollector and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/telemetry-collector-rolebinding.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "telemetryCollector/RoleBinding: enabled with global.rbac.create false" {
  cd `chart_dir`
    assert_empty helm template \
      -s templates/telemetry-collector-rolebinding.yaml  \
      --set 'telemetryCollector.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'global.rbac.create=false'  \
      .
}