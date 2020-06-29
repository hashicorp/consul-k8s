#!/usr/bin/env bats

load _helpers

@test "connectInject/ClusterRoleBinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-clusterrolebinding.yaml  \
      .
}

@test "connectInject/ClusterRoleBinding: enabled with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrolebinding.yaml  \
      --set 'global.enabled=false' \
      --set 'client.enabled=true' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/ClusterRoleBinding: disabled with connectInject.enabled false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-clusterrolebinding.yaml  \
      --set 'connectInject.enabled=false' \
      .
}

@test "connectInject/ClusterRoleBinding: disabled with connectInject.certs.secretName set" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/connect-inject-clusterrolebinding.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.certs.secretName=foo' \
      .
}

@test "connectInject/ClusterRoleBinding: enabled with connectInject.certs.secretName not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/connect-inject-clusterrolebinding.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
