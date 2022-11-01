#!/usr/bin/env bats

load _helpers

@test "apiGateway/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-podsecuritypolicy.yaml  \
      .
}

@test "apiGateway/PodSecurityPolicy: enabled with apiGateway.enabled=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-podsecuritypolicy.yaml  \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
