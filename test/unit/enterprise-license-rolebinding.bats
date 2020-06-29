#!/usr/bin/env bats

load _helpers

@test "enterpriseLicense/RoleBinding: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-rolebinding.yaml  \
      .
}

@test "enterpriseLicense/RoleBinding: disabled with server=false, ent secret defined" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-rolebinding.yaml  \
      --set 'server.enabled=false' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      .
}

@test "enterpriseLicense/RoleBinding: disabled when ent secretName missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-rolebinding.yaml  \
      --set 'server.enterpriseLicense.secretKey=bar' \
      .
}

@test "enterpriseLicense/RoleBinding: disabled when ent secretKey missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-rolebinding.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      .
}

@test "enterpriseLicense/RoleBinding: enabled when ent license defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-rolebinding.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
