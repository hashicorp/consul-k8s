#!/usr/bin/env bats

load _helpers

@test "controller/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/controller-podsecuritypolicy.yaml  \
      .
}

@test "controller/PodSecurityPolicy: disabled by default with controller enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/controller-podsecuritypolicy.yaml  \
      --set 'controller.enabled=true' \
      .
}

@test "controller/PodSecurityPolicy: enabled with controller enabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/controller-podsecuritypolicy.yaml  \
      --set 'controller.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
