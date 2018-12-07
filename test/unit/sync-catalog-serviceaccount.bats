#!/usr/bin/env bats

load _helpers

@test "syncCatalog/ServiceAccount: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-serviceaccount.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/ServiceAccount: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/ServiceAccount: disabled with sync disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-serviceaccount.yaml  \
      --set 'syncCatalog.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/ServiceAccount: enabled with sync enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-serviceaccount.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/ServiceAccount: enabled with sync enabled and global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
