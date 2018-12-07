#!/usr/bin/env bats

load _helpers

@test "syncCatalog/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-podsecuritypolicy.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/PodSecurityPolicy: disabled by default with syncCatalog enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-podsecuritypolicy.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/PodSecurityPolicy: disabled with syncCatalog disabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-podsecuritypolicy.yaml  \
      --set 'syncCatalog.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "syncCatalog/PodSecurityPolicy: enabled with syncCatalog enabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/sync-catalog-podsecuritypolicy.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
