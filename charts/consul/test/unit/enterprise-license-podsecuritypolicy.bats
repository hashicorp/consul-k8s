#!/usr/bin/env bats

load _helpers

@test "enterpriseLicense/PodSecurityPolicy: disabled by default" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-podsecuritypolicy.yaml  \
      .
}

@test "enterpriseLicense/PodSecurityPolicy: disabled if autoload is true (default)" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-podsecuritypolicy.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/PodSecurityPolicy: disabled with server=false, ent secret defined" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-podsecuritypolicy.yaml  \
      --set 'server.enabled=false' \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/PodSecurityPolicy: disabled when ent secretName missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-podsecuritypolicy.yaml  \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/PodSecurityPolicy: disabled when ent secretKey missing" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-podsecuritypolicy.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      .
}

@test "enterpriseLicense/PodSecurityPolicy: disabled when enablePodSecurityPolicies=false" {
  cd `chart_dir`
  assert_empty helm template \
      -s templates/enterprise-license-podsecuritypolicy.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.enablePodSecurityPolicies=false' \
      .
}

@test "enterpriseLicense/PodSecurityPolicy: enabled when ent license defined and enablePodSecurityPolicies=true" {
  cd `chart_dir`
  local actual=$(helm template \
      -s templates/enterprise-license-podsecuritypolicy.yaml  \
      --set 'global.enterpriseLicense.secretName=foo' \
      --set 'global.enterpriseLicense.secretKey=bar' \
      --set 'global.enterpriseLicense.enableLicenseAutoload=false' \
      --set 'global.enablePodSecurityPolicies=true' \
      . | tee /dev/stderr |
      yq 'length > 0' | tee /dev/stderr)
  [ "${actual}" = "true" ]
}
