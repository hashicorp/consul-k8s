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

@test "sync/ServiceAccount: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-service-account.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "sync/ServiceAccount: disabled with sync disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-service-account.yaml  \
      --set 'syncCatalog.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "sync/ServiceAccount: enabled with sync enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-service-account.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "sync/ServiceAccount: enabled with sync enabled and global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-service-account.yaml  \
      --set 'global.enabled=false' \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
