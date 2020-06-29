#!/usr/bin/env bats

load _helpers

@test "createFederationSecret/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/create-federation-secret-podsecuritypolicy.yaml  \
      .
}

@test "createFederationSecret/PodSecurityPolicy: disabled when global.federation.createFederationSecret=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/create-federation-secret-podsecuritypolicy.yaml  \
      --set 'global.federation.createFederationSecret=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      .
}

@test "createFederationSecret/PodSecurityPolicy: enabled with global.federation.createFederationSecret=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/create-federation-secret-podsecuritypolicy.yaml  \
      --set 'global.federation.createFederationSecret=true' \
      --set 'global.federation.enabled=true' \
      --set 'global.tls.enabled=true' \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
