#!/usr/bin/env bats

load _helpers

@test "apiGateway/ClusterRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/api-gateway-controller-clusterrole.yaml  \
      .
}

@test "apiGateway/ClusterRole: enabled with apiGateway.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-clusterrole.yaml  \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/ClusterRole: uses PodSecurityPolicy with apiGateway.enabled=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
    -s templates/api-gateway-controller-clusterrole.yaml \
    --set 'global.enablePodSecurityPolicies=true' \
    --set 'apiGateway.enabled=true' \
    --set 'apiGateway.image=foo' \
    . | tee /dev/stderr |
    yq '.rules[] | select(.resourceNames[] == "release-name-consul-api-gateway-controller") | select(.resources[] == "podsecuritypolicies") | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
