#!/usr/bin/env bats

load _helpers

@test "ingressGateways/Role: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/ingress-gateways-role.yaml  \
      .
}

@test "ingressGateways/Role: enabled with ingressGateways, connectInject enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-role.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "ingressGateways/Role: rules for PodSecurityPolicy" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-role.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].rules[1].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "podsecuritypolicies" ]
}

@test "ingressGateways/Role: rules for global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-role.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].rules[1]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]

  local actual=$(echo $object | yq -r '.resourceNames[0]' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-ingress-gateway-acl-token" ]
}

@test "ingressGateways/Role: rules for ingressGateways service" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-role.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "services" ]
}

@test "ingressGateways/Role: rules for ACLs, PSPs and ingress gateways" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/ingress-gateways-role.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].rules | length' | tee /dev/stderr)
  [ "${actual}" = "3" ]
}

@test "ingressGateways/Role: rules for ACLs, PSPs and ingress gateways with multiple gateways" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/ingress-gateways-role.yaml  \
      --set 'ingressGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'ingressGateways.gateways[0].name=gateway1' \
      --set 'ingressGateways.gateways[1].name=gateway2' \
      . | tee /dev/stderr |
      yq -s -r '.' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.[0].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-gateway1" ]

  local actual=$(echo $object | yq -r '.[1].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-gateway2" ]

  local actual=$(echo $object | yq '.[0].rules | length' | tee /dev/stderr)
  [ "${actual}" = "3" ]

  local actual=$(echo $object | yq '.[1].rules | length' | tee /dev/stderr)
  [ "${actual}" = "3" ]

  local actual=$(echo $object | yq '.[2] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}
