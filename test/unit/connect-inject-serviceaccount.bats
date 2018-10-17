#!/usr/bin/env bats

load _helpers
@test "connectInject/ServiceAccount: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-serviceaccount.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ServiceAccount: enable with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/ServiceAccount: disable with connectInject.enabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-serviceaccount.yaml  \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ServiceAccount: disable with connectInject.certs.secretName set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-serviceaccount.yaml  \
      --set 'connectInject.enabled=false' \
      --set 'connectInject.certs.secretName=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ServiceAccount: enable with connectInject.certs.secretName not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-serviceaccount.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
