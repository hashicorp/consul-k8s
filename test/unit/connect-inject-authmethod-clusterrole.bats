#!/usr/bin/env bats

load _helpers

@test "connectInjectAuthMethod/ClusterRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-authmethod-clusterrole.yaml  \
      .
}

@test "connectInjectAuthMethod/ClusterRole: enabled with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-authmethod-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInjectAuthMethod/ClusterRole: disabled with connectInject.enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-authmethod-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      .
}

@test "connectInjectAuthMethod/ClusterRole: enabled with global.acls.manageSystemACLs.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-authmethod-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
