#!/usr/bin/env bats

load _helpers

@test "client/ClusterRoleBinding: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrolebinding.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ClusterRoleBinding: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrolebinding.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/ClusterRoleBinding: disabled with client disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrolebinding.yaml  \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/ClusterRoleBinding: enabled with client enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrolebinding.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ClusterRoleBinding: enabled with client enabled and global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-clusterrolebinding.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}