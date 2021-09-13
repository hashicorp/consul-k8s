#!/usr/bin/env bats

load _helpers

@test "meshGateway/ServiceAccount: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/mesh-gateway-serviceaccount.yaml  \
      .
}

@test "meshGateway/ServiceAccount: enabled with meshGateway, connectInject enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-serviceaccount.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "meshGateway/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/mesh-gateway-serviceaccount.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.imagePullSecrets[0].name=my-secret' \
      --set 'global.imagePullSecrets[1].name=my-secret2' \
      . | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[0].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret" ]

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[1].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret2" ]
}

#--------------------------------------------------------------------
# meshGateway.serviceAccount.annotations

@test "meshGateway/ServiceAccount: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-serviceaccount.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq '.metadata.annotations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "meshGateway/ServiceAccount: annotations when enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/mesh-gateway-serviceaccount.yaml  \
      --set 'meshGateway.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set "meshGateway.serviceAccount.annotations=foo: bar" \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
