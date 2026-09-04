#!/usr/bin/env bats

load _helpers

@test "server/ClusterRole: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ClusterRole: disabled with global.enabled=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-clusterrole.yaml  \
      --set 'global.enabled=false' \
      .
}

@test "server/ClusterRole: disabled with server disabled" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/server-clusterrole.yaml  \
      --set 'server.enabled=false' \
      .
}

@test "server/ClusterRole: enabled with server enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-clusterrole.yaml  \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ClusterRole: enabled with server enabled and global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/server-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
