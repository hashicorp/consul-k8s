#!/usr/bin/env bats

load _helpers

@test "server/ServiceAccount: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-serviceaccount.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ServiceAccount: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ServiceAccount: disabled with server disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-serviceaccount.yaml  \
      --set 'server.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/ServiceAccount: enabled with server enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-serviceaccount.yaml  \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "server/ServiceAccount: enabled with server enabled and global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/server-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      --set 'server.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
