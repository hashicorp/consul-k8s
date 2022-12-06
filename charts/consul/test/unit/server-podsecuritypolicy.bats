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

#--------------------------------------------------------------------
# server.exposeGossipAndRPCPorts

@test "server/PodSecurityPolicy: hostPort 8300, 8301 and 8302 allowed when exposeGossipAndRPCPorts=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-podsecuritypolicy.yaml  \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'server.exposeGossipAndRPCPorts=true' \
      . | tee /dev/stderr |
      yq -c '.spec.hostPorts' | tee /dev/stderr)
  [ "${actual}" = '[{"min":8300,"max":8300},{"min":8301,"max":8301},{"min":8302,"max":8302},{"min":8502,"max":8502}]' ]
}

@test "server/PodSecurityPolicy: hostPort 8300, server.ports.serflan.port and 8302 allowed when exposeGossipAndRPCPorts=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-podsecuritypolicy.yaml  \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'server.exposeGossipAndRPCPorts=true' \
      --set 'server.ports.serflan.port=8333' \
      . | tee /dev/stderr |
      yq -c '.spec.hostPorts' | tee /dev/stderr)
  [ "${actual}" = '[{"min":8300,"max":8300},{"min":8333,"max":8333},{"min":8302,"max":8302},{"min":8502,"max":8502}]' ]
}
