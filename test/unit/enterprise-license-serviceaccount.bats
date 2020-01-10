#!/usr/bin/env bats

load _helpers

@test "enterpriseLicense/ServiceAccount: disabled by default" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-serviceaccount.yaml  \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ServiceAccount: disabled with server=false, ent secret defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-serviceaccount.yaml  \
      --set 'server.enabled=false' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ServiceAccount: disabled when ent secretName missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-serviceaccount.yaml  \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ServiceAccount: disabled when ent secretKey missing" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-serviceaccount.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "false" ]
}

@test "enterpriseLicense/ServiceAccount: enabled when ent license defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -x templates/enterprise-license-serviceaccount.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
