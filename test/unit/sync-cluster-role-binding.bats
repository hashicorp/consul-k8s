#!/usr/bin/env bats

load _helpers

@test "sync/ClusterRoleBinding: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-cluster-role-binding.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "sync/ClusterRoleBinding: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-cluster-role-binding.yaml  \
      --set 'global.enabled=false' \
      --set 'syncCatalog.enabled=true' \
      --set 'rbac.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "sync/ClusterRoleBinding: disable with syncCatalog.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-cluster-role-binding.yaml  \
      --set 'syncCatalog.enabled=false' \
      --set 'rbac.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "sync/ClusterRoleBinding: disable with rbac.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-cluster-role-binding.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'rbac.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "sync/ClusterRoleBinding: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-cluster-role-binding.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}
