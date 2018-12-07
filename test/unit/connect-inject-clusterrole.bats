#!/usr/bin/env bats

load _helpers

@test "connectInject/ClusterRole: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ClusterRole: enabled with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'global.enabled=false' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/ClusterRole: disabled with connectInject.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ClusterRole: disabled with connectInject.certs.secretName set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.certs.secretName=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ClusterRole: enabled with connectInject.certs.secretName not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-clusterrole.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
