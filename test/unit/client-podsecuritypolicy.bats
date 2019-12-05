#!/usr/bin/env bats

load _helpers

@test "client/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-podsecuritypolicy.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/PodSecurityPolicy: disabled with client disabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-podsecuritypolicy.yaml  \
      --set 'client.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/PodSecurityPolicy: enabled with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-podsecuritypolicy.yaml  \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# client.exposeGossipPorts

@test "client/PodSecurityPolicy: hostPort 8301 allowed when exposeGossipPorts=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-podsecuritypolicy.yaml  \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'client.exposeGossipPorts=true' \
      . | tee /dev/stderr |
      yq -c '.spec.hostPorts' | tee /dev/stderr)
  [ "${actual}" = '[{"min":8500,"max":8500},{"min":8502,"max":8502},{"min":8301,"max":8301}]' ]
}
