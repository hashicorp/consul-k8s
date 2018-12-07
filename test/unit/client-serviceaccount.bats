#!/usr/bin/env bats

load _helpers

@test "client/ServiceAccount: enabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-serviceaccount.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ServiceAccount: disabled with global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/ServiceAccount: disabled with client disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-serviceaccount.yaml  \
      --set 'client.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "client/ServiceAccount: enabled with client enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-serviceaccount.yaml  \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "client/ServiceAccount: enabled with client enabled and global.enabled=false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/client-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}