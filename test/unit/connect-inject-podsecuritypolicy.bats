#!/usr/bin/env bats

load _helpers

@test "connectInject/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-podsecuritypolicy.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/PodSecurityPolicy: disabled by default with connectInject enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-podsecuritypolicy.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/PodSecurityPolicy: disabled with connectInject disabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-podsecuritypolicy.yaml  \
      --set 'connectInject.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/PodSecurityPolicy: enabled with connectInject enabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-podsecuritypolicy.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
