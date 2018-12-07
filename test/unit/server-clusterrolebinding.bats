#!/usr/bin/env bats

load _helpers

@test "server/ClusterRoleBinding: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrolebinding.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ClusterRoleBinding: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrolebinding.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ClusterRoleBinding: disabled with server disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrolebinding.yaml  \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ClusterRoleBinding: enabled with server enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrolebinding.yaml  \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ClusterRoleBinding: enabled with server enabled and global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-clusterrolebinding.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}