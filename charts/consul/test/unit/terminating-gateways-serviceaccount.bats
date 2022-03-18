#!/usr/bin/env bats

load _helpers

@test "terminatingGateways/ServiceAccount: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/terminating-gateways-serviceaccount.yaml  \
      .
}

@test "terminatingGateways/ServiceAccount: enabled with terminatingGateways, connectInject and client.grpc enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-serviceaccount.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "terminatingGateways/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-serviceaccount.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.imagePullSecrets[0].name=my-secret' \
      --set 'global.imagePullSecrets[1].name=my-secret2' \
      . | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -s -r '.[0].imagePullSecrets[0].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret" ]

  local actual=$(echo "$object" |
      yq -s -r '.[0].imagePullSecrets[1].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret2" ]
}

#--------------------------------------------------------------------
# multiple gateways

@test "terminatingGateways/ServiceAccount: multiple gateways" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/terminating-gateways-serviceaccount.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set 'terminatingGateways.gateways[1].name=gateway2' \
      . | tee /dev/stderr |
      yq -s -r '.' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.[0].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "RELEASE-NAME-consul-gateway1-terminating-gateway" ]

  local actual=$(echo $object | yq -r '.[1].metadata.name' | tee /dev/stderr)
  [ "${actual}" = "RELEASE-NAME-consul-gateway2-terminating-gateway" ]

  local actual=$(echo "$object" |
      yq -r '.[2] | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

#--------------------------------------------------------------------
# terminatingGateways.serviceAccount.annotations

@test "terminatingGateways/ServiceAccount: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/terminating-gateways-serviceaccount.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.metadata.annotations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "terminatingGateways/ServiceAccount: annotations when enabled" {
  cd `chart_dir`

  local object=$(helm template \
      -s templates/terminating-gateways-serviceaccount.yaml  \
      --set 'terminatingGateways.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set "terminatingGateways.defaults.serviceAccount.annotations=default: foo" \
      --set 'terminatingGateways.gateways[0].name=gateway1' \
      --set "terminatingGateways.gateways[0].serviceAccount.annotations=gateway1: bar" \
      . | tee /dev/stderr |
      yq -s -r '.' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.[0].metadata.annotations.default' | tee /dev/stderr)
  [ "${actual}" = "foo" ]

  local actual=$(echo $object | yq -r '.[0].metadata.annotations.gateway1' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
