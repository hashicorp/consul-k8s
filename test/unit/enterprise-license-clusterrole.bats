#!/usr/bin/env bats

load _helpers

@test "enterpriseLicense/ClusterRole: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ClusterRole: disabled with server=false, ent secret defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrole.yaml  \
      --set 'server.enabled=false' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ClusterRole: disabled when ent secretName missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrole.yaml  \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ClusterRole: disabled when ent secretKey missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrole.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ClusterRole: enabled when ent license defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrole.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "enterpriseLicense/ClusterRole: rules are empty if global.bootstrapACLs and global.enablePodSecurityPolicies are false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrole.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq '.rules | length' | tee /dev/stderr)
  [ "${actual}" = "0" ]
}

#--------------------------------------------------------------------
# global.bootstrapACLs

@test "enterpriseLicense/ClusterRole: allows acl token when global.bootstrapACLs is true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrole.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resourceNames[0] == "release-name-consul-enterprise-license-acl-token")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}


#--------------------------------------------------------------------
# global.enablePodSecurityPolicies

@test "enterpriseLicense/ClusterRole: allows podsecuritypolicies access with global.enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrole.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq -r '.rules | map(select(.resources[0] == "podsecuritypolicies")) | length' | tee /dev/stderr)
  [ "${actual}" = "1" ]
}
