#!/usr/bin/env bats

load _helpers

@test "enterpriseLicense/ServiceAccount: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-serviceaccount.yaml  \
      .
}

@test "enterpriseLicense/ServiceAccount: disabled if autoload is true (default)" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-serviceaccount.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      .
}

@test "enterpriseLicense/ServiceAccount: disabled with server=false, ent secret defined" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-serviceaccount.yaml  \
      --set 'server.enabled=false' \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/ServiceAccount: disabled when ent secretName missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-serviceaccount.yaml  \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/ServiceAccount: disabled when ent secretKey missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-serviceaccount.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/ServiceAccount: enabled when ent license defined" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-serviceaccount.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}

#--------------------------------------------------------------------
# global.imagePullSecrets

@test "enterpriseLicense/ServiceAccount: can set image pull secrets" {
  cd `chart_dir`
  local object=$(helm template \
      -s templates/enterprise-license-serviceaccount.yaml  \
      --set 'server.enterpriseLicense.secretName=foo' \
      --set 'server.enterpriseLicense.secretKey=bar' \
      --set 'server.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.imagePullSecrets[0].name=my-secret' \
      --set 'global.imagePullSecrets[1].name=my-secret2' \
      . | tee /dev/stderr)

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[0].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret" ]

  local actual=$(echo "$object" |
      yq -r '.imagePullSecrets[1].name' | tee /dev/stderr)
  [ "${actual}" = "my-secret2" ]
}

