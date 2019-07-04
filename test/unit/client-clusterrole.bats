#!/usr/bin/env bats

load _helpers

@test "client/ClusterRole: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ClusterRole: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrole.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/ClusterRole: can be enabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ClusterRole: disabled with client.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrole.yaml  \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/ClusterRole: enabled with client.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrole.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

# The rules key must always be set (#178).
@test "client/ClusterRole: rules empty with client.enabled=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrole.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq '.rules' | tee /dev/stderr)
  [ "${actual}" = "[]" ]
}

#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "client/ClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrole.yaml  \
      --set 'client.enabled=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "podsecuritypolicies" ]
}

#--------------------------------------------------------------------
# global.bootstrapACLs

@test "client/ClusterRole: allows secret access with global.bootsrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrole.yaml  \
      --set 'client.enabled=true' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules[0].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}

@test "client/ClusterRole: allows secret access with global.bootsrapACLs=true and global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrole.yaml  \
      --set 'client.enabled=true' \
      --set 'global.bootstrapACLs=true' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules[1].resources[0]' | tee /dev/stderr)
  [ "${actual}" = "secrets" ]
}
