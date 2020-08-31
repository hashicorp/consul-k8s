#!/usr/bin/env bats

load _helpers

@test "syncCatalog/ClusterRole: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-clusterrole.yaml  \
      .
}

@test "syncCatalog/ClusterRole: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-clusterrole.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "syncCatalog/ClusterRole: disabled with sync disabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/sync-catalog-clusterrole.yaml  \
      --set 'syncCatalog.enabled=false' \
      .
}

@test "syncCatalog/ClusterRole: enabled with sync enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-clusterrole.yaml  \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "syncCatalog/ClusterRole: enabled with sync enabled and global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'syncCatalog.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "syncCatalog/ClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-clusterrole.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[2].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "podsecuritypolicies" ]
}

#--------------------------------------------------------------------
# global.acls.manageSystemACLs

@test "syncCatalog/ClusterRole: allows secret access with global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-clusterrole.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules[2].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}

#--------------------------------------------------------------------
# syncCatalog.toK8S={true,false}

@test "syncCatalog/ClusterRole: has reduced permissions if toK8s=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-clusterrole.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.toK8S=false' \
      . | tee /dev/stderr |
      yq -c '.rules[0].verbs' | tee /dev/stderr)
  [ "${actual}" = '["get","list","watch"]' ]
}

@test "syncCatalog/ClusterRole: has full permissions if toK8s=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/sync-catalog-clusterrole.yaml  \
      --set 'syncCatalog.enabled=true' \
      --set 'syncCatalog.toK8S=true' \
      . | tee /dev/stderr |
      yq -c '.rules[0].verbs' | tee /dev/stderr)
  [ "${actual}" = '["get","list","watch","update","patch","delete","create"]' ]
}
