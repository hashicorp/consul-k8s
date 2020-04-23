#!/usr/bin/env bats

load _helpers

@test "serverACLInitCleanup/ClusterRole: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-cleanup-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInitCleanup/ClusterRole: enabled with global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-cleanup-clusterrole.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInitCleanup/ClusterRole: disabled with server=false and global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-cleanup-clusterrole.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInitCleanup/ClusterRole: enabled with client=true and global.acls.manageSystemACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-cleanup-clusterrole.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInitCleanup/ClusterRole: enabled with externalServers.enabled=true and global.acls.manageSystemACLs=true, but server.enabled set to false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-cleanup-clusterrole.yaml  \
      --set 'server.enabled=false' \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'externalServers.enabled=true' \
      --set 'externalServers.https.address=foo.com' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInitCleanup/ClusterRole: fails if both externalServers.enabled=true and server.enabled=true" {
  cd `chart_dir`
  run helm template \
      -x templates/server-acl-init-cleanup-clusterrole.yaml  \
      --set 'server.enabled=true' \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "only one of server.enabled or externalServers.enabled can be set" ]]
}

@test "serverACLInitCleanup/ClusterRole: fails if both externalServers.enabled=true and server.enabled not set to false" {
  cd `chart_dir`
  run helm template \
      -x templates/server-acl-init-cleanup-clusterrole.yaml  \
      --set 'externalServers.enabled=true' .
  [ "$status" -eq 1 ]
  [[ "$output" =~ "only one of server.enabled or externalServers.enabled can be set" ]]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "serverACLInitCleanup/ClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-cleanup-clusterrole.yaml  \
      --set 'global.acls.manageSystemACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}
