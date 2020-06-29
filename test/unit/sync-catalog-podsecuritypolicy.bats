#!/usr/bin/env bats

load _helpers

@test "syncCatalog/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-podsecuritypolicy.yaml  \
      .
}

@test "syncCatalog/PodSecurityPolicy: disabled by default with syncCatalog enabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-podsecuritypolicy.yaml  \
      --set 'syncCatalog.enabled=true' \
      .
}

@test "syncCatalog/PodSecurityPolicy: disabled with syncCatalog disabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-podsecuritypolicy.yaml  \
      --set 'syncCatalog.enabled=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      .
}

@test "syncCatalog/PodSecurityPolicy: enabled with syncCatalog enabled and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-podsecuritypolicy.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
