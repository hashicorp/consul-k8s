#!/usr/bin/env bats

load _helpers

@test "componentAuthmethod/ClusterRoleBinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/component-authmethod-clusterrolebinding.yaml  \
      .
}

@test "componentAuthmethod/ClusterRoleBinding: enabled with global.acls.manageSystemACLs true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/component-authmethod-clusterrolebinding.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}