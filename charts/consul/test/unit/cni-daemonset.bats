#!/usr/bin/env bats

load _helpers

@test "cni/daemonset: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-daemonset.yaml  \
      .
}

@test "cni/daemonset: enabled with connectInject.cni.enabled=true and connectInject.transparentProxy.defaultEnabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-daemonset.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == *"true"* ]]
}

@test "cni/daemonset: disabled with connectInject.enabled=false and connectInject.transparentProxy.defaultEnabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-daemonset.yaml  \
      .
}

@test "cni/daemonset: throws error when connectInject.enabled=true and connectInject.transparentProxy.defaultEnabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=false' \
      -s templates/cni-daemonset.yaml  \
      .

  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.transparentProxy.defaultEnabled must be true if connectInject.cni.enabled=true" ]]
}

