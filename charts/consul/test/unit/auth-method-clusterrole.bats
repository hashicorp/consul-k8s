#!/usr/bin/env bats

load _helpers

@test "auth-method/ClusterRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/auth-method-clusterrole.yaml  \
      .
}

@test "auth-method/ClusterRole: enabled with global.acls.manageSystemACLs true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/auth-method-clusterrole.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "auth-method/ClusterRole: enabled with global.rbac.create false" {
  cd `chart_dir`
    assert_empty helm template \
        -s templates/auth-method-clusterrole.yaml \
        --set 'global.acls.manageSystemACLs=true' \
        --set 'global.rbac.create=false' \
        .
}