#!/usr/bin/env bats

load _helpers

@test "serverACLInit/ClusterRole: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/ClusterRole: enabled with global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-clusterrole.yaml  \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "serverACLInit/ClusterRole: disabled with server=false and global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-clusterrole.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "serverACLInit/ClusterRole: enabled with client=false and global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-clusterrole.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# connectInject.enabled

@test "serverACLInit/ClusterRole: allows service accounts when connectInject.enabled is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-clusterrole.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "serviceaccounts")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "serverACLInit/ClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-acl-init-clusterrole.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}
