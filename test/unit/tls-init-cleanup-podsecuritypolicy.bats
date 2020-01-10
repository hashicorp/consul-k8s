#!/usr/bin/env bats

load _helpers

@test "tlsInitCleanup/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-cleanup-podsecuritypolicy.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInitCleanup/PodSecurityPolicy: disabled by default with TLS enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-cleanup-podsecuritypolicy.yaml  \
      --set 'global.tls.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInitCleanup/PodSecurityPolicy: disabled with TLS disabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-cleanup-podsecuritypolicy.yaml  \
      --set 'global.tls.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "tlsInitCleanup/PodSecurityPolicy: enabled with TLS enabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/tls-init-cleanup-podsecuritypolicy.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
