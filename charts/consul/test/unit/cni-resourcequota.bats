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
# pods 

@test "cni/resourcequota: resources defined by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-resourcequota.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -rc '.spec.hard.pods' | tee /dev/stderr)
  [ "${actual}" = "5000" ]
}

@test "cni/resourcequota: resources can be overridden" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-resourcequota.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.resourceQuota.pods=bar' \
      . | tee /dev/stderr |
      yq -rc '.spec.hard.pods' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}

