#!/usr/bin/env bats

load _helpers

@test "cni/ClusterRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-clusterrole.yaml  \
      .
}
@test "cni/ClusterRole: enabled with global.rbac.create false" {
  cd `chart_dir`
    assert_empty helm template \
        -s templates/cni-clusterrole.yaml \
        --set 'connectInject.cni.enabled=true' \
        --set 'connectInject.enabled=true' \
        --set 'global.rbac.create=false'  \
        .
}
@test "cni/ClusterRole: enabled with connectInject.cni.enabled=true and connectInject.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-clusterrole.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == "true" ]]
}

@test "cni/ClusterRole: cni namespace has a default when not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-clusterrole.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r -c '.metadata.namespace' | tee /dev/stderr)
  [[ "${actual}" == "default" ]]
}

@test "cni/ClusterRole: able to set cni namespace" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-clusterrole.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.cni.namespace=kube-system' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r -c '.metadata.namespace' | tee /dev/stderr)
  [[ "${actual}" == "kube-system" ]]
}

@test "cni/ClusterRole: disabled with connectInject.cni.enabled=false and connectInject.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      --set 'connectInject.cni.enabled=false' \
      --set 'connectInject.enabled=true' \
      -s templates/cni-clusterrole.yaml  \
      .
}

