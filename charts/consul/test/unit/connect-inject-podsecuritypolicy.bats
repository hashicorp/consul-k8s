#!/usr/bin/env bats

load _helpers

@test "connectInject/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-podsecuritypolicy.yaml  \
      .
}

@test "connectInject/PodSecurityPolicy: disabled by default with connectInject enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-podsecuritypolicy.yaml  \
      --set 'connectInject.enabled=true' \
      .
}

@test "connectInject/PodSecurityPolicy: disabled with connectInject disabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-podsecuritypolicy.yaml  \
      --set 'connectInject.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      .
}

@test "connectInject/PodSecurityPolicy: enabled with connectInject enabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-podsecuritypolicy.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
