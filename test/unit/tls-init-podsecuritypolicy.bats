#!/usr/bin/env bats

load _helpers

@test "tlsInit/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-podsecuritypolicy.yaml  \
      .
}

@test "tlsInit/PodSecurityPolicy: disabled by default with TLS enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-podsecuritypolicy.yaml  \
      --set 'global.tls.enabled=true' \
      .
}

@test "tlsInit/PodSecurityPolicy: disabled with TLS disabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/tls-init-podsecuritypolicy.yaml  \
      --set 'global.tls.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      .
}

@test "tlsInit/PodSecurityPolicy: enabled with TLS enabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/tls-init-podsecuritypolicy.yaml  \
      --set 'global.tls.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
