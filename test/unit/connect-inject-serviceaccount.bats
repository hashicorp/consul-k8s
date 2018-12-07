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

@test "connectInject/ServiceAccount: enabled with global.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-serviceaccount.yaml  \
      --set 'global.enabled=false' \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq -s 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

@test "connectInject/ServiceAccount: disabled with connectInject.enabled false" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-serviceaccount.yaml  \
      --set 'connectInject.enabled=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ServiceAccount: disabled with connectInject.certs.secretName set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-serviceaccount.yaml  \
      --set 'connectInject.enabled=true' \
      --set 'connectInject.certs.secretName=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "connectInject/ServiceAccount: enabled with connectInject.certs.secretName not set" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/connect-inject-serviceaccount.yaml  \
      --set 'connectInject.enabled=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
