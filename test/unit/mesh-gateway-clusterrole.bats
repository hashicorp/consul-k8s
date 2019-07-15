#!/usr/bin/env bats

load _helpers

@test "meshGateway/ClusterRole: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "meshGateway/ClusterRole: enabled with meshGateway, connectInject and client.grpc enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-clusterrole.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "meshGateway/ClusterRole: rules for PodSecurityPolicy" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-clusterrole.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "podsecuritypolicies" ]
}

@test "meshGateway/ClusterRole: rules for global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-clusterrole.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}

@test "meshGateway/ClusterRole: rules is empty if no ACLs or PSPs" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-clusterrole.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      . | tee /dev/stderr |
      yq -r '.rules' | tee /dev/stderr)
  [ "${actual}" = "[]" ]
}

@test "meshGateway/ClusterRole: rules for both ACLs and PSPs" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/mesh-gateway-clusterrole.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'client.grpc=true' \
      --set 'global.bootstrapACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules | length' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}
