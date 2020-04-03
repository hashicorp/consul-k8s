#!/usr/bin/env bats

load _helpers

@test "connectInjectAuthMethod/ClusterRoleBinding: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-authmethod-clusterrolebinding.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInjectAuthMethod/ClusterRoleBinding: enabled with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-authmethod-clusterrolebinding.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInjectAuthMethod/ClusterRoleBinding: disabled with connectInject.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-authmethod-clusterrolebinding.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInjectAuthMethod/ClusterRoleBinding: enabled with global.acls.manageSystemACLs.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-authmethod-clusterrolebinding.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
