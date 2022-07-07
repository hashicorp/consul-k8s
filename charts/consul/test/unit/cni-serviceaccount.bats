#!/usr/bin/env bats

load _helpers

@test "cni/serviceaccount: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-serviceaccount.yaml  \
      .
}

@test "cni/serviceaccount: enabled with connectInject.cni.enabled=true and connectInject.transparentProxy.defaultEnabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-serviceaccount.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == *"true"* ]]
}

@test "cni/serviceaccount: disabled with connectInject.cni.enabled=false and connectInject.transparentProxy.defaultEnabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-serviceaccount.yaml  \
      --set 'connectInject.cni.enabled=false' \
      .
}

@test "cni/serviceaccount: throws error when connectInject.enabled=true and connectInject.transparentProxy.defaultEnabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/cni-serviceaccount.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=false' \
      -s templates/cni-serviceaccount.yaml  \
      .

  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.transparentProxy.defaultEnabled must be true if connectInject.cni.enabled is true" ]]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "cni/serviceaccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/cni-serviceaccount.yaml  \
      --set 'connectInject.cni.enabled=true' \
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

