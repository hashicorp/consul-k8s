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
