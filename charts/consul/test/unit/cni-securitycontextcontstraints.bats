#!/usr/bin/env bats

load _helpers

@test "cni/SecurityContextConstraints: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-securitycontextconstraints.yaml  \
      .
}
@test "cni/SecurityContextConstraints: enabled with global.rbac.create false" {
  cd `chart_dir`
    assert_empty helm template \
        -s templates/cni-securitycontextconstraints.yaml \
        --set 'connectInject.cni.enabled=true' \
        --set 'connectInject.enabled=true' \
        --set 'global.rbac.create=false'  \
        .
}

@test "cni/SecurityContextConstraints: disabled when cni disabled and global.openshift.enabled=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/cni-securitycontextconstraints.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.enabled=false' \
      --set 'global.openshift.enabled=true' \
      .
}

@test "cni/SecurityContextConstraints: enabled when cni enabled and global.openshift.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-securitycontextconstraints.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.cni.enabled=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "cni/SecurityContextConstraints: cni namespace has a default when not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-securitycontextconstraints.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.openshift.enabled=true' \
      . | tee /dev/stderr |
      yq -r -c '.metadata.namespace' | tee /dev/stderr)
  [[ "${actual}" == "default" ]]
}

@test "cni/SecurityContextConstraints: able to set cni namespace" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/cni-securitycontextconstraints.yaml  \
      --set 'connectInject.cni.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.openshift.enabled=true' \
      --set 'connectInject.cni.namespace=kube-system' \
      . | tee /dev/stderr |
      yq -r -c '.metadata.namespace' | tee /dev/stderr)
  [[ "${actual}" == "kube-system" ]]
}
