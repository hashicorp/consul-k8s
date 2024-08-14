#!/usr/bin/env bats

load _helpers

target=templates/gateway-cleanup-serviceaccount.yaml

@test "gatewaycleanup/ServiceAccount: enabled by default" {
    cd `chart_dir`
    local actual=$(helm template \
        -s $target \
        . | tee /dev/stderr |
        yq 'length > 0' | tee /dev/stderr)
    [ "$actual" = "true" ]
}

@test "gatewaycleanup/ServiceAccount: disabled with connectInject.enabled=false" {
    cd `chart_dir`
    assert_empty helm template \
        -s $target \
        --set 'connectInject.enabled=false' \
        . 
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "gatewaycleanup/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/gateway-cleanup-serviceaccount.yaml  \
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


