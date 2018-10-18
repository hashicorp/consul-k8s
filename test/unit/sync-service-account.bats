#!/usr/bin/env bats

load _helpers

@test "sync/ServiceAccount: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-service-account.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "sync/ServiceAccount: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-service-account.yaml  \
      --set 'global.enabled=false' \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.rbac.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "sync/ServiceAccount: disable with syncCatalog.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-service-account.yaml  \
      --set 'syncCatalog.enabled=false' \
      --set 'syncCatalog.rbac.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "sync/ServiceAccount: disable with syncCatalog.rbac.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-service-account.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.rbac.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "sync/ServiceAccount: disable with global.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-service-account.yaml  \
      --set 'syncCatalog.rbac.enabled="-"' \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}
