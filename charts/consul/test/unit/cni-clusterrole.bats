#!/usr/bin/env bats

load _helpers

@test "cni/clusterrole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-clusterrole.yaml  \
      .
}

@test "cni/clusterrole: enabled with connectInject.cni.enabled=true and connectInject.transparentProxy.defaultEnabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-clusterrole.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == *"true"* ]]
}

@test "cni/clusterrole: disabled with connectInject.enabled=false and connectInject.transparentProxy.defaultEnabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-clusterrole.yaml  \
      .
}

@test "cni/clusterrole: throws error when connectInject.enabled=true and connectInject.transparentProxy.defaultEnabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/cni-clusterrole.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=false' \
      -s templates/cni-clusterrole.yaml  \
      .

  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.transparentProxy.defaultEnabled must be true if connectInject.cni.enabled is true" ]]
}

#--------------------------------------------------------------------
# rules

@test "cni/clusterrole: sets get, list, watch, patch, update to pods in all api groups" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/cni-clusterrole.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=true' \
     . | tee /dev/stderr |
      yq -r '.rules[0]' | tee /dev/stderr)

  local actual=$(echo $object | yq -r '.resources[| index("pods")' | tee /dev/stderr)
  [ "${actual}" != null ]

  # .apiGroups[0] = pods
  local actual=$(echo $object | yq -r '.apiGroups[0]' | tee /dev/stderr)
  [ "${actual}" = "" ]

  local actual=$(echo $object | yq -r '.verbs | index("get")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("list")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("watch")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("patch")' | tee /dev/stderr)
  [ "${actual}" != null ]

  local actual=$(echo $object | yq -r '.verbs | index("update")' | tee /dev/stderr)
  [ "${actual}" != null ]
}
