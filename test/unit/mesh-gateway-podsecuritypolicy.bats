#!/usr/bin/env bats

load _helpers

@test "meshGateway/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/mesh-gateway-podsecuritypolicy.yaml  \
      .
}

@test "meshGateway/PodSecurityPolicy: enabled with meshGateway, connectInject enabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-podsecuritypolicy.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
