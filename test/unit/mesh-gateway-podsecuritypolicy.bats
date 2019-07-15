#!/usr/bin/env bats

load _helpers

@test "meshGateway/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-podsecuritypolicy.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "meshGateway/PodSecurityPolicy: enabled with meshGateway, connectInject and client.grpc enabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-podsecuritypolicy.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
