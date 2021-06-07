#!/usr/bin/env bats

load _helpers

@test "syncCatalog/ServiceAccount: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-serviceaccount.yaml  \
      .
}

@test "syncCatalog/ServiceAccount: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "syncCatalog/ServiceAccount: disabled with sync disabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-serviceaccount.yaml  \
      --set 'syncCatalog.enabled=false' \
      .
}

@test "syncCatalog/ServiceAccount: enabled with sync enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-serviceaccount.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/ServiceAccount: enabled with sync enabled and global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "syncCatalog/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/sync-catalog-serviceaccount.yaml  \
      --set 'syncCatalog.enabled=true' \
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

@test "syncCatalog/ServiceAccount: no annotations by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-serviceaccount.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq '.metadata.annotations | length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/ServiceAccount: annotations when enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-serviceaccount.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set "syncCatalog.serviceAccount.annotations=foo: bar" \
      . | tee /dev/stderr |
      yq -r '.metadata.annotations.foo' | tee /dev/stderr)
  [ "${actual}" = "bar" ]
}
