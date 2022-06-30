#!/usr/bin/env bats

load _helpers

@test "cni/clusterrolebinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-clusterrolebinding.yaml  \
      .
}

@test "cni/clusterrolebinding: enabled with connectInject.cni.enabled=true and connectInject.transparentProxy.defaultEnabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-clusterrolebinding.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == *"true"* ]]
}

@test "cni/clusterrolebinding: disabled with connectInject.cni.enabled=false and connectInject.transparentProxy.defaultEnabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-clusterrolebinding.yaml  \
      --set 'connectInject.cni.enabled=false' \
      .
}

@test "cni/clusterrolebinding: throws error when connectInject.cni.enabled=true and connectInject.transparentProxy.defaultEnabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/cni-clusterrolebinding.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=false' \
      -s templates/cni-clusterrolebinding.yaml  \
      .

  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.transparentProxy.defaultEnabled must be true if connectInject.cni.enabled is true" ]]
}

#--------------------------------------------------------------------
# roleRef

@test "cni/clusterrolebinding: roleref name is correct" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-clusterrolebinding.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=true' \
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
      --set 'connectInject.transparentProxy.defaultEnabled=true' \
      . | tee /dev/stderr |
      yq -r '.subjects[0].name' | tee /dev/stderr)
  [ "${actual}" = "release-name-consul-cni" ]
}

@test "cni/clusterrolebinding: subject namespace is correct" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-clusterrolebinding.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=true' \
      . | tee /dev/stderr |
      yq -r '.subjects[0].namespace' | tee /dev/stderr)
  [ "${actual}" = "default" ]
}

