#!/usr/bin/env bats

load _helpers

@test "terminatingGateways/Role: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/terminating-gateways-role.yaml  \
      .
}

@test "terminatingGateways/Role: enabled with terminatingGateways, connectInject and client.grpc enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-role.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "terminatingGateways/Role: rules for PodSecurityPolicy" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-role.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "podsecuritypolicies" ]
}

@test "terminatingGateways/Role: rules for global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-role.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].rules[0]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]

  local actual=$(echo $object | yq -r '.resourceNames[0]' | tee /dev/stderr)
  [ "${actual}" = "RELEASE-NAME-consul-terminating-gateway-terminating-gateway-acl-token" ]
}

@test "terminatingGateways/Role: rules is empty if no ACLs, PSPs" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-role.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].rules' | tee /dev/stderr)
  [ "${actual}" = "[]" ]
}

@test "terminatingGateways/Role: rules for ACLs, PSPs" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-role.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s -r '.[0].rules | length' | tee /dev/stderr)
  [ "${actual}" = "2" ]
}

@test "terminatingGateways/Role: rules for ACLs, PSPs with multiple gateways" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-role.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[1].name=gateway2' \
      . | tee /dev/stderr |
      yq -s -r '.' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.[0].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "RELEASE-NAME-consul-gateway1-terminating-gateway" ]

  local actual=$(echo $object | yq -r '.[1].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "RELEASE-NAME-consul-gateway2-terminating-gateway" ]

  local actual=$(echo $object | yq '.[0].rules | length' | tee /dev/stderr)
  [ "${actual}" = "2" ]

  local actual=$(echo $object | yq '.[1].rules | length' | tee /dev/stderr)
  [ "${actual}" = "2" ]

  local actual=$(echo $object | yq '.[2] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}
