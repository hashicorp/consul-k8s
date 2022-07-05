#!/usr/bin/env bats

load _helpers

@test "cni/resourcequota: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-resourcequota.yaml  \
      .
}

@test "cni/resourcequota: enabled with connectInject.cni.enabled=true and connectInject.transparentProxy.defaultEnabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-resourcequota.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [[ "${actual}" == *"true"* ]]
}

@test "cni/resourcequota: disabled with connectInject.cni.enabled=false and connectInject.transparentProxy.defaultEnabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-resourcequota.yaml  \
      --set 'connectInject.cni.enabled=false' \
      .
}

@test "cni/resourcequota: throws error when connectInject.enabled=true and connectInject.transparentProxy.defaultEnabled=false" {
  cd `chart_dir`
  run helm template \
      -s templates/cni-resourcequota.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.transparentProxy.defaultEnabled=false' \
      -s templates/cni-resourcequota.yaml  \
      .

  [ "$status" -eq 1 ]
  [[ "$output" =~ "connectInject.transparentProxy.defaultEnabled must be true if connectInject.cni.enabled is true" ]]
}

#--------------------------------------------------------------------
# pods 

@test "cni/resourcequota: resources defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-resourcequota.yaml  \
      --set 'connectInject.cni.enabled=true' \
      . | tee /dev/stderr |
      yq -rc '.spec.hard.pods' | tee /dev/stderr)
  [ "${actual}" = "5000" ]
}

@test "cni/resourcequota: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-resourcequota.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.cni.resourceQuota.pods=bar' \
      . | tee /dev/stderr |
      yq -rc '.spec.hard.pods' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

