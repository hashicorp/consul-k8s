#!/usr/bin/env bats

load _helpers

@test "server/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-podsecuritypolicy.yaml  \
      .
}

@test "server/PodSecurityPolicy: disabled with server disabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-podsecuritypolicy.yaml  \
      --set 'server.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      .
}

@test "server/PodSecurityPolicy: enabled with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-podsecuritypolicy.yaml  \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
