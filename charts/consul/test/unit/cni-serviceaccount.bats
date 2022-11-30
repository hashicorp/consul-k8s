#!/usr/bin/env bats

load _helpers

@test "cni/ServiceAccount: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-serviceaccount.yaml  \
      .
}

@test "cni/ServiceAccount: enabled with connectInject.cni.enabled=true and connectInject.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-serviceaccount.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == "true" ]]
}

@test "cni/ServiceAccount: disabled with connectInject.cni.enabled=false and connectInject.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      --set 'connectInject.cni.enabled=false' \
      --set 'connectInject.enabled=true' \
      -s templates/cni-serviceaccount.yaml  \
      .
}

@test "cni/ServiceAccount: cni namespace has a default when not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-serviceaccount.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r -c '.metadata.namespace' | tee /dev/stderr)
  [[ "${actual}" == "default" ]]
}

@test "cni/ServiceAccount: able to set cni namespace" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-serviceaccount.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.namespace=kube-system' \
      . | tee /dev/stderr |
      yq -r -c '.metadata.namespace' | tee /dev/stderr)
  [[ "${actual}" == "kube-system" ]]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "cni/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/cni-serviceaccount.yaml  \
      --set 'connectInject.cni.enabled=true' \
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

