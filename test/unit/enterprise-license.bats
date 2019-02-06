#!/usr/bin/env bats

load _helpers

@test "server/EnterpriseLicense: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/EnterpriseLicense: disabled when servers are disabled" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enabled=false' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/EnterpriseLicense: disabled when secretName is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/EnterpriseLicense: disabled when secretKey is missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "server/EnterpriseLicense: enabled when secretName and secretKey is provided" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
