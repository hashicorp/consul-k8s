#!/usr/bin/env bats

load _helpers

target=templates/sync-catalog-cleanup-serviceaccount.yaml

@test "syncCatalogCleanup/ServiceAccount: disabled by default" {
  cd $(chart_dir)
  assert_empty helm template \
    -s $target \
    .
}

@test "syncCatalogCleanup/ServiceAccount: disabled with cleanup disabled" {
  cd $(chart_dir)
  assert_empty helm template \
    -s $target \
    --set 'syncCatalog.cleanupNodeOnRemoval=false' \
    .
}

@test "syncCatalogCleanup/ServiceAccount: enabled with cleanup enabled" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    . | tee /dev/stderr |
    yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "syncCatalogCleanup/ServiceAccount: can set image pull secrets" {
  cd $(chart_dir)
  local object=$(helm template \
    -s $target \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set 'global.imagePullSecrets[0].name=my-secret' \
    --set 'global.imagePullSecrets[1].name=my-secret2' \
    . | tee /dev/stderr)

  local actual=$(echo "$object" |
    yq -r '.imagePullSecrets[0].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret" ]

  local actual=$(echo "$object" |
    yq -r '.imagePullSecrets[1].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret2" ]
}

#--------------------------------------------------------------------
# syncCatalog.serviceAccount.annotations

@test "syncCatalogCleanup/ServiceAccount: no annotations by default" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    . | tee /dev/stderr |
    yq '.metadata.annotations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalogCleanup/ServiceAccount: annotations when enabled" {
  cd $(chart_dir)
  local actual=$(helm template \
    -s $target \
    --set 'syncCatalog.cleanupNodeOnRemoval=true' \
    --set "syncCatalog.serviceAccount.annotations=foo: bar" \
    . | tee /dev/stderr |
    yq -r '.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
