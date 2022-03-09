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

@test "meshGateway/PodSecurityPolicy: hostNetwork defaults to false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-podsecuritypolicy.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq '.spec.hostNetwork' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "meshGateway/PodSecurityPolicy: hostNetwork allowed if set to true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-podsecuritypolicy.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'meshGateway.hostNetwork=true' \
      . | tee /dev/stderr |
      yq '.spec.hostNetwork' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/PodSecurityPolicy: hostPorts are allowed when setting hostPort" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-podsecuritypolicy.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'meshGateway.hostPort=9999' \
      . | tee /dev/stderr |
      yq -c '.spec.hostPorts' | tee /dev/stderr)
  [ "${actual}" = '[{"min":9999,"max":9999}]' ]
}

@test "meshGateway/PodSecurityPolicy: hostPorts are allowed when hostNetwork=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-podsecuritypolicy.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'meshGateway.hostNetwork=true' \
      . | tee /dev/stderr |
      yq -c '.spec.hostPorts' | tee /dev/stderr)
  [ "${actual}" = '[{"min":8443,"max":8443}]' ]
}
