#!/usr/bin/env bats

load _helpers

@test "cni/ClusterRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-clusterrole.yaml  \
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

@test "cni/ClusterRole: disabled with connectInject.cni.enabled=false and connectInject.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      --set 'connectInject.cni.enabled=false' \
      --set 'connectInject.enabled=true' \
      -s templates/cni-clusterrole.yaml  \
      .
}

