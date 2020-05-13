#!/usr/bin/env bats

load _helpers

@test "ingressGateway/RoleBinding: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ingress-gateways-rolebinding.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "ingressGateway/RoleBinding: enabled with ingressGateways, connectInject enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/ingress-gateways-rolebinding.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/RoleBinding: multiple gateways" {
  cd `chart_dir`
  local object=$(helm template \
      -x templates/ingress-gateways-role.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[1].name=gateway2' \
      . | tee /dev/stderr |
      yq -s -r '.' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.[0].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-gateway1" ]

  local actual=$(echo $object | yq -r '.[1].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-gateway2" ]

  local actual=$(echo $object | yq '.[2] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}
