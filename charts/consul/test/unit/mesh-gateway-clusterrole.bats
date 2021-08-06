#!/usr/bin/env bats

load _helpers

@test "meshGateway/ClusterRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/mesh-gateway-clusterrole.yaml  \
      .
}

@test "meshGateway/ClusterRole: enabled with meshGateway, connectInject enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-clusterrole.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/ClusterRole: rules for PodSecurityPolicy" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-clusterrole.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "podsecuritypolicies" ]
}

@test "meshGateway/ClusterRole: rules for global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-clusterrole.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}

@test "meshGateway/ClusterRole: rules for meshGateway.wanAddress.source=Service" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-clusterrole.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'meshGateway.service.enabled=true' \
      --set 'meshGateway.service.type=LoadBalancer' \
      --set 'meshGateway.wanAddress.source=Service' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "services" ]
}

@test "meshGateway/ClusterRole: rules is empty if no ACLs, PSPs and meshGateway.source != Service" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-clusterrole.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'meshGateway.wanAddress.source=NodeIP' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules' | tee /dev/stderr)
  [ "${actual}" = "[]" ]
}

@test "meshGateway/ClusterRole: rules for ACLs, PSPs and mesh gateways" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-clusterrole.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'meshGateway.service.enabled=true' \
      --set 'meshGateway.service.type=LoadBalancer' \
      --set 'meshGateway.wanAddress.source=Service' \
      . | tee /dev/stderr |
      yq -r '.rules | length' | tee /dev/stderr)
  [ "${actual}" = "3" ]
}
