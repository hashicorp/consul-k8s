#!/usr/bin/env bats

load _helpers

@test "cni/daemonset: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-daemonset.yaml  \
      .
}

@test "cni/daemonset: enabled with connectInject.cni.enabled=true and connectInject.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == *"true"* ]]
}

@test "cni/daemonset: disabled with connectInject.cni.enabled=false and connectInject.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      --set 'connectInject.cni.enabled=false' \
      --set 'connectInject.enabled=true' \
      -s templates/cni-daemonset.yaml  \
      .
}

#--------------------------------------------------------------------
# roleRef

@test "cni/clusterrolebinding: roleref name is correct" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-clusterrolebinding.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.roleRef.name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-cni" ]
}

#--------------------------------------------------------------------
# subjects

@test "cni/clusterrolebinding: subject name is correct" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-clusterrolebinding.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.subjects[0].name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-cni" ]
}

@test "cni/clusterrolebinding: subject namespace is correct" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-clusterrolebinding.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --namespace foo \
      . | tee /dev/stderr |
      yq -r '.subjects[0].namespace' | tee /dev/stderr)
  [ "${actual}" = "foo" ]
}

