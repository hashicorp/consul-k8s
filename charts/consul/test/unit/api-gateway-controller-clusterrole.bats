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

@test "apiGateway/ClusterRole: can use podsecuritypolicies with apiGateway.enabled=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-clusterrole.yaml \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq '.rules[] | select((.resources[0] == "podsecuritypolicies") and (.verbs[0] == "use")) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "apiGateway/ClusterRole: can create roles and rolebindings with apiGateway.enabled=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/api-gateway-controller-clusterrole.yaml \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'apiGateway.enabled=true' \
      --set 'apiGateway.image=foo' \
      . | tee /dev/stderr |
      yq '.rules[] | select((.resources[0] == "roles") and (.resources[1] == "rolebindings") and (.verbs | contains(["create","get","list","watch"]))) | length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
