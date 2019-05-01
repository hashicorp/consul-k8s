#!/usr/bin/env bats

load _helpers

@test "enterpriseLicense/ClusterRoleBinding: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrolebinding.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ClusterRoleBinding: disabled with global.bootstrapACLs=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrolebinding.yaml  \
      --set 'global.bootstrapACLs=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ClusterRoleBinding: disabled with server=false, global.bootstrapACLs=true, ent secret defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrolebinding.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'server.enabled=false' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ClusterRoleBinding: disabled with client=false, global.bootstrapACLs=true, ent secret defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrolebinding.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'client.enabled=false' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ClusterRoleBinding: disabled when ent secretName missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrolebinding.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ClusterRoleBinding: disabled when ent secretKey missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrolebinding.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ClusterRoleBinding: can be enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-clusterrolebinding.yaml  \
      --set 'global.bootstrapACLs=true' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
